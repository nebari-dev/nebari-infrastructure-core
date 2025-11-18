package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// TestDeleteNodeGroups tests the deleteNodeGroups function using mocks
// Note: We don't test successful deletion with waiter because AWS SDK waiters
// are internal logic that's difficult to mock properly for parallel operations.
// We test the deletion API calls through the error case and discovery logic.
func TestDeleteNodeGroups(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		mockSetup   func(*MockEKSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no node groups exist - no error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				// Mock DiscoverNodeGroups - returns empty list
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:        "error discovering node groups",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return nil, fmt.Errorf("AccessDenied: Not authorized")
				}
			},
			expectError: true,
			errorMsg:    "failed to discover node groups",
		},
		{
			name:        "error deleting one of multiple node groups",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{
						Nodegroups: []string{"worker-ng", "gpu-ng"},
					}, nil
				}
				m.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							ClusterName:   params.ClusterName,
							Status:        ekstypes.NodegroupStatusActive,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				m.DeleteNodegroupFunc = func(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error) {
					// Fail deletion for gpu-ng
					if *params.NodegroupName == "gpu-ng" {
						return nil, fmt.Errorf("ResourceInUseException: Node group has running pods")
					}
					return &eks.DeleteNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: params.NodegroupName,
							ClusterName:   params.ClusterName,
							Status:        ekstypes.NodegroupStatusDeleting,
						},
					}, nil
				}
			},
			expectError: true,
			errorMsg:    "failed to delete node group",
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

			// Test deleteNodeGroups
			ctx := context.Background()
			err := p.deleteNodeGroups(ctx, clients, tt.clusterName)

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
		})
	}
}

// TestDeleteNodeGroup tests the deleteNodeGroup function using mocks
// Note: We don't test successful deletion with waiter because AWS SDK waiters
// are internal logic that's difficult to mock properly. We test the deletion
// API call through the error case.
func TestDeleteNodeGroup(t *testing.T) {
	tests := []struct {
		name          string
		clusterName   string
		nodeGroupName string
		mockSetup     func(*MockEKSClient)
		expectError   bool
		errorMsg      string
	}{
		{
			name:          "DeleteNodegroup API error",
			clusterName:   "test-cluster",
			nodeGroupName: "worker-ng",
			mockSetup: func(m *MockEKSClient) {
				m.DeleteNodegroupFunc = func(ctx context.Context, params *eks.DeleteNodegroupInput, optFns ...func(*eks.Options)) (*eks.DeleteNodegroupOutput, error) {
					return nil, fmt.Errorf("ResourceInUseException: Node group has running pods")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete node group",
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

			// Test deleteNodeGroup
			ctx := context.Background()
			err := p.deleteNodeGroup(ctx, clients, tt.clusterName, tt.nodeGroupName)

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
		})
	}
}
