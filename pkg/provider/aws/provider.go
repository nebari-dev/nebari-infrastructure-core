package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

const (
	// ProviderName is the identifier for the AWS provider.
	ProviderName = "aws"

	// ReconcileTimeout is the maximum time allowed for a complete reconciliation operation
	// This includes VPC, IAM, EKS cluster, and node group operations
	ReconcileTimeout = 30 * time.Minute
	AWS              = "aws"
)

// Provider implements the AWS provider
type Provider struct{}

// NewProvider creates a new AWS provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return ProviderName
}

// contains checks if a string slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// containsSubstring checks if any string in the slice contains the substring
func containsSubstring(slice []string, substr string) bool {
	for _, s := range slice {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// ConfigKey returns the YAML configuration key for AWS
func (p *Provider) ConfigKey() string {
	return "amazon_web_services"
}

// extractAWSConfig converts the any provider config to AWS Config type
func extractAWSConfig(ctx context.Context, cfg *config.NebariConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.extractAWSConfig")
	defer span.End()

	rawCfg := cfg.ProviderConfig["amazon_web_services"]
	if rawCfg == nil {
		err := fmt.Errorf("AWS configuration is required")
		span.RecordError(err)
		return nil, err
	}

	var awsCfg Config
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &awsCfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal AWS config: %w", err)
	}

	return &awsCfg, nil
}

// Validate validates the AWS configuration with pre-flight checks
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("existing_cluster", cfg.IsExistingCluster()),
	)

	// For existing clusters, we don't need AWS infrastructure config
	if cfg.IsExistingCluster() {
		span.SetAttributes(attribute.String("kube_context", cfg.GetKubeContext()))
		// Just validate that the kube context exists
		if err := kubeconfig.ValidateContext(cfg.GetKubeContext()); err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}

	// Extract and validate AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Validate required fields
	if awsCfg.Region == "" {
		err := fmt.Errorf("AWS region is required")
		span.RecordError(err)
		return err
	}

	// Validate Kubernetes version format
	if awsCfg.KubernetesVersion != "" {
		// Basic validation - should be like "1.34", "1.29", etc.
		if len(awsCfg.KubernetesVersion) < 3 {
			err := fmt.Errorf("invalid Kubernetes version format: %s", awsCfg.KubernetesVersion)
			span.RecordError(err)
			return err
		}
	}

	// Validate VPC CIDR block if specified
	if awsCfg.VPCCIDRBlock != "" {
		// Basic CIDR validation
		if !containsSubstring([]string{awsCfg.VPCCIDRBlock}, "/") {
			err := fmt.Errorf("invalid VPC CIDR block format: %s (must include /prefix)", awsCfg.VPCCIDRBlock)
			span.RecordError(err)
			return err
		}
	}

	// Validate node groups
	if len(awsCfg.NodeGroups) == 0 {
		err := fmt.Errorf("at least one node group is required")
		span.RecordError(err)
		return err
	}

	for nodeGroupName, nodeGroup := range awsCfg.NodeGroups {
		// Validate instance type is specified
		if nodeGroup.Instance == "" {
			err := fmt.Errorf("node group %s: instance type is required", nodeGroupName)
			span.RecordError(err)
			return err
		}

		// Validate scaling configuration
		if nodeGroup.MinNodes < 0 {
			err := fmt.Errorf("node group %s: min_nodes cannot be negative", nodeGroupName)
			span.RecordError(err)
			return err
		}

		if nodeGroup.MaxNodes < 0 {
			err := fmt.Errorf("node group %s: max_nodes cannot be negative", nodeGroupName)
			span.RecordError(err)
			return err
		}

		if nodeGroup.MinNodes > 0 && nodeGroup.MaxNodes > 0 && nodeGroup.MinNodes > nodeGroup.MaxNodes {
			err := fmt.Errorf("node group %s: min_nodes (%d) cannot be greater than max_nodes (%d)", nodeGroupName, nodeGroup.MinNodes, nodeGroup.MaxNodes)
			span.RecordError(err)
			return err
		}

		// Validate taints
		for i, taint := range nodeGroup.Taints {
			if taint.Key == "" {
				err := fmt.Errorf("node group %s: taint %d is missing key", nodeGroupName, i)
				span.RecordError(err)
				return err
			}

			validEffects := []string{"NoSchedule", "NoExecute", "PreferNoSchedule"}
			if !contains(validEffects, taint.Effect) {
				err := fmt.Errorf("node group %s: taint %d has invalid effect %s (must be one of: %v)", nodeGroupName, i, taint.Effect, validEffects)
				span.RecordError(err)
				return err
			}
		}
	}

	// Validate AWS credentials using GetCallerIdentity
	stsClient, err := newSTSClient(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create STS client: %w", err)
	}

	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("AWS credential validation failed: %w", err)
	}

	span.SetAttributes(
		attribute.String("aws.account_id", aws.ToString(identity.Account)),
		attribute.String("aws.arn", aws.ToString(identity.Arn)),
		attribute.Bool("validation_passed", true),
		attribute.String("aws.region", awsCfg.Region),
	)

	return nil
}

// Deploy deploys AWS infrastructure using stateless reconciliation
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("dry_run", cfg.DryRun),
		attribute.Bool("existing_cluster", cfg.IsExistingCluster()),
	)

	// For existing clusters, skip infrastructure provisioning
	if cfg.IsExistingCluster() {
		span.SetAttributes(attribute.String("kube_context", cfg.GetKubeContext()))
		// Nothing to deploy - using existing cluster
		return nil
	}

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	region := awsCfg.Region
	span.SetAttributes(attribute.String("aws.region", region))

	// Get bucket name from config or generate one
	bucketName := awsCfg.StateBucket
	if bucketName == "" {
		stsClient, err := newSTSClient(ctx, region)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create STS client: %w", err)
		}
		bucketName, err = getStateBucketName(ctx, stsClient, region, cfg.ProjectName)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to get state bucket name: %w", err)
		}
	}

	// Check if state bucket exists
	s3Client, err := newS3Client(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	bucketExists, err := stateBucketExists(ctx, s3Client, bucketName)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Only create the state bucket for non-dry-run operations
	if !cfg.DryRun {
		if err := ensureStateBucket(ctx, s3Client, awsCfg.Region, bucketName); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to ensure state bucket: %w", err)
		}
	}

	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer func() {
		err := tf.Cleanup()
		if err != nil {
			span.RecordError(err)
		}
	}()

	if cfg.DryRun && !bucketExists {
		// First-time dry run: override the S3 backend with a local backend since
		// the state bucket doesn't exist yet and a dry run should not create cloud resources.
		if err := tf.WriteBackendOverride(); err != nil {
			span.RecordError(err)
			return err
		}
		err = tf.Init(ctx)
	} else {
		err = tf.Init(ctx,
			tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
			tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(cfg.ProjectName))),
			tfexec.BackendConfig(fmt.Sprintf("region=%s", awsCfg.Region)),
		)
	}
	if err != nil {
		span.RecordError(err)
		return err
	}

	if cfg.DryRun {
		_, err = tf.Plan(ctx)
		if err != nil {
			span.RecordError(err)
			return err
		}
		return nil
	}

	err = tf.Apply(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

// Destroy tears down AWS infrastructure in reverse order
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Destroy")
	defer span.End()

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	region := awsCfg.Region

	// Get bucket name from config or generate one
	bucketName := awsCfg.StateBucket
	if bucketName == "" {
		stsClient, err := newSTSClient(ctx, region)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create STS client: %w", err)
		}
		bucketName, err = getStateBucketName(ctx, stsClient, region, cfg.ProjectName)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to get state bucket name: %w", err)
		}
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("cluster_name", cfg.ProjectName),
		attribute.String("region", region),
		attribute.Bool("dry_run", cfg.DryRun),
		attribute.Bool("force", cfg.Force),
	)

	// Check if state bucket exists
	s3Client, err := newS3Client(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	bucketExists, err := stateBucketExists(ctx, s3Client, bucketName)
	if err != nil {
		span.RecordError(err)
		return err
	}

	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return err
	}
	defer func() {
		err := tf.Cleanup()
		if err != nil {
			span.RecordError(err)
		}
	}()

	if cfg.DryRun && !bucketExists {
		// First-time dry run: override the S3 backend with a local backend since
		// the state bucket doesn't exist yet and a dry run should not create cloud resources.
		if err := tf.WriteBackendOverride(); err != nil {
			span.RecordError(err)
			return err
		}
		err = tf.Init(ctx)
	} else {
		err = tf.Init(ctx,
			tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
			tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(cfg.ProjectName))),
			tfexec.BackendConfig(fmt.Sprintf("region=%s", region)),
		)
	}
	if err != nil {
		span.RecordError(err)
		return err
	}

	if cfg.DryRun {
		_, err = tf.Plan(ctx, tfexec.Destroy(true))
		if err != nil {
			span.RecordError(err)
			return err
		}

		// Since this is a dry run, we return earlier to avoid destroying the state bucket
		return nil
	}

	// Clean up Kubernetes-created load balancers before destroying infrastructure.
	// These are not managed by Terraform and will block VPC/subnet deletion.
	status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Cleaning up Kubernetes-created load balancers for cluster: %s", cfg.ProjectName)).
		WithResource("load-balancer").
		WithAction("cleanup"))
	elbClient, err := newELBClient(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create ELB client: %w", err)
	}
	ec2ClientForCleanup, err := newEC2Client(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create EC2 client: %w", err)
	}
	if err := cleanupKubernetesLoadBalancers(ctx, elbClient, ec2ClientForCleanup, cfg.ProjectName); err != nil {
		if cfg.Force {
			status.Send(ctx, status.NewUpdate(status.LevelWarning, fmt.Sprintf("Failed to clean up load balancers, continuing with --force: %v", err)).
				WithResource("load-balancer").
				WithAction("cleanup"))
		} else {
			span.RecordError(err)
			return fmt.Errorf("failed to clean up load balancers: %w", err)
		}
	}

	err = tf.Destroy(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := destroyStateBucket(ctx, s3Client, region, bucketName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to destroy state bucket: %w", err)
	}

	return nil
}

// GetKubeconfig generates a kubeconfig file for the EKS cluster
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GetKubeconfig")
	defer span.End()

	// For existing clusters, extract kubeconfig from the specified context
	if cfg.IsExistingCluster() {
		contextName := cfg.GetKubeContext()
		span.SetAttributes(
			attribute.String("provider", ProviderName),
			attribute.String("kube_context", contextName),
			attribute.Bool("existing_cluster", true),
		)
		kubeconfigBytes, err := kubeconfig.ExtractContext(contextName)
		if err != nil {
			span.RecordError(err)
			return nil, err
		}
		return kubeconfigBytes, nil
	}

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	clusterName := cfg.ProjectName
	region := awsCfg.Region

	// Get bucket name from config or generate one
	bucketName := awsCfg.StateBucket
	if bucketName == "" {
		stsClient, err := newSTSClient(ctx, region)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create STS client: %w", err)
		}
		bucketName, err = getStateBucketName(ctx, stsClient, region, cfg.ProjectName)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to get state bucket name: %w", err)
		}
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Verify the state bucket exists before attempting to read outputs
	s3Client, err := newS3Client(ctx, region)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}
	bucketExists, err := stateBucketExists(ctx, s3Client, bucketName)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if !bucketExists {
		err := fmt.Errorf("state bucket does not exist: run 'deploy' first")
		span.RecordError(err)
		return nil, err
	}

	// Initialize terraform to read outputs from state
	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to setup terraform: %w", err)
	}
	defer func() {
		err := tf.Cleanup()
		if err != nil {
			span.RecordError(err)
		}
	}()

	err = tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(cfg.ProjectName))),
		tfexec.BackendConfig(fmt.Sprintf("region=%s", region)),
	)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to initialize terraform: %w", err)
	}

	// Get outputs from terraform state
	outputs, err := tf.Output(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get terraform outputs: %w", err)
	}

	endpoint, ok := outputs["cluster_endpoint"]
	if !ok {
		err := fmt.Errorf("cluster not found: run 'deploy' first to create the cluster")
		span.RecordError(err)
		return nil, err
	}

	caData, ok := outputs["cluster_certificate_authority_data"]
	if !ok {
		err := fmt.Errorf("cluster not found: run 'deploy' first to create the cluster")
		span.RecordError(err)
		return nil, err
	}

	// Output values are JSON-encoded strings, need to extract the actual string value
	var endpointStr, caDataStr string
	if err := json.Unmarshal(endpoint.Value, &endpointStr); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal cluster_endpoint: %w", err)
	}
	if err := json.Unmarshal(caData.Value, &caDataStr); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal cluster_certificate_authority_data: %w", err)
	}

	kubeconfigBytes, err := buildKubeconfig(clusterName, endpointStr, caDataStr, region)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	return kubeconfigBytes, nil
}

// Summary returns key configuration details for display purposes
func (p *Provider) Summary(cfg *config.NebariConfig) map[string]string {
	result := make(map[string]string)

	// Show kube context if using existing cluster
	if cfg.IsExistingCluster() {
		result["Kube Context"] = cfg.GetKubeContext()
		result["Mode"] = "existing-cluster"
		return result
	}

	rawCfg := cfg.ProviderConfig["amazon_web_services"]
	if rawCfg == nil {
		return result
	}

	var awsCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &awsCfg); err != nil {
		return result
	}

	result["Region"] = awsCfg.Region
	return result
}
