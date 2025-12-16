package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestDiscoverVPC tests the DiscoverVPC function using mocks
func TestDiscoverVPC(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockEC2Client)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, *VPCState)
	}{
		{
			name:        "no VPC found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{}, // Empty - no VPC found
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *VPCState) {
				if state != nil {
					t.Errorf("Expected nil state when no VPC found, got %+v", state)
				}
			},
		},
		{
			name:        "VPC found without resources",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{
							{
								VpcId:     aws.String("vpc-12345"),
								CidrBlock: aws.String("10.0.0.0/16"),
								Tags: []types.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
								},
							},
						},
					}, nil
				}
				// Mock all the discovery functions to return empty
				m.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{Subnets: []types.Subnet{}}, nil
				}
				m.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{InternetGateways: []types.InternetGateway{}}, nil
				}
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{NatGateways: []types.NatGateway{}}, nil
				}
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{RouteTables: []types.RouteTable{}}, nil
				}
				m.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []types.SecurityGroup{}}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *VPCState) {
				if state == nil {
					t.Fatal("Expected VPC state, got nil")
					return
				}
				if state.VPCID != "vpc-12345" {
					t.Errorf("VPCID = %v, want vpc-12345", state.VPCID)
				}
				if state.CIDR != "10.0.0.0/16" {
					t.Errorf("CIDR = %v, want 10.0.0.0/16", state.CIDR)
				}
				if len(state.PublicSubnetIDs) != 0 {
					t.Errorf("Expected 0 public subnets, got %d", len(state.PublicSubnetIDs))
				}
				if len(state.PrivateSubnetIDs) != 0 {
					t.Errorf("Expected 0 private subnets, got %d", len(state.PrivateSubnetIDs))
				}
			},
		},
		{
			name:        "VPC found with full resources",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{
							{
								VpcId:     aws.String("vpc-12345"),
								CidrBlock: aws.String("10.0.0.0/16"),
								Tags: []types.Tag{
									{Key: aws.String(TagManagedBy), Value: aws.String(ManagedByValue)},
									{Key: aws.String(TagClusterName), Value: aws.String("test-cluster")},
								},
							},
						},
					}, nil
				}
				m.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []types.Subnet{
							{
								SubnetId:         aws.String("subnet-public-1"),
								CidrBlock:        aws.String("10.0.0.0/20"),
								AvailabilityZone: aws.String("us-west-2a"),
								Tags: []types.Tag{
									{Key: aws.String("kubernetes.io/role/public-elb"), Value: aws.String("1")},
								},
							},
							{
								SubnetId:         aws.String("subnet-private-1"),
								CidrBlock:        aws.String("10.0.128.0/20"),
								AvailabilityZone: aws.String("us-west-2a"),
								Tags: []types.Tag{
									{Key: aws.String("kubernetes.io/role/internal-elb"), Value: aws.String("1")},
								},
							},
						},
					}, nil
				}
				m.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []types.InternetGateway{
							{
								InternetGatewayId: aws.String("igw-12345"),
							},
						},
					}, nil
				}
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{
								NatGatewayId: aws.String("nat-12345"),
								SubnetId:     aws.String("subnet-public-1"),
								State:        types.NatGatewayStateAvailable,
							},
						},
					}, nil
				}
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{
						RouteTables: []types.RouteTable{
							{
								RouteTableId: aws.String("rtb-public"),
								Tags: []types.Tag{
									{Key: aws.String("Name"), Value: aws.String("test-cluster-public-rt")},
								},
							},
							{
								RouteTableId: aws.String("rtb-private-1"),
								Tags: []types.Tag{
									{Key: aws.String("Name"), Value: aws.String("test-cluster-private-rt-us-west-2a")},
								},
							},
						},
					}, nil
				}
				m.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{
						SecurityGroups: []types.SecurityGroup{
							{
								GroupId:   aws.String("sg-12345"),
								GroupName: aws.String("test-cluster-cluster-sg"),
							},
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *VPCState) {
				if state == nil {
					t.Fatal("Expected VPC state, got nil")
					return
				}
				if state.VPCID != "vpc-12345" {
					t.Errorf("VPCID = %v, want vpc-12345", state.VPCID)
				}
				if len(state.PublicSubnetIDs) != 1 {
					t.Errorf("Expected 1 public subnet, got %d", len(state.PublicSubnetIDs))
				}
				if len(state.PrivateSubnetIDs) != 1 {
					t.Errorf("Expected 1 private subnet, got %d", len(state.PrivateSubnetIDs))
				}
				if state.InternetGatewayID != "igw-12345" {
					t.Errorf("InternetGatewayID = %v, want igw-12345", state.InternetGatewayID)
				}
				if len(state.NATGatewayIDs) != 1 {
					t.Errorf("Expected 1 NAT gateway, got %d", len(state.NATGatewayIDs))
				}
			},
		},
		{
			name:        "multiple VPCs found - error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{
							{VpcId: aws.String("vpc-1"), CidrBlock: aws.String("10.0.0.0/16")},
							{VpcId: aws.String("vpc-2"), CidrBlock: aws.String("10.1.0.0/16")},
						},
					}, nil
				}
			},
			expectError: true,
			errorMsg:    "multiple VPCs found for cluster test-cluster",
		},
		{
			name:        "AWS API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return nil, fmt.Errorf("InternalError: AWS service unavailable")
				}
			},
			expectError: true,
			errorMsg:    "DescribeVpcs API call failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockEC2 := &MockEC2Client{}
			tt.mockSetup(mockEC2)

			// Create clients with mock
			clients := &Clients{
				EC2Client: mockEC2,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test DiscoverVPC
			ctx := context.Background()
			state, err := p.DiscoverVPC(ctx, clients, tt.clusterName)

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
