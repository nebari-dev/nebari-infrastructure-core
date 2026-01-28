package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/hashicorp/terraform-exec/tfexec"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	nebariconfig "github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// Provider implements the AWS provider
type Provider struct{}

// NewProvider creates a new AWS provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "aws"
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

// extractAWSConfig converts the any provider config to AWS Config type
func extractAWSConfig(ctx context.Context, cfg *nebariconfig.NebariConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.extractAWSConfig")
	defer span.End()

	if cfg.AmazonWebServices == nil {
		err := fmt.Errorf("AWS configuration is required")
		span.RecordError(err)
		return nil, err
	}

	var awsCfg Config
	if err := nebariconfig.UnmarshalProviderConfig(ctx, cfg.AmazonWebServices, &awsCfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal AWS config: %w", err)
	}

	return &awsCfg, nil
}

// Validate validates the AWS configuration with pre-flight checks
func (p *Provider) Validate(ctx context.Context, cfg *nebariconfig.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
	)

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

	// Validate AWS credentials
	sdkCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(awsCfg.Region))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}
	if _, err := sdkCfg.Credentials.Retrieve(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to retrieve AWS credentials: %w", err)
	}

	span.SetAttributes(
		attribute.Bool("validation_passed", true),
		attribute.String("aws.region", awsCfg.Region),
	)

	return nil
}

// Deploy deploys AWS infrastructure using stateless reconciliation
func (p *Provider) Deploy(ctx context.Context, cfg *nebariconfig.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("dry_run", cfg.DryRun),
	)

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(attribute.String("aws.region", awsCfg.Region))

	// Ensure state bucket exists before initializing Terraform
	bucketName := stateBucketName(cfg.ProjectName)
	if err := ensureStateBucket(ctx, bucketName, awsCfg.Region); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure state bucket: %w", err)
	}

	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(cfg.ProjectName))),
		tfexec.BackendConfig(fmt.Sprintf("region=%s", awsCfg.Region)),
	)
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
func (p *Provider) Destroy(ctx context.Context, cfg *nebariconfig.NebariConfig) error {
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
	bucketName := stateBucketName(cfg.ProjectName)

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", cfg.ProjectName),
		attribute.String("region", region),
		attribute.Bool("dry_run", cfg.DryRun),
		attribute.Bool("force", cfg.Force),
	)

	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return err
	}

	err = tf.Init(ctx,
		tfexec.BackendConfig(fmt.Sprintf("bucket=%s", bucketName)),
		tfexec.BackendConfig(fmt.Sprintf("key=%s", stateKey(cfg.ProjectName))),
		tfexec.BackendConfig(fmt.Sprintf("region=%s", region)),
	)
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
		return nil
	}

	err = tf.Destroy(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if err := destroyStateBucket(ctx, bucketName, region); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to destroy state bucket: %w", err)
	}

	return nil
}

// GetKubeconfig generates a kubeconfig file for the EKS cluster
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *nebariconfig.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GetKubeconfig")
	defer span.End()

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	clusterName := cfg.ProjectName
	region := awsCfg.Region
	bucketName := stateBucketName(cfg.ProjectName)

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Initialize terraform to read outputs from state
	tf, err := tofu.Setup(ctx, tofuTemplates, awsCfg.toTFVars(cfg.ProjectName))
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to setup terraform: %w", err)
	}

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
