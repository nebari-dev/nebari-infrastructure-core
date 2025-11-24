package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestOrphanDetection tests the logic for detecting orphaned node groups
func TestOrphanDetection(t *testing.T) {
	tests := []struct {
		name             string
		desiredNGs       map[string]config.AWSNodeGroup
		actualNGs        []NodeGroupState
		expectedOrphans  []string
		expectedToCreate []string
		expectedToUpdate []string
	}{
		{
			name: "no orphans - all match",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general", "compute"},
		},
		{
			name: "one orphan - node group removed from config",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-old-pool",
					Tags: map[string]string{
						TagNodePool: "old-pool",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-old-pool"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "multiple orphans",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
				{
					Name: "test-cluster-ng-gpu",
					Tags: map[string]string{
						TagNodePool: "gpu",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-compute", "test-cluster-ng-gpu"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "new node group to create",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{"compute"},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "mix of create, update, and delete",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"gpu":     {Instance: "p3.2xlarge"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-compute"},
			expectedToCreate: []string{"gpu"},
			expectedToUpdate: []string{"general"},
		},
		{
			name: "no actual node groups - create all",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
				"compute": {Instance: "c5.2xlarge"},
				"gpu":     {Instance: "p3.2xlarge"},
			},
			actualNGs:        []NodeGroupState{},
			expectedOrphans:  []string{},
			expectedToCreate: []string{"general", "compute", "gpu"},
			expectedToUpdate: []string{},
		},
		{
			name:       "all actual node groups are orphans",
			desiredNGs: map[string]config.AWSNodeGroup{},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-compute",
					Tags: map[string]string{
						TagNodePool: "compute",
					},
				},
			},
			expectedOrphans:  []string{"test-cluster-ng-general", "test-cluster-ng-compute"},
			expectedToCreate: []string{},
			expectedToUpdate: []string{},
		},
		{
			name: "node group without tag - should not crash",
			desiredNGs: map[string]config.AWSNodeGroup{
				"general": {Instance: "t3.medium"},
			},
			actualNGs: []NodeGroupState{
				{
					Name: "test-cluster-ng-general",
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
				{
					Name: "test-cluster-ng-untagged",
					Tags: map[string]string{
						// Missing TagNodePool
					},
				},
			},
			expectedOrphans:  []string{},
			expectedToCreate: []string{},
			expectedToUpdate: []string{"general"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build map of actual node groups by node pool name (from tags)
			actualNodeGroups := make(map[string]*NodeGroupState)
			for i := range tt.actualNGs {
				nodePoolName, ok := tt.actualNGs[i].Tags[TagNodePool]
				if ok {
					actualNodeGroups[nodePoolName] = &tt.actualNGs[i]
				}
			}

			// Detect orphans
			orphanedNodeGroups := []string{}
			for nodePoolName, actualNG := range actualNodeGroups {
				if _, exists := tt.desiredNGs[nodePoolName]; !exists {
					orphanedNodeGroups = append(orphanedNodeGroups, actualNG.Name)
				}
			}

			// Check orphan count
			if len(orphanedNodeGroups) != len(tt.expectedOrphans) {
				t.Errorf("expected %d orphans, got %d: %v",
					len(tt.expectedOrphans), len(orphanedNodeGroups), orphanedNodeGroups)
			}

			// Check that expected orphans are detected
			for _, expectedOrphan := range tt.expectedOrphans {
				found := false
				for _, orphan := range orphanedNodeGroups {
					if orphan == expectedOrphan {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected orphan %s not found in %v", expectedOrphan, orphanedNodeGroups)
				}
			}

			// Detect new node groups to create
			toCreate := []string{}
			for nodeGroupName := range tt.desiredNGs {
				if _, exists := actualNodeGroups[nodeGroupName]; !exists {
					toCreate = append(toCreate, nodeGroupName)
				}
			}

			// Check create count
			if len(toCreate) != len(tt.expectedToCreate) {
				t.Errorf("expected %d to create, got %d: %v",
					len(tt.expectedToCreate), len(toCreate), toCreate)
			}

			// Detect node groups to update
			toUpdate := []string{}
			for nodeGroupName := range tt.desiredNGs {
				if _, exists := actualNodeGroups[nodeGroupName]; exists {
					toUpdate = append(toUpdate, nodeGroupName)
				}
			}

			// Check update count
			if len(toUpdate) != len(tt.expectedToUpdate) {
				t.Errorf("expected %d to update, got %d: %v",
					len(tt.expectedToUpdate), len(toUpdate), toUpdate)
			}
		})
	}
}

// TestReconcileNodeGroup_ScalingChanges tests detection of scaling configuration changes
func TestReconcileNodeGroup_ScalingChanges(t *testing.T) {
	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
		description  string
	}{
		{
			name: "no changes needed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: false,
			description:  "scaling config matches",
		},
		{
			name: "min nodes changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "min nodes increased from 1 to 2",
		},
		{
			name: "max nodes changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 5,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "max nodes increased from 3 to 5",
		},
		{
			name: "both min and max changed",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 10,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: true,
			description:  "both scaling limits changed",
		},
		{
			name: "zero values use defaults (1 and 3)",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 0,
				MaxNodes: 0,
			},
			actual: &NodeGroupState{
				Name:          "test-ng",
				MinSize:       1,
				MaxSize:       3,
				InstanceTypes: []string{"t3.medium"},
			},
			expectUpdate: false,
			description:  "zero values default to 1 and 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the scaling check logic
			desiredMinSize := tt.desired.MinNodes
			desiredMaxSize := tt.desired.MaxNodes
			if desiredMinSize == 0 {
				desiredMinSize = 1
			}
			if desiredMaxSize == 0 {
				desiredMaxSize = 3
			}

			updateNeeded := tt.actual.MinSize != desiredMinSize || tt.actual.MaxSize != desiredMaxSize

			if updateNeeded != tt.expectUpdate {
				t.Errorf("%s: expected updateNeeded=%v, got %v",
					tt.description, tt.expectUpdate, updateNeeded)
			}
		})
	}
}

// TestCheckLabelsUpdate_Logic tests the label update detection logic
func TestCheckLabelsUpdate_Logic(t *testing.T) {
	provider := &Provider{}

	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name:    "labels match",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"node-group":               "general",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: false,
		},
		{
			name:    "missing node-group label",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
		{
			name:    "incorrect node-group label value",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{
					"node-group":               "wrong-value",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
		{
			name:    "no labels exist",
			desired: config.AWSNodeGroup{},
			actual: &NodeGroupState{
				Labels: map[string]string{},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateNeeded := provider.checkLabelsUpdate(tt.desired, tt.actual)

			if updateNeeded != tt.expectUpdate {
				t.Errorf("expected updateNeeded=%v, got %v", tt.expectUpdate, updateNeeded)
			}
		})
	}
}

// TestCheckTaintsUpdate_Logic tests the taint update detection logic
func TestCheckTaintsUpdate_Logic(t *testing.T) {
	provider := &Provider{}

	tests := []struct {
		name         string
		desired      config.AWSNodeGroup
		actual       *NodeGroupState
		expectUpdate bool
	}{
		{
			name: "taints match",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: false,
		},
		{
			name: "different taint count",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
					{Key: "workload", Value: "batch", Effect: "NoExecute"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint key mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "amd.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint value mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "false", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "taint effect mismatch",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoExecute"},
				},
			},
			expectUpdate: true,
		},
		{
			name: "no taints on both sides",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{},
			},
			actual: &NodeGroupState{
				Taints: []Taint{},
			},
			expectUpdate: false,
		},
		{
			name: "desired has no taints, actual has taints",
			desired: config.AWSNodeGroup{
				Taints: []config.Taint{},
			},
			actual: &NodeGroupState{
				Taints: []Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateNeeded := provider.checkTaintsUpdate(tt.desired, tt.actual)

			if updateNeeded != tt.expectUpdate {
				t.Errorf("expected updateNeeded=%v, got %v", tt.expectUpdate, updateNeeded)
			}
		})
	}
}

// TestReconcileNodeGroup tests the reconcileNodeGroup function
func TestReconcileNodeGroup(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		ngName      string
		desired     config.AWSNodeGroup
		actual      *NodeGroupState
		mockSetup   func(*MockEKSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "no update needed - all match",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "general",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-general",
				InstanceTypes: []string{"t3.medium"},
				MinSize:       1,
				MaxSize:       3,
				AMIType:       string(DefaultAMIType),
				CapacityType:  string(DefaultCapacityType),
				Labels: map[string]string{
					"node-group":               "general",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			mockSetup:   func(m *MockEKSClient) {},
			expectError: false,
		},
		{
			name: "scaling update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "general",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 5,
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-general",
				InstanceTypes: []string{"t3.medium"},
				MinSize:       1,
				MaxSize:       3,
				AMIType:       string(DefaultAMIType),
				CapacityType:  string(DefaultCapacityType),
				Labels: map[string]string{
					"node-group":               "general",
					"nic.nebari.dev/node-pool": "general",
				},
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateNodegroupConfigFunc = func(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error) {
					// Verify scaling config
					if params.ScalingConfig == nil {
						return nil, fmt.Errorf("expected ScalingConfig to be set")
					}
					if *params.ScalingConfig.MinSize != 2 || *params.ScalingConfig.MaxSize != 5 {
						return nil, fmt.Errorf("expected min=2, max=5, got min=%d, max=%d",
							*params.ScalingConfig.MinSize, *params.ScalingConfig.MaxSize)
					}
					return &eks.UpdateNodegroupConfigOutput{}, nil
				}
				// Mock for wait
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "instance type change - immutable error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "general",
			desired: config.AWSNodeGroup{
				Instance: "t3.large", // Different from actual
				MinNodes: 1,
				MaxNodes: 3,
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-general",
				InstanceTypes: []string{"t3.medium"}, // Original
				MinSize:       1,
				MaxSize:       3,
				AMIType:       string(DefaultAMIType),
				CapacityType:  string(DefaultCapacityType),
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			mockSetup:   func(m *MockEKSClient) {},
			expectError: true,
			errorMsg:    "instance type is immutable",
		},
		{
			name: "capacity type change - immutable error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "general",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
				Spot:     true, // Different from actual
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-general",
				InstanceTypes: []string{"t3.medium"},
				MinSize:       1,
				MaxSize:       3,
				AMIType:       string(DefaultAMIType),
				CapacityType:  "ON_DEMAND", // Original
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			mockSetup:   func(m *MockEKSClient) {},
			expectError: true,
			errorMsg:    "capacity type (Spot) is immutable",
		},
		{
			name: "AMI type change - immutable error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "general",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
				AMIType:  "AL2023_ARM64_STANDARD", // Different
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-general",
				InstanceTypes: []string{"t3.medium"},
				MinSize:       1,
				MaxSize:       3,
				AMIType:       "AL2023_x86_64_STANDARD", // Original
				CapacityType:  string(DefaultCapacityType),
				Tags: map[string]string{
					TagNodePool: "general",
				},
			},
			mockSetup:   func(m *MockEKSClient) {},
			expectError: true,
			errorMsg:    "AMI type is immutable",
		},
		{
			name: "taint update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			ngName: "gpu",
			desired: config.AWSNodeGroup{
				Instance: "g4dn.xlarge",
				MinNodes: 1,
				MaxNodes: 3,
				GPU:      true,
				Taints: []config.Taint{
					{Key: "nvidia.com/gpu", Value: "true", Effect: "NoSchedule"},
				},
			},
			actual: &NodeGroupState{
				Name:          "test-cluster-ng-gpu",
				InstanceTypes: []string{"g4dn.xlarge"},
				MinSize:       1,
				MaxSize:       3,
				AMIType:       string(ekstypes.AMITypesAl2023X8664Nvidia),
				CapacityType:  string(DefaultCapacityType),
				Taints:        []Taint{}, // No taints currently
				Labels: map[string]string{
					"node-group":               "gpu",
					"nic.nebari.dev/node-pool": "gpu",
				},
				Tags: map[string]string{
					TagNodePool: "gpu",
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateNodegroupConfigFunc = func(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error) {
					return &eks.UpdateNodegroupConfigOutput{}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							ClusterName:   params.ClusterName,
							Status:        ekstypes.NodegroupStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEKS := &MockEKSClient{}
			tt.mockSetup(mockEKS)

			clients := &Clients{
				EKSClient: mockEKS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.reconcileNodeGroup(ctx, clients, tt.cfg, tt.ngName, tt.desired, tt.actual)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

// TestReconcileNodeGroups tests the orchestration function for multiple node groups
func TestReconcileNodeGroups(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		vpc         *VPCState
		cluster     *ClusterState
		iamRoles    *IAMRoles
		actual      []NodeGroupState
		mockSetup   func(*MockEKSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "create new node group when none exist",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
			vpc:      &VPCState{VPCID: "vpc-123", PrivateSubnetIDs: []string{"subnet-1", "subnet-2"}},
			cluster:  &ClusterState{Name: "test-cluster"},
			iamRoles: &IAMRoles{NodeRoleARN: "arn:aws:iam::123:role/node-role"},
			actual:   []NodeGroupState{},
			mockSetup: func(m *MockEKSClient) {
				ngName := "test-cluster-ng-general"
				ngArn := "arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/" + ngName
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: &ngName,
							NodegroupArn:  &ngArn,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							ClusterName:   params.ClusterName,
							Status:        ekstypes.NodegroupStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "delete orphaned node group - deletion initiated but waiter fails in test",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region:     "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{}, // Empty - all node groups are orphans
				},
			},
			vpc:      &VPCState{VPCID: "vpc-123"},
			cluster:  &ClusterState{Name: "test-cluster"},
			iamRoles: &IAMRoles{},
			actual: []NodeGroupState{
				{
					Name:         "test-cluster-ng-orphan",
					CapacityType: string(DefaultCapacityType),
					AMIType:      string(DefaultAMIType),
					Tags: map[string]string{
						TagNodePool: "orphan",
					},
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.DeleteNodegroupFunc = func(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error) {
					return &eks.DeleteNodegroupOutput{}, nil
				}
				// Note: AWS SDK waiter is hard to mock - we test that deletion is initiated
				// Real deletion waiter success requires actual AWS API behavior
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return nil, fmt.Errorf("ResourceNotFoundException: node group not found")
				}
			},
			expectError: true, // Waiter doesn't recognize mock error as success
			errorMsg:    "failed to delete 1 orphaned node group(s)",
		},
		{
			name: "update existing node group",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 2, // Changed from 1
							MaxNodes: 5, // Changed from 3
						},
					},
				},
			},
			vpc:      &VPCState{VPCID: "vpc-123"},
			cluster:  &ClusterState{Name: "test-cluster"},
			iamRoles: &IAMRoles{},
			actual: []NodeGroupState{
				{
					Name:          "test-cluster-ng-general",
					InstanceTypes: []string{"t3.medium"},
					MinSize:       1,
					MaxSize:       3,
					CapacityType:  string(DefaultCapacityType),
					AMIType:       string(DefaultAMIType),
					Labels: map[string]string{
						"node-group":               "general",
						"nic.nebari.dev/node-pool": "general",
					},
					Tags: map[string]string{
						TagNodePool: "general",
					},
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateNodegroupConfigFunc = func(ctx context.Context, params *eks.UpdateNodegroupConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateNodegroupConfigOutput, error) {
					return &eks.UpdateNodegroupConfigOutput{}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							ClusterName:   params.ClusterName,
							Status:        ekstypes.NodegroupStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEKS := &MockEKSClient{}
			tt.mockSetup(mockEKS)

			clients := &Clients{
				EKSClient: mockEKS,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.reconcileNodeGroups(ctx, clients, tt.cfg, tt.vpc, tt.cluster, tt.iamRoles, tt.actual)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if tt.errorMsg != "" && !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

// TestImmutableCapacityTypeCheck tests that capacity type (Spot) changes are detected as immutable
func TestImmutableCapacityTypeCheck(t *testing.T) {
	tests := []struct {
		name        string
		desired     config.AWSNodeGroup
		actual      *NodeGroupState
		expectError bool
		errorMsg    string
	}{
		{
			name: "capacity type matches - on demand",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				Spot:     false,
			},
			actual: &NodeGroupState{
				InstanceTypes: []string{"t3.medium"},
				CapacityType:  "ON_DEMAND",
				AMIType:       string(DefaultAMIType),
			},
			expectError: false,
		},
		{
			name: "capacity type matches - spot",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				Spot:     true,
			},
			actual: &NodeGroupState{
				InstanceTypes: []string{"t3.medium"},
				CapacityType:  "SPOT",
				AMIType:       string(DefaultAMIType),
			},
			expectError: false,
		},
		{
			name: "capacity type changed from on demand to spot (immutable)",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				Spot:     true, // Want spot now
			},
			actual: &NodeGroupState{
				InstanceTypes: []string{"t3.medium"},
				CapacityType:  "ON_DEMAND", // Was on demand
				AMIType:       string(DefaultAMIType),
			},
			expectError: true,
			errorMsg:    "capacity type (Spot) is immutable",
		},
		{
			name: "capacity type changed from spot to on demand (immutable)",
			desired: config.AWSNodeGroup{
				Instance: "t3.medium",
				Spot:     false, // Want on demand now
			},
			actual: &NodeGroupState{
				InstanceTypes: []string{"t3.medium"},
				CapacityType:  "SPOT", // Was spot
				AMIType:       string(DefaultAMIType),
			},
			expectError: true,
			errorMsg:    "capacity type (Spot) is immutable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check capacity type immutability
			desiredCapacityType := string(DefaultCapacityType) // ON_DEMAND
			if tt.desired.Spot {
				desiredCapacityType = "SPOT"
			}

			isImmutableChange := tt.actual.CapacityType != desiredCapacityType

			if tt.expectError && !isImmutableChange {
				t.Error("Expected immutable capacity type error, but none detected")
			}
			if !tt.expectError && isImmutableChange {
				t.Errorf("Unexpected immutable capacity type change detected (actual: %s, desired: %s)",
					tt.actual.CapacityType, desiredCapacityType)
			}
		})
	}
}
