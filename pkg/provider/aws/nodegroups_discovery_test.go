package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// TestDiscoverNodeGroups tests the DiscoverNodeGroups function using mocks
func TestDiscoverNodeGroups(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockEKSClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, []NodeGroupState)
	}{
		{
			name:        "no node groups found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{}, // Empty list
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, states []NodeGroupState) {
				if len(states) != 0 {
					t.Errorf("Expected 0 node groups, got %d", len(states))
				}
			},
		},
		{
			name:        "single node group with NIC tags",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{"default-nodegroup"},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: aws.String("default-nodegroup"),
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123:nodegroup/test-cluster/default-nodegroup/abc"),
							ClusterName:   aws.String("test-cluster"),
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"t3.medium"},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(1),
								MaxSize:     aws.Int32(3),
								DesiredSize: aws.Int32(2),
							},
							AmiType:      ekstypes.AMITypesAl2X8664,
							CapacityType: ekstypes.CapacityTypesOnDemand,
							Tags: map[string]string{
								TagManagedBy:    ManagedByValue,
								TagClusterName:  "test-cluster",
								TagResourceType: ResourceTypeNodePool,
								TagNodePool:     "default-nodegroup",
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, states []NodeGroupState) {
				if len(states) != 1 {
					t.Fatalf("Expected 1 node group, got %d", len(states))
				}
				ng := states[0]
				if ng.Name != "default-nodegroup" {
					t.Errorf("Name = %v, want default-nodegroup", ng.Name)
				}
				if ng.Status != string(ekstypes.NodegroupStatusActive) {
					t.Errorf("Status = %v, want %v", ng.Status, ekstypes.NodegroupStatusActive)
				}
				if len(ng.InstanceTypes) != 1 || ng.InstanceTypes[0] != "t3.medium" {
					t.Errorf("InstanceTypes = %v, want [t3.medium]", ng.InstanceTypes)
				}
			},
		},
		{
			name:        "multiple node groups - only NIC-managed returned",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{"nic-managed", "external-managed", "no-tags"},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					name := *params.NodegroupName
					switch name {
					case "nic-managed":
						return &eks.DescribeNodegroupOutput{
							Nodegroup: &ekstypes.Nodegroup{
								NodegroupName: aws.String("nic-managed"),
								Status:        ekstypes.NodegroupStatusActive,
								InstanceTypes: []string{"t3.medium"},
								Tags: map[string]string{
									TagManagedBy:   ManagedByValue,
									TagClusterName: "test-cluster",
									TagNodePool:    "nic-managed",
								},
							},
						}, nil
					case "external-managed":
						return &eks.DescribeNodegroupOutput{
							Nodegroup: &ekstypes.Nodegroup{
								NodegroupName: aws.String("external-managed"),
								Status:        ekstypes.NodegroupStatusActive,
								InstanceTypes: []string{"t3.large"},
								Tags: map[string]string{
									"managed-by": "external-tool",
								},
							},
						}, nil
					case "no-tags":
						return &eks.DescribeNodegroupOutput{
							Nodegroup: &ekstypes.Nodegroup{
								NodegroupName: aws.String("no-tags"),
								Status:        ekstypes.NodegroupStatusActive,
								InstanceTypes: []string{"t3.small"},
								Tags:          nil,
							},
						}, nil
					}
					return nil, fmt.Errorf("unknown nodegroup: %s", name)
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, states []NodeGroupState) {
				if len(states) != 1 {
					t.Fatalf("Expected 1 NIC-managed node group, got %d", len(states))
				}
				if states[0].Name != "nic-managed" {
					t.Errorf("Expected NIC-managed nodegroup, got %s", states[0].Name)
				}
			},
		},
		{
			name:        "node group with labels and taints",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{"gpu-nodegroup"},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: aws.String("gpu-nodegroup"),
							Status:        ekstypes.NodegroupStatusActive,
							InstanceTypes: []string{"g4dn.xlarge"},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(0),
								MaxSize:     aws.Int32(2),
								DesiredSize: aws.Int32(1),
							},
							AmiType:      ekstypes.AMITypesAl2X8664Gpu,
							CapacityType: ekstypes.CapacityTypesSpot,
							Labels: map[string]string{
								"gpu":  "true",
								"role": "ml",
							},
							Taints: []ekstypes.Taint{
								{
									Key:    aws.String("nvidia.com/gpu"),
									Value:  aws.String("true"),
									Effect: ekstypes.TaintEffectNoSchedule,
								},
							},
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
								TagNodePool:    "gpu-nodegroup",
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, states []NodeGroupState) {
				if len(states) != 1 {
					t.Fatalf("Expected 1 node group, got %d", len(states))
				}
				ng := states[0]
				if len(ng.Labels) != 2 {
					t.Errorf("Expected 2 labels, got %d", len(ng.Labels))
				}
				if ng.Labels["gpu"] != "true" {
					t.Errorf("Expected gpu=true label, got %v", ng.Labels["gpu"])
				}
				if len(ng.Taints) != 1 {
					t.Fatalf("Expected 1 taint, got %d", len(ng.Taints))
				}
				if ng.Taints[0].Key != "nvidia.com/gpu" {
					t.Errorf("Taint key = %v, want nvidia.com/gpu", ng.Taints[0].Key)
				}
				if ng.CapacityType != string(ekstypes.CapacityTypesSpot) {
					t.Errorf("CapacityType = %v, want SPOT", ng.CapacityType)
				}
			},
		},
		{
			name:        "ListNodegroups API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return nil, fmt.Errorf("ResourceNotFoundException: Cluster test-cluster not found")
				}
			},
			expectError: true,
			errorMsg:    "failed to list node groups",
		},
		{
			name:        "DescribeNodegroup API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{"failing-nodegroup"},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return nil, fmt.Errorf("InternalError: AWS service error")
				}
			},
			expectError: true,
			errorMsg:    "failed to describe node group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockEKS := &MockEKSClient{}
			tt.mockSetup(mockEKS)

			// Create clients with mock
			clients := &Clients{
				EKSClient: mockEKS,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test DiscoverNodeGroups
			ctx := context.Background()
			states, err := p.DiscoverNodeGroups(ctx, clients, tt.clusterName)

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
				tt.validateResult(t, states)
			}
		})
	}
}
