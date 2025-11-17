package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// reconcileCluster reconciles the desired EKS cluster configuration with actual state
func (p *Provider) reconcileCluster(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, iamRoles *IAMRoles, actual *ClusterState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.reconcileCluster")
	defer span.End()

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	// Case 1: Cluster doesn't exist - create it
	if actual == nil {
		span.SetAttributes(attribute.String("action", "create"))

		_, err := p.createEKSCluster(ctx, clients, cfg, vpc, iamRoles)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create EKS cluster: %w", err)
		}

		return nil
	}

	// Case 2: Cluster exists - validate immutable fields and update mutable ones
	span.SetAttributes(attribute.String("action", "update"))

	// Validate immutable field: VPC configuration
	if actual.VPCID != vpc.VPCID {
		err := fmt.Errorf("EKS cluster VPC configuration is immutable and cannot be changed (current: %s, desired: %s). Manual intervention required - destroy and recreate cluster", actual.VPCID, vpc.VPCID)
		span.RecordError(err)
		return err
	}

	// Update mutable fields if needed
	updateNeeded := false
	updateInput := &eks.UpdateClusterConfigInput{
		Name: aws.String(clusterName),
	}

	// Check if Kubernetes version needs update
	desiredVersion := cfg.AmazonWebServices.KubernetesVersion
	if desiredVersion == "" {
		desiredVersion = DefaultKubernetesVersion
	}

	if actual.Version != desiredVersion {
		// Version update is a separate operation
		span.SetAttributes(
			attribute.String("version_update.from", actual.Version),
			attribute.String("version_update.to", desiredVersion),
		)

		versionUpdateInput := &eks.UpdateClusterVersionInput{
			Name:    aws.String(clusterName),
			Version: aws.String(desiredVersion),
		}

		_, err := clients.EKSClient.UpdateClusterVersion(ctx, versionUpdateInput)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update EKS cluster version: %w", err)
		}

		// Wait for version update to complete
		waiter := eks.NewClusterActiveWaiter(clients.EKSClient)
		describeInput := &eks.DescribeClusterInput{
			Name: aws.String(clusterName),
		}

		_, err = waiter.WaitForOutput(ctx, describeInput, EKSClusterUpdateTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed waiting for EKS cluster version update: %w", err)
		}
	}

	// Check if endpoint access needs update
	desiredPublic := DefaultEndpointPublic
	desiredPrivate := DefaultEndpointPrivate
	switch cfg.AmazonWebServices.EKSEndpointAccess {
	case "private":
		desiredPublic = false
		desiredPrivate = true
	case "public-and-private":
		desiredPublic = true
		desiredPrivate = true
	}

	if actual.EndpointPublic != desiredPublic || actual.EndpointPrivate != desiredPrivate {
		updateNeeded = true
		updateInput.ResourcesVpcConfig = &ekstypes.VpcConfigRequest{
			EndpointPublicAccess:  aws.Bool(desiredPublic),
			EndpointPrivateAccess: aws.Bool(desiredPrivate),
		}

		span.SetAttributes(
			attribute.Bool("endpoint_access.update_needed", true),
			attribute.Bool("endpoint_access.desired_public", desiredPublic),
			attribute.Bool("endpoint_access.desired_private", desiredPrivate),
		)
	}

	// Check if logging needs update
	loggingUpdateNeeded := p.checkLoggingUpdate(actual)
	if loggingUpdateNeeded {
		updateNeeded = true
		updateInput.Logging = &ekstypes.Logging{
			ClusterLogging: []ekstypes.LogSetup{
				{
					Enabled: aws.Bool(true),
					Types: []ekstypes.LogType{
						ekstypes.LogTypeApi,
						ekstypes.LogTypeAudit,
						ekstypes.LogTypeAuthenticator,
						ekstypes.LogTypeControllerManager,
						ekstypes.LogTypeScheduler,
					},
				},
			},
		}

		span.SetAttributes(
			attribute.Bool("logging.update_needed", true),
		)
	}

	// Apply updates if needed
	if updateNeeded {
		_, err := clients.EKSClient.UpdateClusterConfig(ctx, updateInput)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update EKS cluster configuration: %w", err)
		}

		// Wait for update to complete
		waiter := eks.NewClusterActiveWaiter(clients.EKSClient)
		describeInput := &eks.DescribeClusterInput{
			Name: aws.String(clusterName),
		}

		_, err = waiter.WaitForOutput(ctx, describeInput, EKSClusterUpdateTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed waiting for EKS cluster update: %w", err)
		}

		span.SetAttributes(attribute.Bool("update_applied", true))
	} else {
		span.SetAttributes(attribute.Bool("update_applied", false))
	}

	return nil
}

// checkLoggingUpdate checks if logging configuration needs to be updated
func (p *Provider) checkLoggingUpdate(actual *ClusterState) bool {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.checkLoggingUpdate")
	defer span.End()

	// We want all 5 log types enabled
	requiredLogTypes := []string{
		string(ekstypes.LogTypeApi),
		string(ekstypes.LogTypeAudit),
		string(ekstypes.LogTypeAuthenticator),
		string(ekstypes.LogTypeControllerManager),
		string(ekstypes.LogTypeScheduler),
	}

	// Check if all required log types are enabled
	for _, required := range requiredLogTypes {
		found := false
		for _, enabled := range actual.EnabledLogTypes {
			if enabled == required {
				found = true
				break
			}
		}
		if !found {
			span.SetAttributes(
				attribute.String("missing_log_type", required),
			)
			return true
		}
	}

	return false
}
