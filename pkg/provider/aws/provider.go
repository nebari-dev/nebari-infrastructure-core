package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
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

// Validate validates the AWS configuration with pre-flight checks
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
	)

	// Check that AWS configuration exists
	if cfg.AmazonWebServices == nil {
		err := fmt.Errorf("AWS configuration is required")
		span.RecordError(err)
		return err
	}

	awsCfg := cfg.AmazonWebServices

	// Validate required fields
	if awsCfg.Region == "" {
		err := fmt.Errorf("AWS region is required")
		span.RecordError(err)
		return err
	}

	// Validate Kubernetes version format
	if awsCfg.KubernetesVersion != "" {
		// Basic validation - should be like "1.28", "1.29", etc.
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

	// Validate endpoint access setting
	if awsCfg.EKSEndpointAccess != "" {
		validValues := []string{"public", "private", "public-and-private"}
		if !contains(validValues, awsCfg.EKSEndpointAccess) {
			err := fmt.Errorf("invalid EKS endpoint access: %s (must be one of: %v)", awsCfg.EKSEndpointAccess, validValues)
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

	// Try to initialize AWS clients to validate credentials
	clients, err := NewClients(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize AWS clients (check credentials): %w", err)
	}

	span.SetAttributes(
		attribute.Bool("validation_passed", true),
		attribute.String("aws.region", clients.Region),
	)

	return nil
}

// Deploy deploys AWS infrastructure using stateless reconciliation
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if cfg.AmazonWebServices == nil {
		err := fmt.Errorf("AWS configuration is required")
		span.RecordError(err)
		return err
	}

	region := cfg.AmazonWebServices.Region
	span.SetAttributes(attribute.String("aws.region", region))

	// Use Reconcile to deploy infrastructure (Reconcile initializes its own clients)
	return p.Reconcile(ctx, cfg)
}

// Query discovers the current state of AWS infrastructure (stub implementation)
func (p *Provider) Query(ctx context.Context, clusterName string) (*provider.InfrastructureState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Query")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
	)

	fmt.Printf("aws.Query called for cluster: %s\n", clusterName)

	// TODO: Implement actual discovery logic by querying AWS APIs
	// For now, return nil to indicate no infrastructure found
	_ = ctx // ctx will be used when implementation is complete
	return nil, nil
}

// Reconcile reconciles AWS infrastructure state using stateless discovery
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Reconcile")
	defer span.End()

	clusterName := cfg.ProjectName
	region := cfg.AmazonWebServices.Region

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// Initialize AWS clients
	clients, err := NewClients(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create AWS clients: %w", err)
	}

	// 1. Discover VPC
	actualVPC, err := p.DiscoverVPC(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to discover VPC: %w", err)
	}

	// 2. Reconcile VPC
	err = p.reconcileVPC(ctx, clients, cfg, actualVPC)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to reconcile VPC: %w", err)
	}

	// Re-discover VPC after reconciliation (may have been created)
	actualVPC, err = p.DiscoverVPC(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to re-discover VPC after reconciliation: %w", err)
	}

	if actualVPC == nil {
		err := fmt.Errorf("VPC was not created during reconciliation")
		span.RecordError(err)
		return err
	}

	// 3. Create or discover IAM roles
	// For now, we create IAM roles if they don't exist
	// TODO: Implement IAM role discovery
	iamRoles, err := p.createIAMRoles(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create IAM roles: %w", err)
	}

	// 4. Discover EKS cluster
	actualCluster, err := p.DiscoverCluster(ctx, clients, clusterName)
	if err != nil {
		// Cluster doesn't exist is OK - we'll create it
		actualCluster = nil
	}

	// 5. Reconcile EKS cluster
	err = p.reconcileCluster(ctx, clients, cfg, actualVPC, iamRoles, actualCluster)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to reconcile EKS cluster: %w", err)
	}

	// Re-discover cluster after reconciliation (may have been created)
	actualCluster, err = p.DiscoverCluster(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to re-discover EKS cluster after reconciliation: %w", err)
	}

	if actualCluster == nil {
		err := fmt.Errorf("EKS cluster was not created during reconciliation")
		span.RecordError(err)
		return err
	}

	// 6. Discover node groups
	actualNodeGroups, err := p.DiscoverNodeGroups(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to discover node groups: %w", err)
	}

	// 7. Reconcile node groups
	err = p.reconcileNodeGroups(ctx, clients, cfg, actualVPC, actualCluster, iamRoles, actualNodeGroups)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to reconcile node groups: %w", err)
	}

	span.SetAttributes(
		attribute.Bool("reconciliation_complete", true),
	)

	return nil
}

// Destroy tears down AWS infrastructure in reverse order
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Destroy")
	defer span.End()

	clusterName := cfg.ProjectName
	region := cfg.AmazonWebServices.Region

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	// TODO: Implement actual destroy logic in reverse order:
	// 1. Delete node groups (wait for deletion)
	// 2. Delete EKS cluster (wait for deletion)
	// 3. Delete EFS (if implemented)
	// 4. Delete NAT gateways, Internet Gateway, Subnets, VPC (wait for deletion)
	// 5. Delete IAM roles (detach policies first)
	// 6. Release Elastic IPs
	//
	// For now, return an error indicating this is not implemented
	// to prevent accidental resource deletion without proper testing

	_ = ctx // ctx will be used when implementation is complete
	err := fmt.Errorf("Destroy is not yet implemented - manual cleanup required for cluster: %s", clusterName)
	span.RecordError(err)
	return err
}

// GetKubeconfig generates a kubeconfig file for the EKS cluster
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
	)

	// Discover the cluster to get region
	// We need to query across regions to find the cluster
	// For now, we'll return an error requiring the caller to provide region
	_ = ctx // ctx will be used when implementation is complete
	return nil, fmt.Errorf("GetKubeconfig requires region - use Query() first to discover cluster region")
}
