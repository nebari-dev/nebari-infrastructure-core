package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// ResourceTypeNodeGroup is the resource type for EKS node groups
	ResourceTypeNodeGroup = "eks-node-group"

	// DefaultAMIType is the default AMI type for node groups (Amazon Linux 2023 x86_64)
	DefaultAMIType = ekstypes.AMITypesAl2023X8664Standard
	// DefaultCapacityType is the default capacity type for node groups (on-demand instances)
	DefaultCapacityType = ekstypes.CapacityTypesOnDemand
	// DefaultDiskSize is the default disk size in GB for node group instances
	DefaultDiskSize = 20

	// NodeGroupCreateTimeout is the maximum time to wait for node group creation (typically 5-10 minutes)
	NodeGroupCreateTimeout = 15 * time.Minute
	// NodeGroupUpdateTimeout is the maximum time to wait for node group updates
	NodeGroupUpdateTimeout = 15 * time.Minute
	// NodeGroupDeleteTimeout is the maximum time to wait for node group deletion
	NodeGroupDeleteTimeout = 15 * time.Minute
)

// createNodeGroup creates a single EKS node group
func (p *Provider) createNodeGroup(ctx context.Context, clients *Clients, cfg *config.NebariConfig, vpc *VPCState, cluster *ClusterState, iamRoles *IAMRoles, nodeGroupName string, nodeGroupConfig NodeGroup) (*NodeGroupState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createNodeGroup")
	defer span.End()

	clusterName := cfg.ProjectName
	fullNodeGroupName := GenerateResourceName(clusterName, "ng", nodeGroupName)

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("node_group_name", fullNodeGroupName),
		attribute.String("instance_type", nodeGroupConfig.Instance),
	)

	// Generate tags for this node group
	nicTags := GenerateNodePoolTags(ctx, clusterName, nodeGroupName)
	eksTags := convertToEKSTags(nicTags)

	// Set default scaling values if not specified
	minSize := nodeGroupConfig.MinNodes
	maxSize := nodeGroupConfig.MaxNodes
	desiredSize := nodeGroupConfig.MinNodes

	if minSize == 0 {
		minSize = 1
	}
	if maxSize == 0 {
		maxSize = 3
	}
	if desiredSize == 0 {
		desiredSize = minSize
	}

	// Build scaling configuration
	scalingConfig := &ekstypes.NodegroupScalingConfig{
		MinSize:     aws.Int32(int32(minSize)),
		MaxSize:     aws.Int32(int32(maxSize)),
		DesiredSize: aws.Int32(int32(desiredSize)),
	}

	// Build node group labels
	labels := make(map[string]string)
	labels["node-group"] = nodeGroupName
	labels["nic.nebari.dev/node-pool"] = nodeGroupName

	// Convert taints from config format to EKS format
	var eksTaints []ekstypes.Taint
	for _, taint := range nodeGroupConfig.Taints {
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

	// Determine AMI type based on configuration
	amiType := DefaultAMIType

	// Allow explicit AMI type override
	if nodeGroupConfig.AMIType != "" {
		amiType = parseAMIType(nodeGroupConfig.AMIType)
	} else if nodeGroupConfig.GPU {
		// GPU flag defaults to AL2023 NVIDIA AMI
		amiType = ekstypes.AMITypesAl2023X8664Nvidia
	}

	// Determine capacity type based on Spot flag
	capacityType := DefaultCapacityType
	if nodeGroupConfig.Spot {
		capacityType = ekstypes.CapacityTypesSpot
	}

	// Use default disk size (can be extended in config later)
	diskSize := DefaultDiskSize

	// Build create node group input
	createInput := &eks.CreateNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(fullNodeGroupName),
		Subnets:       vpc.PrivateSubnetIDs, // Node groups always use private subnets
		NodeRole:      aws.String(iamRoles.NodeRoleARN),
		ScalingConfig: scalingConfig,
		InstanceTypes: []string{nodeGroupConfig.Instance},
		AmiType:       amiType,
		CapacityType:  capacityType,
		DiskSize:      aws.Int32(int32(diskSize)),
		Labels:        labels,
		Tags:          eksTags,
	}

	// Add taints if specified
	if len(eksTaints) > 0 {
		createInput.Taints = eksTaints
	}

	// Note: Security groups are automatically applied to nodes through the VPC/subnet configuration
	// We don't need to set RemoteAccess unless SSH access is explicitly required
	// The cluster security group allows node-to-node and node-to-control-plane communication

	// Create the node group
	createOutput, err := clients.EKSClient.CreateNodegroup(ctx, createInput)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create EKS node group %s: %w", fullNodeGroupName, err)
	}

	span.SetAttributes(
		attribute.String("node_group_arn", aws.ToString(createOutput.Nodegroup.NodegroupArn)),
		attribute.String("node_group_status", string(createOutput.Nodegroup.Status)),
	)

	// Wait for node group to become active
	waiter := eks.NewNodegroupActiveWaiter(clients.EKSClient)
	describeInput := &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(fullNodeGroupName),
	}

	waitCtx, cancel := context.WithTimeout(ctx, NodeGroupCreateTimeout)
	defer cancel()

	describeOutput, err := waiter.WaitForOutput(waitCtx, describeInput, NodeGroupCreateTimeout)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed waiting for EKS node group %s to become active: %w", fullNodeGroupName, err)
	}

	// Convert to NodeGroupState
	nodeGroupState := convertEKSNodeGroupToState(describeOutput.Nodegroup)

	span.SetAttributes(
		attribute.String("final_status", string(describeOutput.Nodegroup.Status)),
	)

	return nodeGroupState, nil
}

// convertEKSNodeGroupToState converts an EKS node group API response to NodeGroupState
// Note: Pure data transformation - no tracing needed
func convertEKSNodeGroupToState(nodeGroup *ekstypes.Nodegroup) *NodeGroupState {
	state := &NodeGroupState{
		Name:        aws.ToString(nodeGroup.NodegroupName),
		ARN:         aws.ToString(nodeGroup.NodegroupArn),
		ClusterName: aws.ToString(nodeGroup.ClusterName),
		Status:      string(nodeGroup.Status),
	}

	// Instance types
	state.InstanceTypes = nodeGroup.InstanceTypes

	// Scaling configuration
	if nodeGroup.ScalingConfig != nil {
		state.MinSize = int(aws.ToInt32(nodeGroup.ScalingConfig.MinSize))
		state.MaxSize = int(aws.ToInt32(nodeGroup.ScalingConfig.MaxSize))
		state.DesiredSize = int(aws.ToInt32(nodeGroup.ScalingConfig.DesiredSize))
	}

	// Current size from resources
	if nodeGroup.Resources != nil {
		// Count AutoScalingGroups as a proxy for current size
		// The actual instance count comes from the ASG
		state.CurrentSize = state.DesiredSize
	}

	// Subnets
	state.SubnetIDs = nodeGroup.Subnets

	// Node IAM role
	state.NodeRoleARN = aws.ToString(nodeGroup.NodeRole)

	// AMI type
	state.AMIType = string(nodeGroup.AmiType)

	// Disk size
	state.DiskSize = int(aws.ToInt32(nodeGroup.DiskSize))

	// Labels
	state.Labels = nodeGroup.Labels

	// Taints
	for _, taint := range nodeGroup.Taints {
		state.Taints = append(state.Taints, Taint{
			Key:    aws.ToString(taint.Key),
			Value:  aws.ToString(taint.Value),
			Effect: string(taint.Effect),
		})
	}

	// Launch template
	if nodeGroup.LaunchTemplate != nil {
		state.LaunchTemplateID = aws.ToString(nodeGroup.LaunchTemplate.Id)
		state.LaunchTemplateVersion = aws.ToString(nodeGroup.LaunchTemplate.Version)
	}

	// Capacity type
	state.CapacityType = string(nodeGroup.CapacityType)

	// Tags
	state.Tags = nodeGroup.Tags

	// Health
	if nodeGroup.Health != nil && nodeGroup.Health.Issues != nil {
		for _, issue := range nodeGroup.Health.Issues {
			state.Health.Issues = append(state.Health.Issues, string(issue.Code))
		}
	}

	// Timestamps
	if nodeGroup.CreatedAt != nil {
		state.CreatedAt = nodeGroup.CreatedAt.Format(time.RFC3339)
	}
	if nodeGroup.ModifiedAt != nil {
		state.ModifiedAt = nodeGroup.ModifiedAt.Format(time.RFC3339)
	}

	return state
}

// parseAMIType converts string AMI type to ekstypes.AMITypes
// Note: Pure data transformation - no tracing needed
func parseAMIType(amiTypeStr string) ekstypes.AMITypes {

	var amiType ekstypes.AMITypes

	switch amiTypeStr {
	// AL2023 types (preferred)
	case "AL2023_x86_64_STANDARD":
		amiType = ekstypes.AMITypesAl2023X8664Standard
	case "AL2023_ARM_64_STANDARD":
		amiType = ekstypes.AMITypesAl2023Arm64Standard
	case "AL2023_x86_64_NVIDIA":
		amiType = ekstypes.AMITypesAl2023X8664Nvidia
	case "AL2023_ARM_64_NVIDIA":
		amiType = ekstypes.AMITypesAl2023Arm64Nvidia
	case "AL2023_x86_64_NEURON":
		amiType = ekstypes.AMITypesAl2023X8664Neuron

	// AL2 types (legacy support)
	case "AL2_x86_64":
		amiType = ekstypes.AMITypesAl2X8664
	case "AL2_x86_64_GPU":
		amiType = ekstypes.AMITypesAl2X8664Gpu
	case "AL2_ARM_64":
		amiType = ekstypes.AMITypesAl2Arm64

	// Bottlerocket types (for completeness)
	case "BOTTLEROCKET_x86_64":
		amiType = ekstypes.AMITypesBottlerocketX8664
	case "BOTTLEROCKET_ARM_64":
		amiType = ekstypes.AMITypesBottlerocketArm64
	case "BOTTLEROCKET_x86_64_NVIDIA":
		amiType = ekstypes.AMITypesBottlerocketX8664Nvidia
	case "BOTTLEROCKET_ARM_64_NVIDIA":
		amiType = ekstypes.AMITypesBottlerocketArm64Nvidia

	// Windows types (for completeness)
	case "WINDOWS_CORE_2019_x86_64":
		amiType = ekstypes.AMITypesWindowsCore2019X8664
	case "WINDOWS_FULL_2019_x86_64":
		amiType = ekstypes.AMITypesWindowsFull2019X8664
	case "WINDOWS_CORE_2022_x86_64":
		amiType = ekstypes.AMITypesWindowsCore2022X8664
	case "WINDOWS_FULL_2022_x86_64":
		amiType = ekstypes.AMITypesWindowsFull2022X8664

	default:
		// If unknown, use default AL2023
		amiType = DefaultAMIType
	}

	return amiType
}
