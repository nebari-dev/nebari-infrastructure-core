package aws

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

const (
	// ReconcileTimeout is the maximum time allowed for a complete reconciliation operation
	// This includes VPC, IAM, EKS cluster, and node group operations
	ReconcileTimeout = 30 * time.Minute
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
	_, span := tracer.Start(ctx, "aws.Validate")
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
	_, span := tracer.Start(ctx, "aws.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
		attribute.Bool("dry_run", cfg.DryRun),
	)

	if cfg.AmazonWebServices == nil {
		err := fmt.Errorf("AWS configuration is required")
		span.RecordError(err)
		return err
	}

	region := cfg.AmazonWebServices.Region
	span.SetAttributes(attribute.String("aws.region", region))

	// Handle dry-run mode
	if cfg.DryRun {
		return p.dryRunDeploy(ctx, cfg)
	}

	// Use Reconcile to deploy infrastructure (Reconcile initializes its own clients)
	return p.Reconcile(ctx, cfg)
}

// Reconcile reconciles AWS infrastructure state using stateless discovery
// Note: Pure orchestration function - delegates all logic to tested helper functions.
// Unit test coverage via helper functions. Integration tests validate orchestration.
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Reconcile")
	defer span.End()

	// Determine timeout - use config override if set, otherwise default
	timeout := ReconcileTimeout
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	// Enforce timeout for the entire reconciliation operation
	reconcileCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = reconcileCtx

	clusterName := cfg.ProjectName
	region := cfg.AmazonWebServices.Region

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
		attribute.String("timeout", timeout.String()),
	)

	// Initialize AWS clients
	clients, err := newClientsFunc(ctx, region)
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

	// 2. Reconcile VPC (returns updated VPC state with any newly created components)
	actualVPC, err = p.reconcileVPC(ctx, clients, cfg, actualVPC)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to reconcile VPC: %w", err)
	}

	if actualVPC == nil {
		err := fmt.Errorf("VPC was not created during reconciliation")
		span.RecordError(err)
		return err
	}

	// 3. Ensure IAM roles (discover existing or create new ones)
	iamRoles, err := p.ensureIAMRoles(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to ensure IAM roles: %w", err)
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

	// 5.5. Add EKS-managed cluster security group to VPC endpoints
	// This is critical: nodes use the EKS-managed SG, VPC endpoints need it too
	if actualCluster.ClusterSecurityGroupID != "" && len(actualVPC.VPCEndpointIDs) > 0 {
		err = p.addSecurityGroupToVPCEndpoints(ctx, clients, actualVPC.VPCEndpointIDs, actualCluster.ClusterSecurityGroupID)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to add EKS security group to VPC endpoints: %w", err)
		}
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

	// 8. Discover EFS storage (if configured)
	if cfg.AmazonWebServices.EFS != nil && cfg.AmazonWebServices.EFS.Enabled {
		actualEFS, err := p.DiscoverEFS(ctx, clients, clusterName)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to discover EFS: %w", err)
		}

		// 9. Reconcile EFS storage
		_, err = p.reconcileEFS(ctx, clients, cfg, actualVPC, actualEFS)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to reconcile EFS: %w", err)
		}
	}

	span.SetAttributes(
		attribute.Bool("reconciliation_complete", true),
	)

	return nil
}

// Destroy tears down AWS infrastructure in reverse order
// Note: Pure orchestration function - delegates all logic to tested helper functions.
// Unit test coverage via helper functions. Integration tests validate orchestration.
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.Destroy")
	defer span.End()

	clusterName := cfg.ProjectName
	region := cfg.AmazonWebServices.Region
	forceMode := cfg.Force

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
		attribute.Bool("dry_run", cfg.DryRun),
		attribute.Bool("force", forceMode),
	)

	// Initialize AWS clients
	clients, err := newClientsFunc(ctx, region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create AWS clients: %w", err)
	}

	// Handle dry-run mode
	if cfg.DryRun {
		return p.dryRunDestroy(ctx, clients, clusterName, region)
	}

	// Destroy infrastructure in reverse order of creation
	// This ensures dependencies are respected
	// In force mode, we continue even if some resources fail to delete

	var errs []error

	// 1. Delete all node groups first
	if err := p.deleteNodeGroups(ctx, clients, clusterName); err != nil {
		span.RecordError(err)
		if forceMode {
			errs = append(errs, fmt.Errorf("failed to delete node groups: %w", err))
		} else {
			return fmt.Errorf("failed to delete node groups: %w", err)
		}
	}

	// 2. Delete EFS storage (must happen before VPC deletion due to mount targets)
	efsStorage, err := p.DiscoverEFS(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		if forceMode {
			errs = append(errs, fmt.Errorf("failed to discover EFS: %w", err))
		} else {
			return fmt.Errorf("failed to discover EFS: %w", err)
		}
	}
	if efsStorage != nil {
		if err := p.deleteEFS(ctx, clients, efsStorage); err != nil {
			span.RecordError(err)
			if forceMode {
				errs = append(errs, fmt.Errorf("failed to delete EFS: %w", err))
			} else {
				return fmt.Errorf("failed to delete EFS: %w", err)
			}
		}
	}

	// 3. Delete EKS cluster
	if deleteErr := p.deleteEKSCluster(ctx, clients, clusterName); deleteErr != nil {
		span.RecordError(deleteErr)
		if forceMode {
			errs = append(errs, fmt.Errorf("failed to delete EKS cluster: %w", deleteErr))
		} else {
			return fmt.Errorf("failed to delete EKS cluster: %w", deleteErr)
		}
	}

	// 4. Delete VPC and all associated resources
	if vpcErr := p.deleteVPC(ctx, clients, clusterName); vpcErr != nil {
		span.RecordError(vpcErr)
		if forceMode {
			errs = append(errs, fmt.Errorf("failed to delete VPC: %w", vpcErr))
		} else {
			return fmt.Errorf("failed to delete VPC: %w", vpcErr)
		}
	}

	// 5. Delete IAM roles (detach policies first)
	if iamErr := p.deleteIAMRoles(ctx, clients, clusterName); iamErr != nil {
		span.RecordError(iamErr)
		if forceMode {
			errs = append(errs, fmt.Errorf("failed to delete IAM roles: %w", iamErr))
		} else {
			return fmt.Errorf("failed to delete IAM roles: %w", iamErr)
		}
	}

	// 6. Verification loop: keep cleaning up orphaned resources until none remain
	// This handles resources that may have been missed or became orphaned during deletion
	const maxVerificationPasses = 3
	for pass := 1; pass <= maxVerificationPasses; pass++ {
		span.SetAttributes(attribute.Int("verification_pass", pass))

		orphansFound, err := p.cleanupOrphanedResources(ctx, clients, clusterName)
		if err != nil {
			span.RecordError(err)
			if forceMode {
				errs = append(errs, fmt.Errorf("failed during orphan cleanup pass %d: %w", pass, err))
				break // Don't continue verification passes if cleanup fails
			} else {
				return fmt.Errorf("failed during orphan cleanup pass %d: %w", pass, err)
			}
		}

		if !orphansFound {
			span.SetAttributes(attribute.Int("verification_passes_needed", pass))
			break
		}

		if pass == maxVerificationPasses {
			span.SetAttributes(attribute.Bool("max_verification_passes_reached", true))
		}
	}

	// In force mode, return combined errors if any occurred
	if len(errs) > 0 {
		span.SetAttributes(attribute.Int("force_mode_errors", len(errs)))
		return fmt.Errorf("destroy completed with %d errors (force mode): %v", len(errs), errs)
	}

	span.SetAttributes(
		attribute.Bool("destroy_complete", true),
	)

	return nil
}

// cleanupOrphanedResources finds and cleans up any orphaned resources for this cluster
// Returns true if any orphans were found and cleaned up
func (p *Provider) cleanupOrphanedResources(ctx context.Context, clients *Clients, clusterName string) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupOrphanedResources")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	orphansFound := false

	// Check for orphaned EIPs
	eipsFound, err := p.cleanupOrphanedEIPsWithCount(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to cleanup orphaned EIPs: %w", err)
	}
	if eipsFound > 0 {
		orphansFound = true
		span.SetAttributes(attribute.Int("orphaned_eips_cleaned", eipsFound))
	}

	// Check for orphaned NAT Gateways (in deleting state that may have completed)
	natGWsFound, err := p.cleanupOrphanedNATGateways(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to cleanup orphaned NAT Gateways: %w", err)
	}
	if natGWsFound > 0 {
		orphansFound = true
		span.SetAttributes(attribute.Int("orphaned_nat_gateways_cleaned", natGWsFound))
	}

	span.SetAttributes(attribute.Bool("orphans_found", orphansFound))

	return orphansFound, nil
}

// GetKubeconfig generates a kubeconfig file for the EKS cluster
// Note: This method requires region information which is not provided in the interface
// For AWS, use GetKubeconfigWithRegion directly or discover the cluster region first
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("cluster_name", clusterName),
	)

	// AWS requires region to generate kubeconfig
	// The GetKubeconfig interface doesn't provide region, so we return an error
	// Users should call GetKubeconfigWithRegion directly
	err := fmt.Errorf("GetKubeconfig requires region parameter - use GetKubeconfigWithRegion() or discover cluster region first")
	span.RecordError(err)
	return nil, err
}
