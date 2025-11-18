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

// TestCreateEKSCluster tests the createEKSCluster function using mocks
func TestCreateEKSCluster(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.NebariConfig
		vpc          *VPCState
		iamRoles     *IAMRoles
		mockSetup    func(*MockEKSClient)
		expectError  bool
		errorMsg     string
		validateCall func(*testing.T, *eks.CreateClusterInput)
	}{
		{
			name: "minimal configuration",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region: "us-west-2",
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-12345",
				PrivateSubnetIDs: []string{"subnet-1", "subnet-2"},
				SecurityGroupIDs: []string{"sg-12345"},
			},
			iamRoles: &IAMRoles{
				ClusterRoleARN: "arn:aws:iam::123:role/test-cluster-cluster-role",
				NodeRoleARN:    "arn:aws:iam::123:role/test-cluster-node-role",
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateClusterFunc = func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
					return &eks.CreateClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    params.Name,
							Arn:     aws.String("arn:aws:eks:us-west-2:123:cluster/test-cluster"),
							Status:  ekstypes.ClusterStatusCreating,
							Version: params.Version,
						},
					}, nil
				}
				// Mock waiter
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    params.Name,
							Arn:     aws.String("arn:aws:eks:us-west-2:123:cluster/test-cluster"),
							Status:  ekstypes.ClusterStatusActive,
							Version: aws.String(DefaultKubernetesVersion),
							ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
								SubnetIds: []string{"subnet-1", "subnet-2"},
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateClusterInput) {
				if *input.Name != "test-cluster" {
					t.Errorf("Name = %v, want test-cluster", *input.Name)
				}
				if *input.Version != DefaultKubernetesVersion {
					t.Errorf("Version = %v, want %v", *input.Version, DefaultKubernetesVersion)
				}
				if *input.ResourcesVpcConfig.EndpointPublicAccess != DefaultEndpointPublic {
					t.Errorf("EndpointPublicAccess = %v, want %v", *input.ResourcesVpcConfig.EndpointPublicAccess, DefaultEndpointPublic)
				}
				if *input.ResourcesVpcConfig.EndpointPrivateAccess != DefaultEndpointPrivate {
					t.Errorf("EndpointPrivateAccess = %v, want %v", *input.ResourcesVpcConfig.EndpointPrivateAccess, DefaultEndpointPrivate)
				}
				if len(input.Logging.ClusterLogging) != 1 {
					t.Errorf("Expected 1 logging config, got %d", len(input.Logging.ClusterLogging))
				}
				if !*input.Logging.ClusterLogging[0].Enabled {
					t.Error("Expected logging to be enabled")
				}
				if len(input.Logging.ClusterLogging[0].Types) != 5 {
					t.Errorf("Expected 5 log types, got %d", len(input.Logging.ClusterLogging[0].Types))
				}
			},
		},
		{
			name: "with custom kubernetes version",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.29",
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-12345",
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			iamRoles: &IAMRoles{
				ClusterRoleARN: "arn:aws:iam::123:role/cluster-role",
			},
			mockSetup: func(m *MockEKSClient) {
				m.CreateClusterFunc = func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
					return &eks.CreateClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    params.Name,
							Status:  ekstypes.ClusterStatusCreating,
							Version: params.Version,
						},
					}, nil
				}
				m.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    params.Name,
							Status:  ekstypes.ClusterStatusActive,
							Version: aws.String("1.29"),
						},
					}, nil
				}
			},
			expectError: false,
			validateCall: func(t *testing.T, input *eks.CreateClusterInput) {
				if *input.Version != "1.29" {
					t.Errorf("Version = %v, want 1.29", *input.Version)
				}
			},
		},
		{
			name: "with KMS encryption",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:    "us-west-2",
					EKSKMSArn: "arn:aws:kms:us-west-2:123:key/12345",
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-12345",
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			iamRoles: &IAMRoles{
				ClusterRoleARN: "arn:aws:iam::123:role/cluster-role",
			},
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
			validateCall: func(t *testing.T, input *eks.CreateClusterInput) {
				if len(input.EncryptionConfig) != 1 {
					t.Fatalf("Expected 1 encryption config, got %d", len(input.EncryptionConfig))
				}
				if *input.EncryptionConfig[0].Provider.KeyArn != "arn:aws:kms:us-west-2:123:key/12345" {
					t.Errorf("KMS ARN = %v, want arn:aws:kms:us-west-2:123:key/12345", *input.EncryptionConfig[0].Provider.KeyArn)
				}
				if len(input.EncryptionConfig[0].Resources) != 1 || input.EncryptionConfig[0].Resources[0] != "secrets" {
					t.Errorf("Expected encryption for 'secrets', got %v", input.EncryptionConfig[0].Resources)
				}
			},
		},
		{
			name: "with custom public access CIDRs",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				Provider:    "aws",
				AmazonWebServices: &config.AWSConfig{
					Region:               "us-west-2",
					EKSPublicAccessCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
				},
			},
			vpc: &VPCState{
				VPCID:            "vpc-12345",
				PrivateSubnetIDs: []string{"subnet-1"},
			},
			iamRoles: &IAMRoles{
				ClusterRoleARN: "arn:aws:iam::123:role/cluster-role",
			},
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
			validateCall: func(t *testing.T, input *eks.CreateClusterInput) {
				if len(input.ResourcesVpcConfig.PublicAccessCidrs) != 2 {
					t.Fatalf("Expected 2 public access CIDRs, got %d", len(input.ResourcesVpcConfig.PublicAccessCidrs))
				}
				if input.ResourcesVpcConfig.PublicAccessCidrs[0] != "10.0.0.0/8" {
					t.Errorf("CIDR[0] = %v, want 10.0.0.0/8", input.ResourcesVpcConfig.PublicAccessCidrs[0])
				}
			},
		},
		{
			name: "CreateCluster API error",
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
			mockSetup: func(m *MockEKSClient) {
				m.CreateClusterFunc = func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
					return nil, fmt.Errorf("ResourceLimitExceededException: Cluster limit reached")
				}
			},
			expectError: true,
			errorMsg:    "failed to create EKS cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockEKS := &MockEKSClient{}
			tt.mockSetup(mockEKS)

			// Capture the CreateCluster input for validation
			var capturedInput *eks.CreateClusterInput
			originalFunc := mockEKS.CreateClusterFunc
			mockEKS.CreateClusterFunc = func(ctx context.Context, params *eks.CreateClusterInput, optFns ...func(*eks.Options)) (*eks.CreateClusterOutput, error) {
				capturedInput = params
				return originalFunc(ctx, params, optFns...)
			}

			// Create clients with mock
			clients := &Clients{
				EKSClient: mockEKS,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test createEKSCluster
			ctx := context.Background()
			state, err := p.createEKSCluster(ctx, clients, tt.cfg, tt.vpc, tt.iamRoles)

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

			if state == nil {
				t.Fatal("Expected ClusterState, got nil")
			}

			// Validate the API call inputs
			if tt.validateCall != nil && capturedInput != nil {
				tt.validateCall(t, capturedInput)
			}
		})
	}
}
