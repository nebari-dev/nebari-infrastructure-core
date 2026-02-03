package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// TestCreateVPC tests the createVPC function using mocks
func TestCreateVPC(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *config.NebariConfig
		mockSetup      func(*MockEC2Client)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, *VPCState)
	}{
		{
			name: "successful VPC creation with defaults",
			cfg:  newTestConfig("test-cluster", &Config{Region: "us-west-2"}),
			mockSetup: func(m *MockEC2Client) {
				// Mock availability zones
				m.DescribeAvailabilityZonesFunc = func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
					return &ec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []types.AvailabilityZone{
							{ZoneName: aws.String("us-west-2a")},
							{ZoneName: aws.String("us-west-2b")},
							{ZoneName: aws.String("us-west-2c")},
						},
					}, nil
				}

				// Mock VPC creation
				m.CreateVpcFunc = func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
					return &ec2.CreateVpcOutput{
						Vpc: &types.Vpc{
							VpcId:     aws.String("vpc-12345"),
							CidrBlock: params.CidrBlock,
						},
					}, nil
				}

				// Mock DNS enablement
				m.ModifyVpcAttributeFunc = func(ctx context.Context, params *ec2.ModifyVpcAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyVpcAttributeOutput, error) {
					return &ec2.ModifyVpcAttributeOutput{}, nil
				}

				// Mock Internet Gateway creation
				m.CreateInternetGatewayFunc = func(ctx context.Context, params *ec2.CreateInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error) {
					return &ec2.CreateInternetGatewayOutput{
						InternetGateway: &types.InternetGateway{
							InternetGatewayId: aws.String("igw-12345"),
						},
					}, nil
				}

				m.AttachInternetGatewayFunc = func(ctx context.Context, params *ec2.AttachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error) {
					return &ec2.AttachInternetGatewayOutput{}, nil
				}

				// Mock subnet creation (called twice - public and private)
				subnetCounter := 0
				m.CreateSubnetFunc = func(ctx context.Context, params *ec2.CreateSubnetInput, optFns ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error) {
					subnetCounter++
					return &ec2.CreateSubnetOutput{
						Subnet: &types.Subnet{
							SubnetId:         aws.String(fmt.Sprintf("subnet-%d", subnetCounter)),
							AvailabilityZone: params.AvailabilityZone,
							CidrBlock:        params.CidrBlock,
						},
					}, nil
				}

				// Mock subnet attribute modification (enable auto-assign public IP for public subnets)
				m.ModifySubnetAttributeFunc = func(ctx context.Context, params *ec2.ModifySubnetAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifySubnetAttributeOutput, error) {
					return &ec2.ModifySubnetAttributeOutput{}, nil
				}

				// Mock NAT Gateway creation
				natCounter := 0
				m.CreateNatGatewayFunc = func(ctx context.Context, params *ec2.CreateNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateNatGatewayOutput, error) {
					natCounter++
					return &ec2.CreateNatGatewayOutput{
						NatGateway: &types.NatGateway{
							NatGatewayId: aws.String(fmt.Sprintf("nat-%d", natCounter)),
						},
					}, nil
				}

				// Mock Elastic IP allocation
				eipCounter := 0
				m.AllocateAddressFunc = func(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
					eipCounter++
					return &ec2.AllocateAddressOutput{
						AllocationId: aws.String(fmt.Sprintf("eipalloc-%d", eipCounter)),
					}, nil
				}

				// Mock NAT Gateway waiter (DescribeNatGateways)
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{
								NatGatewayId: &params.NatGatewayIds[0],
								State:        types.NatGatewayStateAvailable,
							},
						},
					}, nil
				}

				// Mock route table creation
				rtbCounter := 0
				m.CreateRouteTableFunc = func(ctx context.Context, params *ec2.CreateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteTableOutput, error) {
					rtbCounter++
					return &ec2.CreateRouteTableOutput{
						RouteTable: &types.RouteTable{
							RouteTableId: aws.String(fmt.Sprintf("rtb-%d", rtbCounter)),
						},
					}, nil
				}

				// Mock route creation
				m.CreateRouteFunc = func(ctx context.Context, params *ec2.CreateRouteInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error) {
					return &ec2.CreateRouteOutput{}, nil
				}

				// Mock route table association
				m.AssociateRouteTableFunc = func(ctx context.Context, params *ec2.AssociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.AssociateRouteTableOutput, error) {
					return &ec2.AssociateRouteTableOutput{
						AssociationId: aws.String("rtbassoc-12345"),
					}, nil
				}

				// Mock security group creation
				m.CreateSecurityGroupFunc = func(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
					return &ec2.CreateSecurityGroupOutput{
						GroupId: aws.String("sg-12345"),
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
				if state.CIDR != DefaultVPCCIDR {
					t.Errorf("CIDR = %v, want %v", state.CIDR, DefaultVPCCIDR)
				}
				if len(state.AvailabilityZones) != 3 {
					t.Errorf("Expected 3 AZs, got %d", len(state.AvailabilityZones))
				}
				if state.InternetGatewayID != "igw-12345" {
					t.Errorf("InternetGatewayID = %v, want igw-12345", state.InternetGatewayID)
				}
				if len(state.PublicSubnetIDs) != 3 {
					t.Errorf("Expected 3 public subnets, got %d", len(state.PublicSubnetIDs))
				}
				if len(state.PrivateSubnetIDs) != 3 {
					t.Errorf("Expected 3 private subnets, got %d", len(state.PrivateSubnetIDs))
				}
				if len(state.NATGatewayIDs) != 3 {
					t.Errorf("Expected 3 NAT gateways, got %d", len(state.NATGatewayIDs))
				}
				if len(state.SecurityGroupIDs) != 1 {
					t.Errorf("Expected 1 security group, got %d", len(state.SecurityGroupIDs))
				}
			},
		},
		{
			name: "VPC creation with custom CIDR",
			cfg:  newTestConfig("test-cluster", &Config{Region: "us-west-2", VPCCIDRBlock: "172.16.0.0/16"}),
			mockSetup: func(m *MockEC2Client) {
				// Setup similar to above but simplified
				m.DescribeAvailabilityZonesFunc = func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
					return &ec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []types.AvailabilityZone{
							{ZoneName: aws.String("us-west-2a")},
							{ZoneName: aws.String("us-west-2b")},
						},
					}, nil
				}
				m.CreateVpcFunc = func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
					return &ec2.CreateVpcOutput{
						Vpc: &types.Vpc{
							VpcId:     aws.String("vpc-custom"),
							CidrBlock: params.CidrBlock,
						},
					}, nil
				}
				m.ModifyVpcAttributeFunc = func(ctx context.Context, params *ec2.ModifyVpcAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyVpcAttributeOutput, error) {
					return &ec2.ModifyVpcAttributeOutput{}, nil
				}
				m.CreateInternetGatewayFunc = func(ctx context.Context, params *ec2.CreateInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error) {
					return &ec2.CreateInternetGatewayOutput{
						InternetGateway: &types.InternetGateway{
							InternetGatewayId: aws.String("igw-custom"),
						},
					}, nil
				}
				m.AttachInternetGatewayFunc = func(ctx context.Context, params *ec2.AttachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.AttachInternetGatewayOutput, error) {
					return &ec2.AttachInternetGatewayOutput{}, nil
				}
				m.CreateSubnetFunc = func(ctx context.Context, params *ec2.CreateSubnetInput, optFns ...func(*ec2.Options)) (*ec2.CreateSubnetOutput, error) {
					return &ec2.CreateSubnetOutput{
						Subnet: &types.Subnet{
							SubnetId:  aws.String("subnet-custom"),
							CidrBlock: params.CidrBlock,
						},
					}, nil
				}
				m.ModifySubnetAttributeFunc = func(ctx context.Context, params *ec2.ModifySubnetAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifySubnetAttributeOutput, error) {
					return &ec2.ModifySubnetAttributeOutput{}, nil
				}
				m.CreateNatGatewayFunc = func(ctx context.Context, params *ec2.CreateNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateNatGatewayOutput, error) {
					return &ec2.CreateNatGatewayOutput{
						NatGateway: &types.NatGateway{
							NatGatewayId: aws.String("nat-custom"),
						},
					}, nil
				}
				m.AllocateAddressFunc = func(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
					return &ec2.AllocateAddressOutput{
						AllocationId: aws.String("eipalloc-custom"),
					}, nil
				}
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{State: types.NatGatewayStateAvailable},
						},
					}, nil
				}
				m.CreateRouteTableFunc = func(ctx context.Context, params *ec2.CreateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteTableOutput, error) {
					return &ec2.CreateRouteTableOutput{
						RouteTable: &types.RouteTable{
							RouteTableId: aws.String("rtb-custom"),
						},
					}, nil
				}
				m.CreateRouteFunc = func(ctx context.Context, params *ec2.CreateRouteInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error) {
					return &ec2.CreateRouteOutput{}, nil
				}
				m.AssociateRouteTableFunc = func(ctx context.Context, params *ec2.AssociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.AssociateRouteTableOutput, error) {
					return &ec2.AssociateRouteTableOutput{
						AssociationId: aws.String("rtbassoc-custom"),
					}, nil
				}
				m.CreateSecurityGroupFunc = func(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
					return &ec2.CreateSecurityGroupOutput{
						GroupId: aws.String("sg-custom"),
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, state *VPCState) {
				if state.CIDR != "172.16.0.0/16" {
					t.Errorf("CIDR = %v, want 172.16.0.0/16", state.CIDR)
				}
			},
		},
		{
			name: "insufficient availability zones",
			cfg:  newTestConfig("test-cluster", &Config{Region: "us-west-2"}),
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAvailabilityZonesFunc = func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
					return &ec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []types.AvailabilityZone{
							{ZoneName: aws.String("us-west-2a")},
						},
					}, nil
				}
			},
			expectError: true,
			errorMsg:    "at least 2 availability zones required",
		},
		{
			name: "VPC creation API error",
			cfg:  newTestConfig("test-cluster", &Config{Region: "us-west-2"}),
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAvailabilityZonesFunc = func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
					return &ec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []types.AvailabilityZone{
							{ZoneName: aws.String("us-west-2a")},
							{ZoneName: aws.String("us-west-2b")},
						},
					}, nil
				}
				m.CreateVpcFunc = func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
					return nil, fmt.Errorf("VpcLimitExceeded: The maximum number of VPCs has been reached")
				}
			},
			expectError: true,
			errorMsg:    "failed to create VPC",
		},
		{
			name: "Internet Gateway creation error",
			cfg:  newTestConfig("test-cluster", &Config{Region: "us-west-2"}),
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAvailabilityZonesFunc = func(ctx context.Context, params *ec2.DescribeAvailabilityZonesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAvailabilityZonesOutput, error) {
					return &ec2.DescribeAvailabilityZonesOutput{
						AvailabilityZones: []types.AvailabilityZone{
							{ZoneName: aws.String("us-west-2a")},
							{ZoneName: aws.String("us-west-2b")},
						},
					}, nil
				}
				m.CreateVpcFunc = func(ctx context.Context, params *ec2.CreateVpcInput, optFns ...func(*ec2.Options)) (*ec2.CreateVpcOutput, error) {
					return &ec2.CreateVpcOutput{
						Vpc: &types.Vpc{
							VpcId:     aws.String("vpc-12345"),
							CidrBlock: params.CidrBlock,
						},
					}, nil
				}
				m.ModifyVpcAttributeFunc = func(ctx context.Context, params *ec2.ModifyVpcAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyVpcAttributeOutput, error) {
					return &ec2.ModifyVpcAttributeOutput{}, nil
				}
				m.CreateInternetGatewayFunc = func(ctx context.Context, params *ec2.CreateInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.CreateInternetGatewayOutput, error) {
					return nil, fmt.Errorf("InternetGatewayLimitExceeded: Limit exceeded")
				}
			},
			expectError: true,
			errorMsg:    "failed to create internet gateway",
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

			// Test createVPC
			ctx := context.Background()
			state, err := p.createVPC(ctx, clients, tt.cfg)

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
