package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// reconcileNodeGroups reconciles desired node group configuration with actual state
// Node groups are created sequentially to avoid AWS rate limits and provide clear progress visibility
// Continues processing all node groups even if some fail, providing a summary at the end
// Note: Pure orchestration function - delegates to createNodeGroup() and reconcileNodeGroup().
// Unit test coverage via helper functions.
func (p *Provider) reconcileNodeGroups(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, cluster *ClusterState, iamRoles *IAMRoles, actual []NodeGroupState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.reconcileNodeGroups")
	defer span.End()

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Int("desired_node_groups", len(awsCfg.NodeGroups)),
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

	// Track successes and failures
	var successfulNodeGroups []string
	var failedNodeGroups []string
	nodeGroupErrors := make(map[string]error)

	// Reconcile each desired node group sequentially
	nodeGroupIndex := 0
	for nodeGroupName, nodeGroupConfig := range awsCfg.NodeGroups {
		nodeGroupIndex++
		actualNG, exists := actualNodeGroups[nodeGroupName]

		if !exists {
			// Node group doesn't exist - create it
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.action", nodeGroupName), "create"),
			)

			status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Creating node group '%s' (%d/%d)", nodeGroupName, nodeGroupIndex, len(awsCfg.NodeGroups))).
				WithResource("node-group").
				WithAction("creating").
				WithMetadata("node_group", nodeGroupName).
				WithMetadata("instance_type", nodeGroupConfig.Instance).
				WithMetadata("min_nodes", nodeGroupConfig.MinNodes).
				WithMetadata("max_nodes", nodeGroupConfig.MaxNodes))

			_, err := p.createNodeGroup(ctx, clients, cfg, vpc, cluster, iamRoles, nodeGroupName, nodeGroupConfig)
			if err != nil {
				span.RecordError(err)
				status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to create node group '%s': %v", nodeGroupName, err)).
					WithResource("node-group").
					WithAction("failed").
					WithMetadata("node_group", nodeGroupName))
				failedNodeGroups = append(failedNodeGroups, nodeGroupName)
				nodeGroupErrors[nodeGroupName] = err
				// Continue to next node group instead of returning
				continue
			}

			status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Node group '%s' created and active", nodeGroupName)).
				WithResource("node-group").
				WithAction("created").
				WithMetadata("node_group", nodeGroupName))
			successfulNodeGroups = append(successfulNodeGroups, nodeGroupName)
		} else {
			// Node group exists - check if updates are needed
			span.SetAttributes(
				attribute.String(fmt.Sprintf("node_group.%s.action", nodeGroupName), "update"),
			)

			status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Checking node group '%s' for updates (%d/%d)", nodeGroupName, nodeGroupIndex, len(awsCfg.NodeGroups))).
				WithResource("node-group").
				WithAction("checking").
				WithMetadata("node_group", nodeGroupName))

			err := p.reconcileNodeGroup(ctx, clients, cfg, nodeGroupName, nodeGroupConfig, actualNG)
			if err != nil {
				span.RecordError(err)
				status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to reconcile node group '%s': %v", nodeGroupName, err)).
					WithResource("node-group").
					WithAction("failed").
					WithMetadata("node_group", nodeGroupName))
				failedNodeGroups = append(failedNodeGroups, nodeGroupName)
				nodeGroupErrors[nodeGroupName] = err
				// Continue to next node group instead of returning
				continue
			}

			status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Node group '%s' reconciled", nodeGroupName)).
				WithResource("node-group").
				WithAction("reconciled").
				WithMetadata("node_group", nodeGroupName))
			successfulNodeGroups = append(successfulNodeGroups, nodeGroupName)
		}
	}

	// Handle orphaned node groups (exist in actual but not in desired)
	// These are node groups that were previously created but removed from config
	orphanedNodeGroups := []string{}
	for nodePoolName, actualNG := range actualNodeGroups {
		if _, exists := awsCfg.NodeGroups[nodePoolName]; !exists {
			orphanedNodeGroups = append(orphanedNodeGroups, actualNG.Name)
		}
	}

	var deletedNodeGroups []string
	var failedDeletions []string

	if len(orphanedNodeGroups) > 0 {
		span.SetAttributes(
			attribute.Int("orphaned_node_groups", len(orphanedNodeGroups)),
		)

		// Delete orphaned node groups sequentially
		for i, nodeGroupName := range orphanedNodeGroups {
			span.SetAttributes(
				attribute.String(fmt.Sprintf("orphaned_node_group.%s.action", nodeGroupName), "delete"),
			)

			status.Send(ctx, status.NewUpdate(status.LevelProgress, fmt.Sprintf("Deleting orphaned node group '%s' (%d/%d)", nodeGroupName, i+1, len(orphanedNodeGroups))).
				WithResource("node-group").
				WithAction("deleting").
				WithMetadata("node_group", nodeGroupName))

			err := p.deleteNodeGroup(ctx, clients, clusterName, nodeGroupName)
			if err != nil {
				span.RecordError(err)
				status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Failed to delete orphaned node group '%s': %v", nodeGroupName, err)).
					WithResource("node-group").
					WithAction("failed").
					WithMetadata("node_group", nodeGroupName))
				failedDeletions = append(failedDeletions, nodeGroupName)
				nodeGroupErrors[nodeGroupName] = err
				// Continue to next orphaned node group instead of returning
				continue
			}

			status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Orphaned node group '%s' deleted", nodeGroupName)).
				WithResource("node-group").
				WithAction("deleted").
				WithMetadata("node_group", nodeGroupName))
			deletedNodeGroups = append(deletedNodeGroups, nodeGroupName)
		}
	}

	// Record summary metrics in span
	span.SetAttributes(
		attribute.Bool("sequential_reconciliation", true),
		attribute.Int("successful_node_groups", len(successfulNodeGroups)),
		attribute.Int("failed_node_groups", len(failedNodeGroups)),
		attribute.Int("deleted_orphaned_node_groups", len(deletedNodeGroups)),
		attribute.Int("failed_deletions", len(failedDeletions)),
	)

	// Send summary status update
	if len(failedNodeGroups) > 0 || len(failedDeletions) > 0 {
		// Build error summary message
		summary := fmt.Sprintf("Node group reconciliation completed with errors. Success: %d, Failed: %d",
			len(successfulNodeGroups), len(failedNodeGroups)+len(failedDeletions))

		status.Send(ctx, status.NewUpdate(status.LevelWarning, summary).
			WithResource("node-groups").
			WithAction("summary").
			WithMetadata("successful_count", len(successfulNodeGroups)).
			WithMetadata("failed_count", len(failedNodeGroups)+len(failedDeletions)).
			WithMetadata("successful_node_groups", fmt.Sprintf("%v", successfulNodeGroups)).
			WithMetadata("failed_node_groups", fmt.Sprintf("%v", append(failedNodeGroups, failedDeletions...))))

		// Return error with details about all failures
		var errorMsg string
		if len(failedNodeGroups) > 0 {
			errorMsg = fmt.Sprintf("failed to reconcile %d node group(s): %v", len(failedNodeGroups), failedNodeGroups)
		}
		if len(failedDeletions) > 0 {
			if errorMsg != "" {
				errorMsg += "; "
			}
			errorMsg += fmt.Sprintf("failed to delete %d orphaned node group(s): %v", len(failedDeletions), failedDeletions)
		}
		return fmt.Errorf("%s", errorMsg)
	}

	// All node groups succeeded
	if len(successfulNodeGroups) > 0 || len(deletedNodeGroups) > 0 {
		summary := fmt.Sprintf("Node group reconciliation completed successfully. Reconciled: %d, Deleted: %d",
			len(successfulNodeGroups), len(deletedNodeGroups))

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, summary).
			WithResource("node-groups").
			WithAction("summary").
			WithMetadata("successful_count", len(successfulNodeGroups)).
			WithMetadata("deleted_count", len(deletedNodeGroups)))
	}

	return nil
}

// reconcileNodeGroup reconciles a single node group
// Note: Pure orchestration function - delegates to update functions based on diffs.
// Unit test coverage via helper functions (checkLabelsUpdate, checkTaintsUpdate, updateNodeGroupScaling).
func (p *Provider) reconcileNodeGroup(ctx context.Context, clients *Clients, cfg *config.NebariConfig, nodeGroupName string, desired NodeGroup, actual *NodeGroupState) error {
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

	// Check for capacity type (Spot) changes (also immutable)
	desiredCapacityType := string(DefaultCapacityType) // ON_DEMAND
	if desired.Spot {
		desiredCapacityType = string(ekstypes.CapacityTypesSpot)
	}

	if actual.CapacityType != desiredCapacityType {
		err := fmt.Errorf("node group capacity type (Spot) is immutable and cannot be changed (current: %s, desired: %s). Manual intervention required - destroy and recreate node group", actual.CapacityType, desiredCapacityType)
		span.RecordError(err)
		span.SetAttributes(
			attribute.String("current_capacity_type", actual.CapacityType),
			attribute.String("desired_capacity_type", desiredCapacityType),
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
func (p *Provider) checkLabelsUpdate(desired NodeGroup, actual *NodeGroupState) bool {
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
func (p *Provider) checkTaintsUpdate(desired NodeGroup, actual *NodeGroupState) bool {
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
