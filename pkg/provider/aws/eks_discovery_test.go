package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// TestDiscoverCluster tests the DiscoverCluster function using mocks
func TestDiscoverCluster(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockEKSClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, *ClusterState)
	}{
		{
			name:        "cluster found and active",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:     aws.String("test-cluster"),
							Arn:      aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
							Endpoint: aws.String("https://ABC123.eks.us-west-2.amazonaws.com"),
							Version:  aws.String("1.34"),
							Status:   ekstypes.ClusterStatusActive,
							CertificateAuthority: &ekstypes.Certificate{
								Data: aws.String("LS0tLS1CRUdJTi=="),
							},
							ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
								VpcId:     aws.String("vpc-12345"),
								SubnetIds: []string{"subnet-1", "subnet-2"},
							},
							Tags: map[string]string{
								TagManagedBy:    ManagedByValue,
								TagClusterName:  "test-cluster",
								TagResourceType: ResourceTypeEKSCluster,
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *ClusterState) {
				if state == nil {
					t.Fatal("Expected cluster state, got nil")
					return
				}
				if state.Name != "test-cluster" {
					t.Errorf("Name = %v, want test-cluster", state.Name)
				}
				if state.Status != string(ekstypes.ClusterStatusActive) {
					t.Errorf("Status = %v, want %v", state.Status, ekstypes.ClusterStatusActive)
				}
				if state.Version != "1.34" {
					t.Errorf("Version = %v, want 1.34", state.Version)
				}
				if state.VPCID != "vpc-12345" {
					t.Errorf("VPCID = %v, want vpc-12345", state.VPCID)
				}
			},
		},
		{
			name:        "cluster not found",
			clusterName: "nonexistent-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					// AWS SDK returns ResourceNotFoundException for non-existent clusters
					return nil, &ekstypes.ResourceNotFoundException{
						Message: aws.String("No cluster found for name: nonexistent-cluster"),
					}
				}
			},
			expectError: true,
			errorMsg:    "failed to describe EKS cluster nonexistent-cluster",
		},
		{
			name:        "cluster without NIC tags (nil tags)",
			clusterName: "unmanaged-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    aws.String("unmanaged-cluster"),
							Version: aws.String("1.34"),
							Status:  ekstypes.ClusterStatusActive,
							Tags:    nil, // Nil tags
						},
					}, nil
				}
			},
			expectError: true,
			errorMsg:    "cluster unmanaged-cluster exists but is not managed by NIC (no tags)",
		},
		{
			name:        "cluster missing managed-by tag",
			clusterName: "unmanaged-cluster2",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    aws.String("unmanaged-cluster2"),
							Version: aws.String("1.34"),
							Status:  ekstypes.ClusterStatusActive,
							Tags:    map[string]string{"some-tag": "some-value"}, // Tags exist but no NIC tags
						},
					}, nil
				}
			},
			expectError: true,
			errorMsg:    "cluster unmanaged-cluster2 exists but is not managed by NIC (missing or incorrect",
		},
		{
			name:        "cluster creating",
			clusterName: "creating-cluster",
			mockSetup: func(m *MockEKSClient) {
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    aws.String("creating-cluster"),
							Arn:     aws.String("arn:aws:eks:us-west-2:123456789012:cluster/creating-cluster"),
							Version: aws.String("1.34"),
							Status:  ekstypes.ClusterStatusCreating,
							Tags: map[string]string{
								TagManagedBy:    ManagedByValue,
								TagClusterName:  "creating-cluster",
								TagResourceType: ResourceTypeEKSCluster,
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *ClusterState) {
				if state == nil {
					t.Fatal("Expected cluster state, got nil")
					return
				}
				if state.Status != string(ekstypes.ClusterStatusCreating) {
					t.Errorf("Status = %v, want %v", state.Status, ekstypes.ClusterStatusCreating)
				}
			},
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

			// Test DiscoverCluster
			ctx := context.Background()
			state, err := p.DiscoverCluster(ctx, clients, tt.clusterName)

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
		})
	}
}
