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

// TestCleanupOrphanedEIPsWithCount tests the cleanupOrphanedEIPsWithCount function
func TestCleanupOrphanedEIPsWithCount(t *testing.T) {
	tests := []struct {
		name          string
		clusterName   string
		mockSetup     func(*MockEC2Client)
		expectedCount int
		expectError   bool
		errorMsg      string
	}{
		{
			name:        "no EIPs found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{},
					}, nil
				}
			},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:        "release unassociated EIPs",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{
							{AllocationId: aws.String("eipalloc-1"), AssociationId: nil},
							{AllocationId: aws.String("eipalloc-2"), AssociationId: nil},
						},
					}, nil
				}
				m.ReleaseAddressFunc = func(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
					return &ec2.ReleaseAddressOutput{}, nil
				}
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name:        "skip associated EIPs",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return &ec2.DescribeAddressesOutput{
						Addresses: []types.Address{
							{AllocationId: aws.String("eipalloc-1"), AssociationId: aws.String("eipassoc-1")},
							{AllocationId: aws.String("eipalloc-2"), AssociationId: nil},
						},
					}, nil
				}
				m.ReleaseAddressFunc = func(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
					return &ec2.ReleaseAddressOutput{}, nil
				}
			},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:        "error describing EIPs",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeAddressesFunc = func(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
					return nil, fmt.Errorf("AccessDenied")
				}
			},
			expectedCount: 0,
			expectError:   true,
			errorMsg:      "failed to describe cluster EIPs",
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
			count, err := p.cleanupOrphanedEIPsWithCount(ctx, clients, tt.clusterName)

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
			if count != tt.expectedCount {
				t.Errorf("Count = %d, want %d", count, tt.expectedCount)
			}
		})
	}
}

// TestCleanupOrphanedNATGateways tests the cleanupOrphanedNATGateways function
func TestCleanupOrphanedNATGateways(t *testing.T) {
	tests := []struct {
		name          string
		clusterName   string
		mockSetup     func(*MockEC2Client)
		expectedCount int
		expectError   bool
		errorMsg      string
	}{
		{
			name:        "no NAT gateways found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{},
					}, nil
				}
			},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:        "NAT gateway in available state - delete initiated",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{NatGatewayId: aws.String("nat-123"), State: types.NatGatewayStateAvailable},
						},
					}, nil
				}
				m.DeleteNatGatewayFunc = func(ctx context.Context, params *ec2.DeleteNatGatewayInput, optFns ...func(*ec2.Options)) (*ec2.DeleteNatGatewayOutput, error) {
					return &ec2.DeleteNatGatewayOutput{}, nil
				}
			},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:        "NAT gateway already deleted - skip",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{NatGatewayId: aws.String("nat-123"), State: types.NatGatewayStateDeleted},
						},
					}, nil
				}
			},
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:        "NAT gateway in deleting state - counted but not re-deleted",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return &ec2.DescribeNatGatewaysOutput{
						NatGateways: []types.NatGateway{
							{NatGatewayId: aws.String("nat-123"), State: types.NatGatewayStateDeleting},
						},
					}, nil
				}
			},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:        "error describing NAT gateways",
			clusterName: "test-cluster",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeNatGatewaysFunc = func(ctx context.Context, params *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
					return nil, fmt.Errorf("AccessDenied")
				}
			},
			expectedCount: 0,
			expectError:   true,
			errorMsg:      "failed to describe NAT gateways",
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
			count, err := p.cleanupOrphanedNATGateways(ctx, clients, tt.clusterName)

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
			if count != tt.expectedCount {
				t.Errorf("Count = %d, want %d", count, tt.expectedCount)
			}
		})
	}
}

// TestDeleteRouteTables tests the deleteRouteTables function
func TestDeleteRouteTables(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:  "no route tables",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{
						RouteTables: []types.RouteTable{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "skip main route table",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{
						RouteTables: []types.RouteTable{
							{
								RouteTableId: aws.String("rtb-main"),
								Associations: []types.RouteTableAssociation{
									{Main: aws.Bool(true)},
								},
							},
						},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "delete non-main route tables with associations",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{
						RouteTables: []types.RouteTable{
							{
								RouteTableId: aws.String("rtb-private"),
								Associations: []types.RouteTableAssociation{
									{
										RouteTableAssociationId: aws.String("rtbassoc-123"),
										Main:                    aws.Bool(false),
									},
								},
							},
						},
					}, nil
				}
				m.DisassociateRouteTableFunc = func(ctx context.Context, params *ec2.DisassociateRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DisassociateRouteTableOutput, error) {
					return &ec2.DisassociateRouteTableOutput{}, nil
				}
				m.DeleteRouteTableFunc = func(ctx context.Context, params *ec2.DeleteRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error) {
					return &ec2.DeleteRouteTableOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "error describing route tables",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return nil, fmt.Errorf("AccessDenied")
				}
			},
			expectError: true,
			errorMsg:    "failed to describe route tables",
		},
		{
			name:  "error deleting route table",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeRouteTablesFunc = func(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
					return &ec2.DescribeRouteTablesOutput{
						RouteTables: []types.RouteTable{
							{
								RouteTableId: aws.String("rtb-private"),
								Associations: []types.RouteTableAssociation{},
							},
						},
					}, nil
				}
				m.DeleteRouteTableFunc = func(ctx context.Context, params *ec2.DeleteRouteTableInput, optFns ...func(*ec2.Options)) (*ec2.DeleteRouteTableOutput, error) {
					return nil, fmt.Errorf("DependencyViolation")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete route table",
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
			err := p.deleteRouteTables(ctx, clients, tt.vpcID)

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

func TestDeleteVPCEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		vpcID       string
		mockSetup   func(*MockEC2Client)
		expectError bool
		errorMsg    string
	}{
		{
			name:  "no VPC endpoints - success",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcEndpointsFunc = func(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
					return &ec2.DescribeVpcEndpointsOutput{
						VpcEndpoints: []types.VpcEndpoint{},
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "successful deletion of VPC endpoints",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				callCount := 0
				m.DescribeVpcEndpointsFunc = func(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
					callCount++
					// First call: return endpoints for initial discovery
					if callCount == 1 {
						return &ec2.DescribeVpcEndpointsOutput{
							VpcEndpoints: []types.VpcEndpoint{
								{
									VpcEndpointId: aws.String("vpce-123"),
									VpcId:         aws.String("vpc-123"),
									ServiceName:   aws.String("com.amazonaws.us-west-2.s3"),
									State:         types.StateAvailable,
								},
								{
									VpcEndpointId: aws.String("vpce-456"),
									VpcId:         aws.String("vpc-123"),
									ServiceName:   aws.String("com.amazonaws.us-west-2.ec2"),
									State:         types.StateAvailable,
								},
							},
						}, nil
					}
					// Second call (during wait): return deleted state or no endpoints
					return &ec2.DescribeVpcEndpointsOutput{
						VpcEndpoints: []types.VpcEndpoint{},
					}, nil
				}
				m.DeleteVpcEndpointsFunc = func(ctx context.Context, params *ec2.DeleteVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcEndpointsOutput, error) {
					// Verify correct endpoint IDs are passed
					if len(params.VpcEndpointIds) != 2 {
						return nil, fmt.Errorf("expected 2 endpoint IDs, got %d", len(params.VpcEndpointIds))
					}
					return &ec2.DeleteVpcEndpointsOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:  "describe error",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcEndpointsFunc = func(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
					return nil, fmt.Errorf("DescribeVpcEndpoints failed")
				}
			},
			expectError: true,
			errorMsg:    "failed to describe VPC endpoints",
		},
		{
			name:  "delete error",
			vpcID: "vpc-123",
			mockSetup: func(m *MockEC2Client) {
				m.DescribeVpcEndpointsFunc = func(ctx context.Context, params *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
					return &ec2.DescribeVpcEndpointsOutput{
						VpcEndpoints: []types.VpcEndpoint{
							{
								VpcEndpointId: aws.String("vpce-123"),
								VpcId:         aws.String("vpc-123"),
							},
						},
					}, nil
				}
				m.DeleteVpcEndpointsFunc = func(ctx context.Context, params *ec2.DeleteVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteVpcEndpointsOutput, error) {
					return nil, fmt.Errorf("DeleteVpcEndpoints failed")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete VPC endpoints",
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
			err := p.deleteVPCEndpoints(ctx, clients, tt.vpcID)

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
