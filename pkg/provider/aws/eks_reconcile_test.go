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

// TestReconcileCluster tests the reconcileCluster function using mocks
func TestReconcileCluster(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.NebariConfig
		vpc           *VPCState
		iamRoles      *IAMRoles
		actual        *ClusterState
		mockSetup     func(*MockEKSClient)
		expectError   bool
		errorMsg      string
		validateCalls func(*testing.T, *MockEKSClient)
	}{
		{
			name: "cluster doesn't exist - create",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-12345",
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			iamRoles: &IAMRoles{
				ClusterRoleARN: "arn:aws:iam::123:role/cluster-role",
			},
			actual: nil, // Cluster doesn't exist
			mockSetup: func(m *MockEKSClient) {
				m.CreateClusterFunc = func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
					return &eks.CreateClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusCreating,
						},
					}, nil
				}
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
			validateCalls: func(t *testing.T, m *MockEKSClient) {
				// CreateCluster should have been called
				if m.CreateClusterFunc == nil {
					t.Error("CreateCluster should have been called")
				}
			},
		},
		{
			name: "cluster exists - no update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:            "test-cluster",
				Version:         "1.34",
				VPCID:           "vpc-12345",
				EndpointPublic:  true,
				EndpointPrivate: false,
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
					string(ekstypes.LogTypeAuthenticator),
					string(ekstypes.LogTypeControllerManager),
					string(ekstypes.LogTypeScheduler),
				},
			},
			mockSetup: func(m *MockEKSClient) {
				// No API calls should be made
			},
			expectError: false,
		},
		{
			name: "cluster exists - version update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:            "test-cluster",
				Version:         "1.33",
				VPCID:           "vpc-12345",
				EndpointPublic:  true,
				EndpointPrivate: false,
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
					string(ekstypes.LogTypeAuthenticator),
					string(ekstypes.LogTypeControllerManager),
					string(ekstypes.LogTypeScheduler),
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateClusterVersionFunc = func(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error) {
					return &eks.UpdateClusterVersionOutput{}, nil
				}
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    params.Name,
							Status:  ekstypes.ClusterStatusActive,
							Version: aws.String("1.34"),
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "cluster exists - endpoint access update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
					EKSEndpointAccess: "private",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:            "test-cluster",
				Version:         "1.34",
				VPCID:           "vpc-12345",
				EndpointPublic:  true,
				EndpointPrivate: false,
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
					string(ekstypes.LogTypeAuthenticator),
					string(ekstypes.LogTypeControllerManager),
					string(ekstypes.LogTypeScheduler),
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateClusterConfigFunc = func(ctx context.Context, params *eks.UpdateClusterConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterConfigOutput, error) {
					return &eks.UpdateClusterConfigOutput{}, nil
				}
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "cluster exists - logging update needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:            "test-cluster",
				Version:         "1.34",
				VPCID:           "vpc-12345",
				EndpointPublic:  true,
				EndpointPrivate: false,
				EnabledLogTypes: []string{}, // No logging enabled
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateClusterConfigFunc = func(ctx context.Context, params *eks.UpdateClusterConfigInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterConfigOutput, error) {
					return &eks.UpdateClusterConfigOutput{}, nil
				}
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:   params.Name,
							Status: ekstypes.ClusterStatusActive,
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "cluster exists - VPC change attempted (immutable)",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-67890", // Different VPC
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:  "test-cluster",
				VPCID: "vpc-12345", // Original VPC
			},
			mockSetup: func(m *MockEKSClient) {
				// No API calls should be made
			},
			expectError: true,
			errorMsg:    "VPC configuration is immutable",
		},
		{
			name: "cluster exists - invalid version upgrade",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.30", // Skip 1.29
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:    "test-cluster",
				Version: "1.34",
				VPCID:   "vpc-12345",
			},
			mockSetup: func(m *MockEKSClient) {
				// No API calls should be made
			},
			expectError: true,
			errorMsg:    "invalid Kubernetes version upgrade",
		},
		{
			name: "UpdateClusterVersion API error",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
				},
			},
			vpc: &VPCState{
				VPCID: "vpc-12345",
			},
			iamRoles: &IAMRoles{},
			actual: &ClusterState{
				Name:            "test-cluster",
				Version:         "1.33",
				VPCID:           "vpc-12345",
				EndpointPublic:  true,
				EndpointPrivate: false,
				EnabledLogTypes: []string{
					string(ekstypes.LogTypeApi),
					string(ekstypes.LogTypeAudit),
					string(ekstypes.LogTypeAuthenticator),
					string(ekstypes.LogTypeControllerManager),
					string(ekstypes.LogTypeScheduler),
				},
			},
			mockSetup: func(m *MockEKSClient) {
				m.UpdateClusterVersionFunc = func(ctx context.Context, params *eks.UpdateClusterVersionInput, optFns ...func(*eks.Options)) (*eks.UpdateClusterVersionOutput, error) {
					return nil, fmt.Errorf("InvalidParameterException: Version not supported")
				}
			},
			expectError: true,
			errorMsg:    "failed to update EKS cluster version",
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

			// Test reconcileCluster
			ctx := context.Background()
			err := p.reconcileCluster(ctx, clients, tt.cfg, tt.vpc, tt.iamRoles, tt.actual)

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

			// Validate API calls if needed
			if tt.validateCalls != nil {
				tt.validateCalls(t, mockEKS)
			}
		})
	}
}
