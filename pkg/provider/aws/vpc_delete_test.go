package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestDeleteVPC tests the deleteVPC function using mocks
// Note: Comprehensive VPC deletion testing is complex because it requires extensive
// mocking of VPC discovery (including subnets, route tables, IGW, NAT GW, security groups).
// We test the deletion flow through the individual helper function tests below.
func TestDeleteVPC(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "VPC doesn't exist - no error",
			clusterName: "nonexistent-cluster",
			mockSetup: func(m *MockEC2Client) {
				// Mock DiscoverVPC - VPC not found
				m.DescribeVpcsFunc = func(ctx context.Context, params *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
					return &ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{},
					}, nil
				}
				// Mock cleanup orphaned EIPs - no EIPs found
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{},
					}, nil
				}
			},
			expectError: false,
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

			// Test deleteVPC
			ctx := context.Background()
			err := p.deleteVPC(ctx, clients, tt.clusterName)

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

// TestDeleteNATGateways tests the deleteNATGateways function using mocks
func TestDeleteNATGateways(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		clusterName string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "successful deletion of NAT gateways with EIPs",
			vpcID:       "vpc-123",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				callCount := 0
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					callCount++
					// First call: return NAT Gateways in "available" state
					if callCount == 1 {
						return &ec2.DescribeNatGatewaysOutput{
							NatGateways: []types.NatGateway{
								{
									NatGatewayId: aws.String("nat-123"),
									State:        types.NatGatewayStateAvailable,
									NatGatewayAddresses: []types.NatGatewayAddress{
										{AllocationId: aws.String("eipalloc-123")},
									},
								},
								{
									NatGatewayId: aws.String("nat-456"),
									State:        types.NatGatewayStateAvailable,
									NatGatewayAddresses: []types.NatGatewayAddress{
										{AllocationId: aws.String("eipalloc-456")},
									},
								},
							},
						}, nil
					}
					// Subsequent calls (for waiter): return "deleted" state
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{
								NatGatewayId: aws.String("nat-123"),
								State:        types.NatGatewayStateDeleted,
							},
							{
								NatGatewayId: aws.String("nat-456"),
								State:        types.NatGatewayStateDeleted,
							},
						},
					}, nil
				}
				m.DeleteNatGatewayFunc = func(ctx context.Context, params *ec2.DeleteNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error) {
					return &ec2.DeleteNatGatewayOutput{}, nil
				}
				m.ReleaseAddressFunc = func(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
					return &ec2.ReleaseAddressOutput{}, nil
				}
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					// Return no orphaned EIPs
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:        "no NAT gateways - no error",
			vpcID:       "vpc-123",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{},
					}, nil
				}
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:        "error describing cluster EIPs",
			vpcID:       "vpc-123",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return nil, fmt.Errorf("AccessDenied: Not authorized")
				}
			},
			expectError: true,
			errorMsg:    "failed to describe cluster EIPs",
		},
		{
			name:        "error describing NAT gateways",
			vpcID:       "vpc-123",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{Addresses: []types.Address{}}, nil
				}
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return nil, fmt.Errorf("AccessDenied: Not authorized")
				}
			},
			expectError: true,
			errorMsg:    "failed to describe NAT gateways",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := &MockEC2Client{}
			tt.mockSetup(mockEC2)

			clients := &Clients{
				EC2Client: mockEC2,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.deleteNATGateways(ctx, clients, tt.vpcID, tt.clusterName)

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

// TestDeleteInternetGateway tests the deleteInternetGateway function using mocks
func TestDeleteInternetGateway(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:  "successful internet gateway deletion",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []types.InternetGateway{
							{InternetGatewayId: aws.String("igw-123")},
						},
					}, nil
				}
				m.DetachInternetGatewayFunc = func(ctx context.Context, params *ec2.DetachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error) {
					return &ec2.DetachInternetGatewayOutput{}, nil
				}
				m.DeleteInternetGatewayFunc = func(ctx context.Context, params *ec2.DeleteInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteInternetGatewayOutput, error) {
					return &ec2.DeleteInternetGatewayOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "no internet gateway - no error",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []types.InternetGateway{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "error detaching internet gateway",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeInternetGatewaysFunc = func(ctx context.Context, params *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
					return &ec2.DescribeInternetGatewaysOutput{
						InternetGateways: []types.InternetGateway{
							{InternetGatewayId: aws.String("igw-123")},
						},
					}, nil
				}
				m.DetachInternetGatewayFunc = func(ctx context.Context, params *ec2.DetachInternetGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DetachInternetGatewayOutput, error) {
					return nil, fmt.Errorf("DependencyViolation: Gateway is in use")
				}
			},
			expectError: true,
			errorMsg:    "failed to detach internet gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := &MockEC2Client{}
			tt.mockSetup(mockEC2)

			clients := &Clients{
				EC2Client: mockEC2,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.deleteInternetGateway(ctx, clients, tt.vpcID)

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

// TestDeleteSubnets tests the deleteSubnets function using mocks
func TestDeleteSubnets(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:  "successful deletion of multiple subnets",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []types.Subnet{
							{SubnetId: aws.String("subnet-123")},
							{SubnetId: aws.String("subnet-456")},
							{SubnetId: aws.String("subnet-789")},
						},
					}, nil
				}
				m.DeleteSubnetFunc = func(ctx context.Context, params *ec2.DeleteSubnetInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error) {
					return &ec2.DeleteSubnetOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "no subnets - no error",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []types.Subnet{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "error deleting subnet",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSubnetsFunc = func(ctx context.Context, params *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
					return &ec2.DescribeSubnetsOutput{
						Subnets: []types.Subnet{
							{SubnetId: aws.String("subnet-123")},
						},
					}, nil
				}
				m.DeleteSubnetFunc = func(ctx context.Context, params *ec2.DeleteSubnetInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSubnetOutput, error) {
					return nil, fmt.Errorf("DependencyViolation: Subnet has dependencies")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete subnet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := &MockEC2Client{}
			tt.mockSetup(mockEC2)

			clients := &Clients{
				EC2Client: mockEC2,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.deleteSubnets(ctx, clients, tt.vpcID)

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

// TestDeleteSecurityGroups tests the deleteSecurityGroups function using mocks
func TestDeleteSecurityGroups(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:  "successful deletion of security groups excluding default",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{
						SecurityGroups: []types.SecurityGroup{
							{GroupId: aws.String("sg-default"), GroupName: aws.String("default")},
							{GroupId: aws.String("sg-123"), GroupName: aws.String("eks-cluster-sg")},
							{GroupId: aws.String("sg-456"), GroupName: aws.String("eks-node-sg")},
						},
					}, nil
				}
				m.DeleteSecurityGroupFunc = func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					return &ec2.DeleteSecurityGroupOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "only default security group - no deletion",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{
						SecurityGroups: []types.SecurityGroup{
							{GroupId: aws.String("sg-default"), GroupName: aws.String("default")},
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "error deleting security group",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeSecurityGroupsFunc = func(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
					return &ec2.DescribeSecurityGroupsOutput{
						SecurityGroups: []types.SecurityGroup{
							{GroupId: aws.String("sg-123"), GroupName: aws.String("eks-cluster-sg")},
						},
					}, nil
				}
				m.DeleteSecurityGroupFunc = func(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
					return nil, fmt.Errorf("DependencyViolation: Security group in use")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete security group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockEC2 := &MockEC2Client{}
			tt.mockSetup(mockEC2)

			clients := &Clients{
				EC2Client: mockEC2,
				Region:    "us-west-2",
			}

			p := NewProvider()
			ctx := context.Background()
			err := p.deleteSecurityGroups(ctx, clients, tt.vpcID)

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
