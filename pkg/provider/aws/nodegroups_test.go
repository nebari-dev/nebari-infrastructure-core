package aws

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestConvertEKSNodeGroupToState(t *testing.T) {
	now := time.Now()

	nodeGroup := &ekstypes.Nodegroup{
		NodegroupName: aws.String("test-ng-worker"),
		NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/test-ng-worker/12345678"),
		ClusterName:   aws.String("test-cluster"),
		Status:        ekstypes.NodegroupStatusActive,
		InstanceTypes: []string{"m5.large", "m5.xlarge"},
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			MinSize:     aws.Int32(1),
			MaxSize:     aws.Int32(5),
			DesiredSize: aws.Int32(3),
		},
		Subnets:  []string{"subnet-1", "subnet-2"},
		NodeRole: aws.String("arn:aws:iam::123456789012:role/test-node-role"),
		AmiType:  ekstypes.AMITypesAl2X8664,
		DiskSize: aws.Int32(20),
		Labels:   map[string]string{"node-group": "worker"},
		Taints: []ekstypes.Taint{
			{
				Key:    aws.String("dedicated"),
				Value:  aws.String("worker"),
				Effect: ekstypes.TaintEffectNoSchedule,
			},
		},
		CapacityType: ekstypes.CapacityTypesOnDemand,
		Tags: map[string]string{
			"Environment": "test",
		},
		CreatedAt:  &now,
		ModifiedAt: &now,
	}

	state := convertEKSNodeGroupToState(nodeGroup)

	if state.Name != "test-ng-worker" {
		t.Errorf("Name = %v, want %v", state.Name, "test-ng-worker")
	}

	if state.ClusterName != "test-cluster" {
		t.Errorf("ClusterName = %v, want %v", state.ClusterName, "test-cluster")
	}

	if state.Status != string(ekstypes.NodegroupStatusActive) {
		t.Errorf("Status = %v, want %v", state.Status, ekstypes.NodegroupStatusActive)
	}

	if len(state.InstanceTypes) != 2 {
		t.Errorf("InstanceTypes length = %v, want %v", len(state.InstanceTypes), 2)
	}

	if state.MinSize != 1 {
		t.Errorf("MinSize = %v, want %v", state.MinSize, 1)
	}

	if state.MaxSize != 5 {
		t.Errorf("MaxSize = %v, want %v", state.MaxSize, 5)
	}

	if state.DesiredSize != 3 {
		t.Errorf("DesiredSize = %v, want %v", state.DesiredSize, 3)
	}

	if state.DiskSize != 20 {
		t.Errorf("DiskSize = %v, want %v", state.DiskSize, 20)
	}

	if state.AMIType != string(ekstypes.AMITypesAl2X8664) {
		t.Errorf("AMIType = %v, want %v", state.AMIType, ekstypes.AMITypesAl2X8664)
	}

	if state.CapacityType != string(ekstypes.CapacityTypesOnDemand) {
		t.Errorf("CapacityType = %v, want %v", state.CapacityType, ekstypes.CapacityTypesOnDemand)
	}

	if len(state.Taints) != 1 {
		t.Errorf("Taints length = %v, want %v", len(state.Taints), 1)
	}

	if state.Taints[0].Key != "dedicated" {
		t.Errorf("Taint key = %v, want %v", state.Taints[0].Key, "dedicated")
	}

	if state.Taints[0].Effect != string(ekstypes.TaintEffectNoSchedule) {
		t.Errorf("Taint effect = %v, want %v", state.Taints[0].Effect, ekstypes.TaintEffectNoSchedule)
	}
}

func TestConvertEKSNodeGroupToState_MinimalNodeGroup(t *testing.T) {
	// Test with minimal node group data
	nodeGroup := &ekstypes.Nodegroup{
		NodegroupName: aws.String("minimal-ng"),
		ClusterName:   aws.String("test-cluster"),
		Status:        ekstypes.NodegroupStatusCreating,
		InstanceTypes: []string{"t3.medium"},
	}

	state := convertEKSNodeGroupToState(nodeGroup)

	if state.Name != "minimal-ng" {
		t.Errorf("Name = %v, want %v", state.Name, "minimal-ng")
	}

	if state.Status != string(ekstypes.NodegroupStatusCreating) {
		t.Errorf("Status = %v, want %v", state.Status, ekstypes.NodegroupStatusCreating)
	}

	// Optional fields should be empty/zero values
	if state.MinSize != 0 {
		t.Errorf("MinSize should be 0 for minimal node group, got %v", state.MinSize)
	}

	if len(state.Taints) != 0 {
		t.Errorf("Taints should be empty for minimal node group, got %v", len(state.Taints))
	}

	if len(state.Labels) != 0 {
		t.Errorf("Labels should be empty for minimal node group, got %v", len(state.Labels))
	}
}

func TestCheckLabelsUpdate(t *testing.T) {
	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name: "no update when labels match",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
			},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"node-group": "worker",
				},
				Tags: map[string]string{
					TagNodePool: "worker",
				},
			},
			expectUpdate: false,
		},
		{
			name: "update needed when labels are missing",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
			},
			actual: &NodeGroupState{
				Labels: map[string]string{},
				Tags: map[string]string{
					TagNodePool: "worker",
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			needsUpdate := p.checkLabelsUpdate(tt.desired, tt.actual)

			if needsUpdate != tt.expectUpdate {
				t.Errorf("checkLabelsUpdate() = %v, want %v", needsUpdate, tt.expectUpdate)
			}
		})
	}
}

func TestCheckTaintsUpdate(t *testing.T) {
	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name: "no update when both have no taints",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
				Taints:   []config.Taint{},
			},
			actual: &NodeGroupState{
				Taints: []Taint{},
			},
			expectUpdate: false,
		},
		{
			name: "no update when taints match",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
				Taints: []config.Taint{
					{
						Key:    "dedicated",
						Value:  "worker",
						Effect: "NoSchedule",
					},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{
						Key:    "dedicated",
						Value:  "worker",
						Effect: "NoSchedule",
					},
				},
			},
			expectUpdate: false,
		},
		{
			name: "update needed when taints don't match",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
				Taints: []config.Taint{
					{
						Key:    "dedicated",
						Value:  "worker",
						Effect: "NoSchedule",
					},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{
						Key:    "dedicated",
						Value:  "gpu",
						Effect: "NoSchedule",
					},
				},
			},
			expectUpdate: true,
		},
		{
			name: "update needed when taint counts differ",
			desired: config.AWSNodeGroup{
				Instance: "m5.large",
				Taints: []config.Taint{
					{
						Key:    "taint1",
						Value:  "value1",
						Effect: "NoSchedule",
					},
					{
						Key:    "taint2",
						Value:  "value2",
						Effect: "NoExecute",
					},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{
						Key:    "taint1",
						Value:  "value1",
						Effect: "NoSchedule",
					},
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			needsUpdate := p.checkTaintsUpdate(tt.desired, tt.actual)

			if needsUpdate != tt.expectUpdate {
				t.Errorf("checkTaintsUpdate() = %v, want %v", needsUpdate, tt.expectUpdate)
			}
		})
	}
}

func TestNodeGroupResourceTypeConstant(t *testing.T) {
	if ResourceTypeNodeGroup != "eks-node-group" {
		t.Errorf("ResourceTypeNodeGroup = %v, want %v", ResourceTypeNodeGroup, "eks-node-group")
	}
}

func TestNodeGroupDefaultConstants(t *testing.T) {
	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"DefaultAMIType", DefaultAMIType, ekstypes.AMITypesAl2X8664},
		{"DefaultCapacityType", DefaultCapacityType, ekstypes.CapacityTypesOnDemand},
		{"DefaultDiskSize", DefaultDiskSize, 20},
		{"NodeGroupCreateTimeout", NodeGroupCreateTimeout, 15 * time.Minute},
		{"NodeGroupUpdateTimeout", NodeGroupUpdateTimeout, 15 * time.Minute},
		{"NodeGroupDeleteTimeout", NodeGroupDeleteTimeout, 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.actual, tt.expected)
			}
		})
	}
}

func TestConvertEKSNodeGroupToState_WithHealthIssues(t *testing.T) {
	nodeGroup := &ekstypes.Nodegroup{
		NodegroupName: aws.String("unhealthy-ng"),
		ClusterName:   aws.String("test-cluster"),
		Status:        ekstypes.NodegroupStatusDegraded,
		InstanceTypes: []string{"m5.large"},
		Health: &ekstypes.NodegroupHealth{
			Issues: []ekstypes.Issue{
				{
					Code: ekstypes.NodegroupIssueCodeAutoScalingGroupNotFound,
				},
				{
					Code: ekstypes.NodegroupIssueCodeEc2SubnetNotFound,
				},
			},
		},
	}

	state := convertEKSNodeGroupToState(nodeGroup)

	if len(state.Health.Issues) != 2 {
		t.Errorf("Health.Issues length = %v, want %v", len(state.Health.Issues), 2)
	}

	if state.Health.Issues[0] != string(ekstypes.NodegroupIssueCodeAutoScalingGroupNotFound) {
		t.Errorf("First health issue = %v, want %v", state.Health.Issues[0], ekstypes.NodegroupIssueCodeAutoScalingGroupNotFound)
	}
}

func TestConvertEKSNodeGroupToState_WithLaunchTemplate(t *testing.T) {
	nodeGroup := &ekstypes.Nodegroup{
		NodegroupName: aws.String("test-ng"),
		ClusterName:   aws.String("test-cluster"),
		Status:        ekstypes.NodegroupStatusActive,
		InstanceTypes: []string{"m5.large"},
		LaunchTemplate: &ekstypes.LaunchTemplateSpecification{
			Id:      aws.String("lt-1234567890abcdef"),
			Version: aws.String("1"),
		},
	}

	state := convertEKSNodeGroupToState(nodeGroup)

	if state.LaunchTemplateID != "lt-1234567890abcdef" {
		t.Errorf("LaunchTemplateID = %v, want %v", state.LaunchTemplateID, "lt-1234567890abcdef")
	}

	if state.LaunchTemplateVersion != "1" {
		t.Errorf("LaunchTemplateVersion = %v, want %v", state.LaunchTemplateVersion, "1")
	}
}

func TestConvertEKSNodeGroupToState_NilValues(t *testing.T) {
	// Test with nil optional fields
	nodeGroup := &ekstypes.Nodegroup{
		NodegroupName: aws.String("test-ng"),
		ClusterName:   aws.String("test-cluster"),
		Status:        ekstypes.NodegroupStatusActive,
		InstanceTypes: []string{"m5.large"},
		// All optional fields nil
		ScalingConfig:  nil,
		Subnets:        nil,
		Labels:         nil,
		Taints:         nil,
		LaunchTemplate: nil,
		Health:         nil,
		Tags:           nil,
		CreatedAt:      nil,
		ModifiedAt:     nil,
	}

	state := convertEKSNodeGroupToState(nodeGroup)

	// Should not panic and should have zero/empty values
	if state.MinSize != 0 {
		t.Error("MinSize should be 0")
	}

	if state.MaxSize != 0 {
		t.Error("MaxSize should be 0")
	}

	if len(state.SubnetIDs) != 0 {
		t.Error("SubnetIDs should be empty")
	}

	if len(state.Labels) != 0 {
		t.Error("Labels should be empty")
	}

	if len(state.Taints) != 0 {
		t.Error("Taints should be empty")
	}

	if len(state.Health.Issues) != 0 {
		t.Error("Health.Issues should be empty")
	}

	if state.CreatedAt != "" {
		t.Error("CreatedAt should be empty")
	}

	if state.ModifiedAt != "" {
		t.Error("ModifiedAt should be empty")
	}
}
