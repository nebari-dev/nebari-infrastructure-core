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
	"golang.org/x/sync/errgroup"
)

// reconcileNodeGroups reconciles desired node group configuration with actual state
// Note: Pure orchestration function - delegates to createNodeGroup() and reconcileNodeGroup().
// Unit test coverage via helper functions.
func (p *Provider) reconcileNodeGroups(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, cluster *ClusterState, iamRoles *IAMRoles, actual []NodeGroupState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.reconcileNodeGroups")
	defer span.End()

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Int("desired_node_groups", len(cfg.AmazonWebServices.NodeGroups)),
		attribute.Int("actual_node_groups", len(actual)),
	)

	// Build map of actual node groups by node pool name (from tags)
	actualNodeGroups := make(map[string]*NodeGroupState)
	for i := range actual {
		nodePoolName, ok := actual[i].Tags[TagNodePool]
		if ok {
			actualNodeGroups[nodePoolName] = &actual[i]
		}
	}

	// Use errgroup for parallel node group reconciliation
	// This improves performance when managing multiple node groups
	g, gctx := errgroup.WithContext(ctx)

	// Reconcile each desired node group in parallel
	for nodeGroupName, nodeGroupConfig := range cfg.AmazonWebServices.NodeGroups {
		// Capture loop variables for goroutine
		ngName := nodeGroupName
		ngConfig := nodeGroupConfig
		actualNG, exists := actualNodeGroups[nodeGroupName]

		g.Go(func() error {
			if !exists {
				// Node group doesn't exist - create it
				span.SetAttributes(
					attribute.String(fmt.Sprintf("node_group.%s.action", ngName), "create"),
				)

				_, err := p.createNodeGroup(gctx, clients, cfg, vpc, cluster, iamRoles, ngName, ngConfig)
				if err != nil {
					span.RecordError(err)
					return fmt.Errorf("failed to create node group %s: %w", ngName, err)
				}
				return nil
			}

			// Node group exists - check if updates are needed
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.action", ngName), "update"),
			)

			err := p.reconcileNodeGroup(gctx, clients, cfg, ngName, ngConfig, actualNG)
			if err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to reconcile node group %s: %w", ngName, err)
			}
			return nil
		})
	}

	// Wait for all node group operations to complete
	// If any operation fails, all operations will be cancelled via context
	if err := g.Wait(); err != nil {
		span.RecordError(err)
		return err
	}

	// Handle orphaned node groups (exist in actual but not in desired)
	// These are node groups that were previously created but removed from config
	orphanedNodeGroups := []string{}
	for nodePoolName, actualNG := range actualNodeGroups {
		if _, exists := cfg.AmazonWebServices.NodeGroups[nodePoolName]; !exists {
			orphanedNodeGroups = append(orphanedNodeGroups, actualNG.Name)
		}
	}

	if len(orphanedNodeGroups) > 0 {
		span.SetAttributes(
			attribute.Int("orphaned_node_groups", len(orphanedNodeGroups)),
		)

		// Delete orphaned node groups in parallel
		g2, gctx2 := errgroup.WithContext(ctx)

		for _, ngName := range orphanedNodeGroups {
			nodeGroupName := ngName // Capture for goroutine

			g2.Go(func() error {
				span.SetAttributes(
					attribute.String(fmt.Sprintf("orphaned_node_group.%s.action", nodeGroupName), "delete"),
				)

				err := p.deleteNodeGroup(gctx2, clients, clusterName, nodeGroupName)
				if err != nil {
					span.RecordError(err)
					return fmt.Errorf("failed to delete orphaned node group %s: %w", nodeGroupName, err)
				}
				return nil
			})
		}

		if err := g2.Wait(); err != nil {
			span.RecordError(err)
			return err
		}
	}

	span.SetAttributes(attribute.Bool("parallel_reconciliation", true))
	return nil
}

// reconcileNodeGroup reconciles a single node group
// Note: Pure orchestration function - delegates to update functions based on diffs.
// Unit test coverage via helper functions (checkLabelsUpdate, checkTaintsUpdate, updateNodeGroupScaling).
func (p *Provider) reconcileNodeGroup(ctx context.Context, clients *Clients, cfg *config.NebariConfig, nodeGroupName string, desired config.AWSNodeGroup, actual *NodeGroupState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.reconcileNodeGroup")
	defer span.End()

	clusterName := cfg.ProjectName
	fullNodeGroupName := actual.Name

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("node_group_name", fullNodeGroupName),
	)

	updateNeeded := false
	updateInput := &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(fullNodeGroupName),
	}

	// Check scaling configuration
	desiredMinSize := desired.MinNodes
	desiredMaxSize := desired.MaxNodes
	if desiredMinSize == 0 {
		desiredMinSize = 1
	}
	if desiredMaxSize == 0 {
		desiredMaxSize = 3
	}

	if actual.MinSize != desiredMinSize || actual.MaxSize != desiredMaxSize {
		updateNeeded = true
		updateInput.ScalingConfig = &ekstypes.NodegroupScalingConfig{
			MinSize: aws.Int32(int32(desiredMinSize)),
			MaxSize: aws.Int32(int32(desiredMaxSize)),
			// Don't change desired size during reconciliation - respect autoscaling
		}

		span.SetAttributes(
			attribute.Bool("scaling_config.update_needed", true),
			attribute.Int("scaling_config.desired_min", desiredMinSize),
			attribute.Int("scaling_config.desired_max", desiredMaxSize),
			attribute.Int("scaling_config.actual_min", actual.MinSize),
			attribute.Int("scaling_config.actual_max", actual.MaxSize),
		)
	}

	// Check labels
	labelsUpdateNeeded := p.checkLabelsUpdate(desired, actual)
	if labelsUpdateNeeded {
		updateNeeded = true

		// Build new labels
		labels := make(map[string]string)
		labels["node-group"] = nodeGroupName
		labels["nic.nebari.dev/node-pool"] = nodeGroupName

		updateInput.Labels = &ekstypes.UpdateLabelsPayload{
			AddOrUpdateLabels: labels,
		}

		span.SetAttributes(
			attribute.Bool("labels.update_needed", true),
		)
	}

	// Check taints
	taintsUpdateNeeded := p.checkTaintsUpdate(desired, actual)
	if taintsUpdateNeeded {
		updateNeeded = true

		// Convert taints from config format to EKS format
		var eksTaints []ekstypes.Taint
		for _, taint := range desired.Taints {
			eksTaint := ekstypes.Taint{
				Key:   aws.String(taint.Key),
				Value: aws.String(taint.Value),
			}

			// Convert effect string to EKS TaintEffect type
			switch taint.Effect {
			case "NoSchedule":
				eksTaint.Effect = ekstypes.TaintEffectNoSchedule
			case "NoExecute":
				eksTaint.Effect = ekstypes.TaintEffectNoExecute
			case "PreferNoSchedule":
				eksTaint.Effect = ekstypes.TaintEffectPreferNoSchedule
			default:
				eksTaint.Effect = ekstypes.TaintEffectNoSchedule
			}

			eksTaints = append(eksTaints, eksTaint)
		}

		updateInput.Taints = &ekstypes.UpdateTaintsPayload{
			AddOrUpdateTaints: eksTaints,
		}

		span.SetAttributes(
			attribute.Bool("taints.update_needed", true),
		)
	}

	// Validate immutable fields
	if len(actual.InstanceTypes) > 0 && actual.InstanceTypes[0] != desired.Instance {
		err := fmt.Errorf("node group instance type is immutable and cannot be changed (current: %s, desired: %s). Manual intervention required - destroy and recreate node group", actual.InstanceTypes[0], desired.Instance)
		span.RecordError(err)
		return err
	}

	// Check for AMI type changes (also immutable)
	desiredAMIType := string(DefaultAMIType)
	if desired.AMIType != "" {
		desiredAMIType = desired.AMIType
	} else if desired.GPU {
		desiredAMIType = string(ekstypes.AMITypesAl2023X8664Nvidia)
	}

	if actual.AMIType != desiredAMIType {
		err := fmt.Errorf("node group AMI type is immutable and cannot be changed (current: %s, desired: %s). Manual intervention required - destroy and recreate node group", actual.AMIType, desiredAMIType)
		span.RecordError(err)
		span.SetAttributes(
			attribute.String("current_ami_type", actual.AMIType),
			attribute.String("desired_ami_type", desiredAMIType),
		)
		return err
	}

	// Apply updates if needed
	if updateNeeded {
		_, err := clients.EKSClient.UpdateNodegroupConfig(ctx, updateInput)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to update node group configuration: %w", err)
		}

		// Wait for update to complete
		waiter := eks.NewNodegroupActiveWaiter(clients.EKSClient)
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   aws.String(clusterName),
			NodegroupName: aws.String(fullNodeGroupName),
		}

		waitCtx, cancel := context.WithTimeout(ctx, NodeGroupUpdateTimeout)
		defer cancel()

		_, err = waiter.WaitForOutput(waitCtx, describeInput, NodeGroupUpdateTimeout)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed waiting for node group update: %w", err)
		}

		span.SetAttributes(attribute.Bool("update_applied", true))
	} else {
		span.SetAttributes(attribute.Bool("update_applied", false))
	}

	return nil
}

// checkLabelsUpdate checks if labels need to be updated
func (p *Provider) checkLabelsUpdate(desired config.AWSNodeGroup, actual *NodeGroupState) bool {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.checkLabelsUpdate")
	defer span.End()

	// Check required labels
	requiredLabels := map[string]string{
		"node-group": actual.Tags[TagNodePool], // Should match node pool name from tags
	}

	for key, expectedValue := range requiredLabels {
		actualValue, ok := actual.Labels[key]
		if !ok || actualValue != expectedValue {
			span.SetAttributes(
				attribute.String("missing_or_incorrect_label", key),
			)
			return true
		}
	}

	return false
}

// checkTaintsUpdate checks if taints need to be updated
func (p *Provider) checkTaintsUpdate(desired config.AWSNodeGroup, actual *NodeGroupState) bool {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.checkTaintsUpdate")
	defer span.End()

	// If desired taints count doesn't match actual, update needed
	if len(desired.Taints) != len(actual.Taints) {
		span.SetAttributes(
			attribute.Int("desired_taints_count", len(desired.Taints)),
			attribute.Int("actual_taints_count", len(actual.Taints)),
		)
		return true
	}

	// Check each desired taint exists in actual
	for _, desiredTaint := range desired.Taints {
		found := false
		for _, actualTaint := range actual.Taints {
			if desiredTaint.Key == actualTaint.Key &&
				desiredTaint.Value == actualTaint.Value &&
				desiredTaint.Effect == actualTaint.Effect {
				found = true
				break
			}
		}
		if !found {
			span.SetAttributes(
				attribute.String("missing_taint", fmt.Sprintf("%s=%s:%s", desiredTaint.Key, desiredTaint.Value, desiredTaint.Effect)),
			)
			return true
		}
	}

	return false
}
