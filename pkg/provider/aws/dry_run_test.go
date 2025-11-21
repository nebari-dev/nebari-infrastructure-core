package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestDryRunDeploy tests the dryRunDeploy function
func TestDryRunDeploy(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		mockSetup   func(*MockEC2Client, *MockEKSClient, *MockIAMClient)
		expectError bool
		errorMsg    string
	}{
		{
			name: "no existing infrastructure",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				DryRun:      true,
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
					VPCCIDRBlock:      "10.0.0.0/16",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				// VPC not found
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
				}
				// Cluster not found
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return nil, &ekstypes.ResourceNotFoundException{Message: aws.String("not found")}
				}
				// No node groups
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{}}, nil
				}
				// IAM roles not found
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("not found")}
				}
			},
			expectError: false,
		},
		{
			name: "existing infrastructure with updates needed",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				DryRun:      true,
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.29",
					VPCCIDRBlock:      "10.0.0.0/16",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 2,
							MaxNodes: 5,
						},
						"new-group": {
							Instance: "t3.large",
							MinNodes: 1,
							MaxNodes: 3,
							Spot:     true,
						},
					},
				},
			},
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				// VPC exists
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []ec2types.Vpc{
							{
								VpcId:     aws.String("vpc-12345"),
								CidrBlock: aws.String("10.0.0.0/16"),
								Tags: []ec2types.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
								},
							},
						},
					}, nil
				}
				// Subnets exist
				ec2Mock.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []ec2types.Subnet{
							{SubnetId: aws.String("subnet-pub1"), AvailabilityZone: aws.String("us-west-2a"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/public-elb"), Value: aws.String("1")}}},
							{SubnetId: aws.String("subnet-priv1"), AvailabilityZone: aws.String("us-west-2a"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/internal-elb"), Value: aws.String("1")}}},
						},
					}, nil
				}
				ec2Mock.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []ec2types.InternetGateway{{InternetGatewayId: aws.String("igw-123")}},
					}, nil
				}
				ec2Mock.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []ec2types.NatGateway{{NatGatewayId: aws.String("nat-123"), State: ec2types.NatGatewayStateAvailable}},
					}, nil
				}
				ec2Mock.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{RouteTables: []ec2types.RouteTable{}}, nil
				}
				ec2Mock.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{}}, nil
				}
				// Cluster exists with older version
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    aws.String("test-cluster"),
							Arn:     aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
							Version: aws.String("1.34"),
							Status:  ekstypes.ClusterStatusActive,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				// Existing node group needs updates
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{"test-cluster-ng-general"}}, nil
				}
				eksMock.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: aws.String("test-cluster-ng-general"),
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/test-cluster-ng-general/123"),
							InstanceTypes: []string{"t3.medium"},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(1),
								MaxSize:     aws.Int32(3),
								DesiredSize: aws.Int32(2),
							},
							Status:       ekstypes.NodegroupStatusActive,
							CapacityType: ekstypes.CapacityTypesOnDemand,
							AmiType:      ekstypes.AMITypesAl2X8664,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
								TagNodePool:    "general",
							},
						},
					}, nil
				}
				// IAM roles exist
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/" + *params.RoleName),
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "orphaned node group to delete",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				DryRun:      true,
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
					NodeGroups: map[string]config.AWSNodeGroup{
						"general": {
							Instance: "t3.medium",
							MinNodes: 1,
							MaxNodes: 3,
						},
					},
				},
			},
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				// VPC not found
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
				}
				// Cluster exists
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:    aws.String("test-cluster"),
							Arn:     aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
							Version: aws.String("1.34"),
							Status:  ekstypes.ClusterStatusActive,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				// Has orphaned node group not in config
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{"general", "orphaned-group"}}, nil
				}
				eksMock.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					name := *params.NodegroupName
					// Map AWS node group names to their node pool names (from TagNodePool)
					nodePoolName := name
					if name == "orphaned-group" {
						nodePoolName = "orphaned" // Not in config
					}
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: aws.String(name),
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/" + name + "/123"),
							InstanceTypes: []string{"t3.medium"},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(1),
								MaxSize:     aws.Int32(3),
								DesiredSize: aws.Int32(2),
							},
							Status:       ekstypes.NodegroupStatusActive,
							CapacityType: ekstypes.CapacityTypesOnDemand,
							AmiType:      ekstypes.AMITypesAl2X8664,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
								TagNodePool:    nodePoolName,
							},
						},
					}, nil
				}
				// IAM roles not found
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("not found")}
				}
			},
			expectError: false,
		},
		{
			name: "GPU and spot node groups",
			cfg: &config.NebariConfig{
				ProjectName: "test-cluster",
				DryRun:      true,
				AmazonWebServices: &config.AWSConfig{
					Region:            "us-west-2",
					KubernetesVersion: "1.34",
					NodeGroups: map[string]config.AWSNodeGroup{
						"gpu": {
							Instance: "g4dn.xlarge",
							MinNodes: 0,
							MaxNodes: 2,
							GPU:      true,
						},
						"spot": {
							Instance: "t3.large",
							MinNodes: 1,
							MaxNodes: 5,
							Spot:     true,
						},
					},
				},
			},
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
				}
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return nil, &ekstypes.ResourceNotFoundException{Message: aws.String("not found")}
				}
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{}}, nil
				}
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("not found")}
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock clients
			ec2Mock := &MockEC2Client{}
			eksMock := &MockEKSClient{}
			iamMock := &MockIAMClient{}

			// Setup mocks
			tt.mockSetup(ec2Mock, eksMock, iamMock)

			// Override newClientsFunc to return our mocks
			originalFunc := newClientsFunc
			newClientsFunc = func(ctx context.Context, region string) (*Clients, error) {
				return &Clients{
					EC2Client: ec2Mock,
					EKSClient: eksMock,
					IAMClient: iamMock,
					Region:    region,
				}, nil
			}
			defer func() { newClientsFunc = originalFunc }()

			// Run test
			p := &Provider{}
			err := p.dryRunDeploy(context.Background(), tt.cfg)

			// Check error
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestDryRunDestroy tests the dryRunDestroy function
func TestDryRunDestroy(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		region      string
		mockSetup   func(*MockEC2Client, *MockEKSClient, *MockIAMClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "no infrastructure exists",
			clusterName: "test-cluster",
			region:      "us-west-2",
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
				}
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return nil, &ekstypes.ResourceNotFoundException{Message: aws.String("not found")}
				}
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{}}, nil
				}
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("not found")}
				}
			},
			expectError: false,
		},
		{
			name:        "full infrastructure exists",
			clusterName: "test-cluster",
			region:      "us-west-2",
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				// VPC exists
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []ec2types.Vpc{
							{
								VpcId:     aws.String("vpc-12345"),
								CidrBlock: aws.String("10.0.0.0/16"),
								Tags: []ec2types.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
								},
							},
						},
					}, nil
				}
				ec2Mock.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []ec2types.Subnet{
							{SubnetId: aws.String("subnet-pub1"), AvailabilityZone: aws.String("us-west-2a"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/public-elb"), Value: aws.String("1")}}},
							{SubnetId: aws.String("subnet-pub2"), AvailabilityZone: aws.String("us-west-2b"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/public-elb"), Value: aws.String("1")}}},
							{SubnetId: aws.String("subnet-priv1"), AvailabilityZone: aws.String("us-west-2a"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/internal-elb"), Value: aws.String("1")}}},
							{SubnetId: aws.String("subnet-priv2"), AvailabilityZone: aws.String("us-west-2b"), Tags: []ec2types.Tag{{Key: aws.String("kubernetes.io/role/internal-elb"), Value: aws.String("1")}}},
						},
					}, nil
				}
				ec2Mock.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []ec2types.InternetGateway{{InternetGatewayId: aws.String("igw-123")}},
					}, nil
				}
				ec2Mock.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []ec2types.NatGateway{
							{NatGatewayId: aws.String("nat-123"), State: ec2types.NatGatewayStateAvailable},
							{NatGatewayId: aws.String("nat-456"), State: ec2types.NatGatewayStateAvailable},
						},
					}, nil
				}
				ec2Mock.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{RouteTables: []ec2types.RouteTable{}}, nil
				}
				ec2Mock.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{}}, nil
				}
				// Cluster exists
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:     aws.String("test-cluster"),
							Arn:      aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
							Version:  aws.String("1.34"),
							Status:   ekstypes.ClusterStatusActive,
							Endpoint: aws.String("https://ABC123.gr7.us-west-2.eks.amazonaws.com"),
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				// Node groups exist
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{"general", "gpu"}}, nil
				}
				eksMock.DescribeNodegroupFunc = func(ctx context.Context, params *eks.DescribeNodegroupInput, optFns ...func(*eks.Options)) (*eks.DescribeNodegroupOutput, error) {
					name := *params.NodegroupName
					instanceType := "t3.medium"
					amiType := ekstypes.AMITypesAl2X8664
					capacityType := ekstypes.CapacityTypesOnDemand
					if name == "gpu" {
						instanceType = "g4dn.xlarge"
						amiType = ekstypes.AMITypesAl2X8664Gpu
						capacityType = ekstypes.CapacityTypesSpot
					}
					return &eks.DescribeNodegroupOutput{
						Nodegroup: &ekstypes.Nodegroup{
							NodegroupName: aws.String(name),
							NodegroupArn:  aws.String("arn:aws:eks:us-west-2:123456789012:nodegroup/test-cluster/" + name + "/123"),
							InstanceTypes: []string{instanceType},
							ScalingConfig: &ekstypes.NodegroupScalingConfig{
								MinSize:     aws.Int32(1),
								MaxSize:     aws.Int32(3),
								DesiredSize: aws.Int32(2),
							},
							Status:       ekstypes.NodegroupStatusActive,
							CapacityType: capacityType,
							AmiType:      amiType,
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				// IAM roles exist
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/" + *params.RoleName),
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:        "cluster only no VPC",
			clusterName: "test-cluster",
			region:      "us-west-2",
			mockSetup: func(ec2Mock *MockEC2Client, eksMock *MockEKSClient, iamMock *MockIAMClient) {
				ec2Mock.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{}}, nil
				}
				eksMock.DescribeClusterFunc = func(ctx context.Context, params *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
					return &eks.DescribeClusterOutput{
						Cluster: &ekstypes.Cluster{
							Name:     aws.String("test-cluster"),
							Arn:      aws.String("arn:aws:eks:us-west-2:123456789012:cluster/test-cluster"),
							Version:  aws.String("1.34"),
							Status:   ekstypes.ClusterStatusActive,
							Endpoint: aws.String("https://ABC123.gr7.us-west-2.eks.amazonaws.com"),
							Tags: map[string]string{
								TagManagedBy:   ManagedByValue,
								TagClusterName: "test-cluster",
							},
						},
					}, nil
				}
				eksMock.ListNodegroupsFunc = func(ctx context.Context, params *eks.ListNodegroupsInput, optFns ...func(*eks.Options)) (*eks.ListNodegroupsOutput, error) {
					return &eks.ListNodegroupsOutput{Nodegroups: []string{}}, nil
				}
				iamMock.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{Message: aws.String("not found")}
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock clients
			ec2Mock := &MockEC2Client{}
			eksMock := &MockEKSClient{}
			iamMock := &MockIAMClient{}

			// Setup mocks
			tt.mockSetup(ec2Mock, eksMock, iamMock)

			// Create clients struct with mocks
			clients := &Clients{
				EC2Client: ec2Mock,
				EKSClient: eksMock,
				IAMClient: iamMock,
				Region:    tt.region,
			}

			// Run test
			p := &Provider{}
			err := p.dryRunDestroy(context.Background(), clients, tt.clusterName, tt.region)

			// Check error
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains([]string{err.Error()}, tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestGetVPCCIDR tests the getVPCCIDR helper function
func TestGetVPCCIDR(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.NebariConfig
		expected string
	}{
		{
			name: "custom CIDR",
			cfg: &config.NebariConfig{
				AmazonWebServices: &config.AWSConfig{
					VPCCIDRBlock: "192.168.0.0/16",
				},
			},
			expected: "192.168.0.0/16",
		},
		{
			name: "default CIDR",
			cfg: &config.NebariConfig{
				AmazonWebServices: &config.AWSConfig{
					VPCCIDRBlock: "",
				},
			},
			expected: "10.0.0.0/16",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getVPCCIDR(tt.cfg)
			if result != tt.expected {
				t.Errorf("getVPCCIDR() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestJoinStrings tests the joinStrings helper function
func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		strs     []string
		sep      string
		expected string
	}{
		{
			name:     "empty slice",
			strs:     []string{},
			sep:      ", ",
			expected: "",
		},
		{
			name:     "single element",
			strs:     []string{"one"},
			sep:      ", ",
			expected: "one",
		},
		{
			name:     "multiple elements",
			strs:     []string{"one", "two", "three"},
			sep:      ", ",
			expected: "one, two, three",
		},
		{
			name:     "different separator",
			strs:     []string{"a", "b"},
			sep:      " | ",
			expected: "a | b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinStrings(tt.strs, tt.sep)
			if result != tt.expected {
				t.Errorf("joinStrings() = %v, want %v", result, tt.expected)
			}
		})
	}
}
