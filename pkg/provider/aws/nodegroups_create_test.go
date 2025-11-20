package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestCreateNodeGroup tests the createNodeGroup function using mocks
func TestCreateNodeGroup(t *testing.T) {
	tests := []struct {
		name            string
		cfg             *config.NebariConfig
		vpc             *VPCState
		cluster         *ClusterState
		iamRoles        *IAMRoles
		nodeGroupName   string
		nodeGroupConfig config.AWSNodeGroup
		mockSetup       func(*MockEKSClient)
		expectError     bool
		errorMsg        string
		validateResult  func(*testing.T, *NodeGroupState)
		validateCall    func(*testing.T, *eks.CreateNodegroupInput)
	}{
		{
			name: "basic node group creation",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1", "subnet-2"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "default",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 2,
				MaxNodes: 5,
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123:nodegroup/test-cluster/test-cluster-ng-default/abc"),
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123:nodegroup/test-cluster/test-cluster-ng-default/abc"),
							ClusterName:   aws.String("test-cluster"),
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"t3.medium"},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(2),
								MaxSize:     aws.Int32(5),
								DesiredSize: aws.Int32(2),
							},
							AmiType:      ekstypes.AMITypesAl2X8664,
							CapacityType: ekstypes.CapacityTypesOnDemand,
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *NodeGroupState) {
				if state == nil {
					t.Fatal("Expected NodeGroupState, got nil")
				}
				if state.Status != string(ekstypes.NodegroupStatusActive) {
					t.Errorf("Status = %v, want %v", state.Status, ekstypes.NodegroupStatusActive)
				}
				if len(state.InstanceTypes) != 1 || state.InstanceTypes[0] != "t3.medium" {
					t.Errorf("InstanceTypes = %v, want [t3.medium]", state.InstanceTypes)
				}
			},
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if *input.ClusterName != "test-cluster" {
					t.Errorf("ClusterName = %v, want test-cluster", *input.ClusterName)
				}
				if len(input.Subnets) != 2 {
					t.Errorf("Expected 2 subnets, got %d", len(input.Subnets))
				}
				if input.ScalingConfig.MinSize == nil || *input.ScalingConfig.MinSize != 2 {
					t.Errorf("MinSize = %v, want 2", input.ScalingConfig.MinSize)
				}
				if input.ScalingConfig.MaxSize == nil || *input.ScalingConfig.MaxSize != 5 {
					t.Errorf("MaxSize = %v, want 5", input.ScalingConfig.MaxSize)
				}
				if len(input.InstanceTypes) != 1 || input.InstanceTypes[0] != "t3.medium" {
					t.Errorf("InstanceTypes = %v, want [t3.medium]", input.InstanceTypes)
				}
			},
		},
		{
			name: "node group with GPU",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "gpu",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "g4dn.xlarge",
				MinNodes: 0,
				MaxNodes: 2,
				GPU:      true,
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"g4dn.xlarge"},
							AmiType:       ekstypes.AMITypesAl2023X8664Nvidia,
							CapacityType:  ekstypes.CapacityTypesOnDemand,
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if input.AmiType != ekstypes.AMITypesAl2023X8664Nvidia {
					t.Errorf("AmiType = %v, want AL2023_x86_64_NVIDIA", input.AmiType)
				}
			},
		},
		{
			name: "node group with Spot instances",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "spot",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "t3.large",
				MinNodes: 1,
				MaxNodes: 10,
				Spot:     true,
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							CapacityType:  ekstypes.CapacityTypesSpot,
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if input.CapacityType != ekstypes.CapacityTypesSpot {
					t.Errorf("CapacityType = %v, want SPOT", input.CapacityType)
				}
			},
		},
		{
			name: "node group with taints",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "tainted",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
				Taints: []config.Taint{
					{
						Key:    "dedicated",
						Value:  "ml",
						Effect: "NoSchedule",
					},
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							Taints: []ekstypes.Taint{
								{
									Key:    aws.String("dedicated"),
									Value:  aws.String("ml"),
									Effect: ekstypes.TaintEffectNoSchedule,
								},
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if len(input.Taints) != 1 {
					t.Fatalf("Expected 1 taint, got %d", len(input.Taints))
				}
				if *input.Taints[0].Key != "dedicated" {
					t.Errorf("Taint key = %v, want dedicated", *input.Taints[0].Key)
				}
				if input.Taints[0].Effect != ekstypes.TaintEffectNoSchedule {
					t.Errorf("Taint effect = %v, want NoSchedule", input.Taints[0].Effect)
				}
			},
		},
		{
			name: "node group with default scaling",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "default",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "t3.medium",
				// MinNodes and MaxNodes are 0 - should use defaults
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(1),
								MaxSize:     aws.Int32(3),
								DesiredSize: aws.Int32(1),
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				// Should default to min=1, max=3, desired=1
				if *input.ScalingConfig.MinSize != 1 {
					t.Errorf("Default MinSize = %v, want 1", *input.ScalingConfig.MinSize)
				}
				if *input.ScalingConfig.MaxSize != 3 {
					t.Errorf("Default MaxSize = %v, want 3", *input.ScalingConfig.MaxSize)
				}
				if *input.ScalingConfig.DesiredSize != 1 {
					t.Errorf("Default DesiredSize = %v, want 1", *input.ScalingConfig.DesiredSize)
				}
			},
		},
		{
			name: "node group with explicit AL2023 ARM64 AMI type",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "arm64",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "m7g.xlarge",
				MinNodes: 1,
				MaxNodes: 3,
				AMIType:  "AL2023_ARM_64_STANDARD",
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"m7g.xlarge"},
							AmiType:       ekstypes.AMITypesAl2023Arm64Standard,
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if input.AmiType != ekstypes.AMITypesAl2023Arm64Standard {
					t.Errorf("AmiType = %v, want AL2023_ARM_64_STANDARD", input.AmiType)
				}
			},
		},
		{
			name: "node group with explicit AL2023 Neuron AMI type",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "neuron",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "inf2.xlarge",
				MinNodes: 0,
				MaxNodes: 2,
				AMIType:  "AL2023_x86_64_NEURON",
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return &eks.CreateNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusCreating,
						},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"inf2.xlarge"},
							AmiType:       ekstypes.AMITypesAl2023X8664Neuron,
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateNodegroupInput) {
				if input.AmiType != ekstypes.AMITypesAl2023X8664Neuron {
					t.Errorf("AmiType = %v, want AL2023_x86_64_NEURON", input.AmiType)
				}
			},
		},
		{
			name: "CreateNodegroup API error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
			},
			vpc: &VPCState{
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			cluster: &ClusterState{
				Name: "test-cluster",
			},
			iamRoles: &IAMRoles{
				NodeRoleARN: "arn:aws:iam::123:role/node-role",
			},
			nodeGroupName: "default",
			nodeGroupConfig: config.AWSNodeGroup{
				Instance: "t3.medium",
				MinNodes: 1,
				MaxNodes: 3,
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					return nil, fmt.Errorf("ResourceInUseException: NodeGroup already exists")
				}
			},
			expectError: true,
			errorMsg:    "failed to create EKS node group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockEKS := &MockEKSClient{}
			tt.mockSetup(mockEKS)

			// Capture CreateNodegroup input
			var capturedInput *eks.CreateNodegroupInput
			if tt.validateCall != nil {
				originalFunc := mockEKS.CreateNodegroupFunc
				mockEKS.CreateNodegroupFunc = func(ctx context.Context, params *eks.CreateNodegroupInput, optFns ...func(*eks.Options)) (*eks.CreateNodegroupOutput, error) {
					capturedInput = params
					return originalFunc(ctx, params, optFns...)
				}
			}

			// Create clients with mock
			clients := &Clients{
				EKSClient: mockEKS,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test createNodeGroup
			ctx := context.Background()
			state, err := p.createNodeGroup(ctx, clients, tt.cfg, tt.vpc, tt.cluster, tt.iamRoles, tt.nodeGroupName, tt.nodeGroupConfig)

			// Validate error
			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error containing %q, got nil", tt.errorMsg)
				}
				if !containsSubstring([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Error = %v, want to contain %v", err.Error(), tt.errorMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Validate result
			if tt.validateResult != nil {
				tt.validateResult(t, state)
			}

			// Validate API call
			if tt.validateCall != nil && capturedInput != nil {
				tt.validateCall(t, capturedInput)
			}
		})
	}
}
