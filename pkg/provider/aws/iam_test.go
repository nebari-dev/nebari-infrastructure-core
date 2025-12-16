package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func TestConvertToIAMTags(t *testing.T) {
	nicTags := map[string]string{
		"nic.nebari.dev/managed-by":    "nic",
		"nic.nebari.dev/cluster-name":  "test-cluster",
		"nic.nebari.dev/resource-type": "iam-cluster-role",
	}

	iamTags := convertToIAMTags(nicTags)

	if len(iamTags) != 3 {
		t.Errorf("Expected 3 IAM tags, got %d", len(iamTags))
	}

	// Check that all keys and values are present
	tagMap := make(map[string]string)
	for _, tag := range iamTags {
		tagMap[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}

	for key, expectedValue := range nicTags {
		actualValue, ok := tagMap[key]
		if !ok {
			t.Errorf("Expected tag key %s not found", key)
		}
		if actualValue != expectedValue {
			t.Errorf("Tag %s = %v, want %v", key, actualValue, expectedValue)
		}
	}
}

func TestConvertToIAMTags_EmptyMap(t *testing.T) {
	nicTags := map[string]string{}
	iamTags := convertToIAMTags(nicTags)

	if len(iamTags) != 0 {
		t.Errorf("Expected 0 IAM tags for empty input, got %d", len(iamTags))
	}
}

func TestConvertToIAMTags_Type(t *testing.T) {
	nicTags := map[string]string{
		"key1": "value1",
	}

	iamTags := convertToIAMTags(nicTags)

	// Verify the type is correct
	var _ = iamTags

	if len(iamTags) != 1 {
		t.Fatalf("Expected 1 tag, got %d", len(iamTags))
	}

	if aws.ToString(iamTags[0].Key) != "key1" {
		t.Errorf("Tag key = %v, want %v", aws.ToString(iamTags[0].Key), "key1")
	}

	if aws.ToString(iamTags[0].Value) != "value1" {
		t.Errorf("Tag value = %v, want %v", aws.ToString(iamTags[0].Value), "value1")
	}
}

func TestEKSClusterManagedPolicies(t *testing.T) {
	expectedPolicies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
		"arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
	}

	if len(eksClusterManagedPolicies) != len(expectedPolicies) {
		t.Errorf("Expected %d cluster managed policies, got %d", len(expectedPolicies), len(eksClusterManagedPolicies))
	}

	for i, expected := range expectedPolicies {
		if eksClusterManagedPolicies[i] != expected {
			t.Errorf("Cluster policy %d = %v, want %v", i, eksClusterManagedPolicies[i], expected)
		}
	}
}

func TestEKSNodeManagedPolicies(t *testing.T) {
	expectedPolicies := []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
	}

	if len(eksNodeManagedPolicies) != len(expectedPolicies) {
		t.Errorf("Expected %d node managed policies, got %d", len(expectedPolicies), len(eksNodeManagedPolicies))
	}

	for i, expected := range expectedPolicies {
		if eksNodeManagedPolicies[i] != expected {
			t.Errorf("Node policy %d = %v, want %v", i, eksNodeManagedPolicies[i], expected)
		}
	}
}

func TestEKSTrustPolicies(t *testing.T) {
	// Validate that trust policies are valid JSON and contain expected principals
	if eksClusterTrustPolicy == "" {
		t.Error("EKS cluster trust policy is empty")
	}

	if eksNodeTrustPolicy == "" {
		t.Error("EKS node trust policy is empty")
	}

	// Basic validation - check for required strings
	requiredClusterStrings := []string{
		"eks.amazonaws.com",
		"sts:AssumeRole",
		"2012-10-17",
	}

	for _, required := range requiredClusterStrings {
		if !containsSubstring([]string{eksClusterTrustPolicy}, required) {
			t.Errorf("EKS cluster trust policy missing required string: %s", required)
		}
	}

	requiredNodeStrings := []string{
		"ec2.amazonaws.com",
		"sts:AssumeRole",
		"2012-10-17",
	}

	for _, required := range requiredNodeStrings {
		if !containsSubstring([]string{eksNodeTrustPolicy}, required) {
			t.Errorf("EKS node trust policy missing required string: %s", required)
		}
	}
}

func TestIAMResourceTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"cluster role", ResourceTypeIAMClusterRole, "iam-cluster-role"},
		{"node role", ResourceTypeIAMNodeRole, "iam-node-role"},
		{"OIDC provider", ResourceTypeIAMOIDCProvider, "iam-oidc-provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s constant = %v, want %v", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

// TestDiscoverIAMRole tests the discoverIAMRole function using mocks
func TestDiscoverIAMRole(t *testing.T) {
	tests := []struct {
		name           string
		roleName       string
		mockSetup      func(*MockIAMClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, string)
	}{
		{
			name:     "role found",
			roleName: "test-cluster-cluster-role",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName:                 aws.String("test-cluster-cluster-role"),
							Arn:                      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
							AssumeRolePolicyDocument: aws.String(eksClusterTrustPolicy),
						},
					}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, arn string) {
				expected := "arn:aws:iam::123456789012:role/test-cluster-cluster-role"
				if arn != expected {
					t.Errorf("ARN = %v, want %v", arn, expected)
				}
			},
		},
		{
			name:     "role not found",
			roleName: "nonexistent-role",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{
						Message: aws.String("The role with name nonexistent-role cannot be found."),
					}
				}
			},
			expectError: true,
			errorMsg:    "role nonexistent-role not found",
		},
		{
			name:     "AWS API error",
			roleName: "test-role",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, fmt.Errorf("ServiceUnavailableException: Service is temporarily unavailable")
				}
			},
			expectError: true,
			errorMsg:    "role test-role not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockIAM := &MockIAMClient{}
			tt.mockSetup(mockIAM)

			// Create clients with mock
			clients := &Clients{
				IAMClient: mockIAM,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test discoverIAMRole
			ctx := context.Background()
			arn, err := p.discoverIAMRole(ctx, clients, tt.roleName)

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
				tt.validateResult(t, arn)
			}
		})
	}
}

// TestDiscoverIAMRoles tests the discoverIAMRoles function using mocks
func TestDiscoverIAMRoles(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockIAMClient)
		expectNil      bool
		validateResult func(*testing.T, *IAMRoles)
	}{
		{
			name:        "both roles found",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					roleName := *params.RoleName
					switch roleName {
					case "test-cluster-cluster-role":
						return &iam.GetRoleOutput{
							Role: &iamtypes.Role{
								RoleName: aws.String("test-cluster-cluster-role"),
								Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
							},
						}, nil
					case "test-cluster-node-role":
						return &iam.GetRoleOutput{
							Role: &iamtypes.Role{
								RoleName: aws.String("test-cluster-node-role"),
								Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-node-role"),
							},
						}, nil
					default:
						return nil, &iamtypes.NoSuchEntityException{}
					}
				}
			},
			expectNil: false,
			validateResult: func(t *testing.T, roles *IAMRoles) {
				if roles == nil {
					t.Fatal("Expected IAMRoles, got nil")
					return
				}
				if roles.ClusterRoleARN != "arn:aws:iam::123456789012:role/test-cluster-cluster-role" {
					t.Errorf("ClusterRoleARN = %v, want arn:aws:iam::123456789012:role/test-cluster-cluster-role", roles.ClusterRoleARN)
				}
				if roles.NodeRoleARN != "arn:aws:iam::123456789012:role/test-cluster-node-role" {
					t.Errorf("NodeRoleARN = %v, want arn:aws:iam::123456789012:role/test-cluster-node-role", roles.NodeRoleARN)
				}
				if roles.ServiceAccountRoles == nil {
					t.Error("ServiceAccountRoles should be initialized")
				}
			},
		},
		{
			name:        "cluster role not found - returns nil",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					// First call (cluster role) fails
					return nil, &iamtypes.NoSuchEntityException{
						Message: aws.String("Role not found"),
					}
				}
			},
			expectNil: true,
		},
		{
			name:        "node role not found - returns nil",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				callCount := 0
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					callCount++
					if callCount == 1 {
						// First call (cluster role) succeeds
						return &iam.GetRoleOutput{
							Role: &iamtypes.Role{
								RoleName: aws.String("test-cluster-cluster-role"),
								Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
							},
						}, nil
					}
					// Second call (node role) fails
					return nil, &iamtypes.NoSuchEntityException{
						Message: aws.String("Role not found"),
					}
				}
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockIAM := &MockIAMClient{}
			tt.mockSetup(mockIAM)

			// Create clients with mock
			clients := &Clients{
				IAMClient: mockIAM,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test discoverIAMRoles
			ctx := context.Background()
			roles, err := p.discoverIAMRoles(ctx, clients, tt.clusterName)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Validate nil expectation
			if tt.expectNil {
				if roles != nil {
					t.Errorf("Expected nil IAMRoles, got %+v", roles)
				}
				return
			}

			// Validate result
			if tt.validateResult != nil {
				tt.validateResult(t, roles)
			}
		})
	}
}

// TestCreateIAMRoles tests the createIAMRoles function using mocks
func TestCreateIAMRoles(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockIAMClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, *IAMRoles)
	}{
		{
			name:        "successful role creation",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				// Mock CreateRole for both cluster and node roles
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					roleName := *params.RoleName
					var arn string
					if containsSubstring([]string{roleName}, "cluster-role") {
						arn = "arn:aws:iam::123456789012:role/test-cluster-cluster-role"
					} else {
						arn = "arn:aws:iam::123456789012:role/test-cluster-node-role"
					}
					return &iam.CreateRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String(arn),
						},
					}, nil
				}

				// Mock AttachRolePolicy
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return &iam.AttachRolePolicyOutput{}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, roles *IAMRoles) {
				if roles == nil {
					t.Fatal("Expected IAMRoles, got nil")
					return
				}
				if roles.ClusterRoleARN != "arn:aws:iam::123456789012:role/test-cluster-cluster-role" {
					t.Errorf("ClusterRoleARN = %v, want test-cluster-cluster-role", roles.ClusterRoleARN)
				}
				if roles.NodeRoleARN != "arn:aws:iam::123456789012:role/test-cluster-node-role" {
					t.Errorf("NodeRoleARN = %v, want test-cluster-node-role", roles.NodeRoleARN)
				}
				if roles.ServiceAccountRoles == nil {
					t.Error("ServiceAccountRoles map should be initialized")
				}
			},
		},
		{
			name:        "cluster role creation failure",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return nil, fmt.Errorf("EntityAlreadyExists: Role already exists")
				}
			},
			expectError: true,
			errorMsg:    "failed to create EKS cluster role",
		},
		{
			name:        "node role creation failure",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				callCount := 0
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					callCount++
					if callCount == 1 {
						// First call (cluster role) succeeds
						return &iam.CreateRoleOutput{
							Role: &iamtypes.Role{
								RoleName: params.RoleName,
								Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
							},
						}, nil
					}
					// Second call (node role) fails
					return nil, fmt.Errorf("EntityAlreadyExists: Role already exists")
				}
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return &iam.AttachRolePolicyOutput{}, nil
				}
			},
			expectError: true,
			errorMsg:    "failed to create EKS node role",
		},
		{
			name:        "policy attachment failure",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return &iam.CreateRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/" + *params.RoleName),
						},
					}, nil
				}
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return nil, fmt.Errorf("NoSuchEntity: Policy not found")
				}
			},
			expectError: true,
			errorMsg:    "failed to attach policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockIAM := &MockIAMClient{}
			tt.mockSetup(mockIAM)

			// Create clients with mock
			clients := &Clients{
				IAMClient: mockIAM,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test createIAMRoles
			ctx := context.Background()
			roles, err := p.createIAMRoles(ctx, clients, tt.clusterName)

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
				tt.validateResult(t, roles)
			}
		})
	}
}

// TestCreateEKSClusterRole tests the createEKSClusterRole function using mocks
func TestCreateEKSClusterRole(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockIAMClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, string)
		validateCall   func(*testing.T, *iam.CreateRoleInput)
	}{
		{
			name:        "successful cluster role creation",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return &iam.CreateRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
						},
					}, nil
				}
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return &iam.AttachRolePolicyOutput{}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, arn string) {
				expected := "arn:aws:iam::123456789012:role/test-cluster-cluster-role"
				if arn != expected {
					t.Errorf("ARN = %v, want %v", arn, expected)
				}
			},
			validateCall: func(t *testing.T, input *iam.CreateRoleInput) {
				if !containsSubstring([]string{*input.RoleName}, "cluster-role") {
					t.Errorf("RoleName = %v, want to contain 'cluster-role'", *input.RoleName)
				}
				if *input.AssumeRolePolicyDocument != eksClusterTrustPolicy {
					t.Error("Expected eks cluster trust policy")
				}
				if len(input.Tags) == 0 {
					t.Error("Expected NIC tags to be set")
				}
			},
		},
		{
			name:        "role creation API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return nil, fmt.Errorf("LimitExceeded: Cannot exceed quota for Roles")
				}
			},
			expectError: true,
			errorMsg:    "failed to create IAM role",
		},
		{
			name:        "policy attachment error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return &iam.CreateRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-cluster-role"),
						},
					}, nil
				}
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return nil, fmt.Errorf("InvalidInput: Invalid policy ARN")
				}
			},
			expectError: true,
			errorMsg:    "failed to attach policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockIAM := &MockIAMClient{}
			tt.mockSetup(mockIAM)

			// Capture CreateRole input
			var capturedInput *iam.CreateRoleInput
			if tt.validateCall != nil {
				originalFunc := mockIAM.CreateRoleFunc
				mockIAM.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					capturedInput = params
					return originalFunc(ctx, params, optFns...)
				}
			}

			// Create clients with mock
			clients := &Clients{
				IAMClient: mockIAM,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test createEKSClusterRole
			ctx := context.Background()
			arn, err := p.createEKSClusterRole(ctx, clients, tt.clusterName)

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
				tt.validateResult(t, arn)
			}

			// Validate API call
			if tt.validateCall != nil && capturedInput != nil {
				tt.validateCall(t, capturedInput)
			}
		})
	}
}

// TestCreateEKSNodeRole tests the createEKSNodeRole function using mocks
func TestCreateEKSNodeRole(t *testing.T) {
	tests := []struct {
		name           string
		clusterName    string
		mockSetup      func(*MockIAMClient)
		expectError    bool
		errorMsg       string
		validateResult func(*testing.T, string)
		validateCall   func(*testing.T, *iam.CreateRoleInput)
	}{
		{
			name:        "successful node role creation",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return &iam.CreateRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123456789012:role/test-cluster-node-role"),
						},
					}, nil
				}
				m.AttachRolePolicyFunc = func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
					return &iam.AttachRolePolicyOutput{}, nil
				}
			},
			expectError: false,
			validateResult: func(t *testing.T, arn string) {
				expected := "arn:aws:iam::123456789012:role/test-cluster-node-role"
				if arn != expected {
					t.Errorf("ARN = %v, want %v", arn, expected)
				}
			},
			validateCall: func(t *testing.T, input *iam.CreateRoleInput) {
				if !containsSubstring([]string{*input.RoleName}, "node-role") {
					t.Errorf("RoleName = %v, want to contain 'node-role'", *input.RoleName)
				}
				if *input.AssumeRolePolicyDocument != eksNodeTrustPolicy {
					t.Error("Expected eks node trust policy")
				}
				if len(input.Tags) == 0 {
					t.Error("Expected NIC tags to be set")
				}
			},
		},
		{
			name:        "role creation API error",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					return nil, fmt.Errorf("EntityAlreadyExists: Role already exists")
				}
			},
			expectError: true,
			errorMsg:    "failed to create IAM role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockIAM := &MockIAMClient{}
			tt.mockSetup(mockIAM)

			// Capture CreateRole input
			var capturedInput *iam.CreateRoleInput
			if tt.validateCall != nil {
				originalFunc := mockIAM.CreateRoleFunc
				mockIAM.CreateRoleFunc = func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
					capturedInput = params
					return originalFunc(ctx, params, optFns...)
				}
			}

			// Create clients with mock
			clients := &Clients{
				IAMClient: mockIAM,
				Region:    "us-west-2",
			}

			// Create provider
			p := NewProvider()

			// Test createEKSNodeRole
			ctx := context.Background()
			arn, err := p.createEKSNodeRole(ctx, clients, tt.clusterName)

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
				tt.validateResult(t, arn)
			}

			// Validate API call
			if tt.validateCall != nil && capturedInput != nil {
				tt.validateCall(t, capturedInput)
			}
		})
	}
}
