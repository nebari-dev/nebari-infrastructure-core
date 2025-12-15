package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// TestDeleteEKSCluster tests the deleteEKSCluster function using mocks
// Note: We don't test successful deletion with waiter because AWS SDK waiters
// are internal logic that's difficult to mock properly. We test the deletion
// API call through the error case.
func TestDeleteEKSCluster(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		mockSetup   func(*MockEKSClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "cluster doesn't exist - no error",
			clusterName: "nonexistent-cluster",
			mockSetup: func(m *MockEKSClient) {
				// Mock DiscoverCluster - cluster not found
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return nil, &ekstypes.ResourceNotFoundException{
						Message: aws.String("Cluster not found"),
					}
				}
			},
			expectError: false, // Should not error when cluster doesn't exist
		},
		{
			name:        "cluster exists but not managed by NIC - no deletion",
			clusterName: "external-cluster",
			mockSetup: func(m *MockEKSClient) {
				// Mock DiscoverCluster - cluster exists but wrong tags
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusActive,
							Tags: map[string]string{
								"managed-by": "terraform",
							},
						},
					}, nil
				}
			},
			expectError: false, // DiscoverCluster returns nil for non-NIC clusters
		},
		{
			name:        "DeleteCluster API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusActive,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				m.DeleteClusterFunc = func(ctx context.Context, params *eks.DeleteClusterInput, optFns ...func(*eks.Options)) (*eks.DeleteClusterOutput, error) {
					return nil, fmt.Errorf("ResourceInUseException: Cluster has node groups")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete EKS cluster",
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

			// Test deleteEKSCluster
			ctx := context.Background()
			err := p.deleteEKSCluster(ctx, clients, tt.clusterName)

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
