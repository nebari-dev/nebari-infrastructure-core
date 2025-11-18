package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// TestDeleteIAMRoles tests the deleteIAMRoles function using mocks
func TestDeleteIAMRoles(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		mockSetup   func(*MockIAMClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "successful deletion of both roles",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				// Mock discoverIAMRoles - both roles exist
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123:role/" + *params.RoleName),
						},
					}, nil
				}

				// Mock ListAttachedRolePolicies
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return &iam.ListAttachedRolePoliciesOutput{
						AttachedPolicies: []iamtypes.AttachedPolicy{
							{PolicyArn: aws.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy")},
						},
					}, nil
				}

				// Mock DetachRolePolicy
				m.DetachRolePolicyFunc = func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
					return &iam.DetachRolePolicyOutput{}, nil
				}

				// Mock ListRolePolicies (inline policies)
				m.ListRolePoliciesFunc = func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
					return &iam.ListRolePoliciesOutput{
						PolicyNames: []string{}, // No inline policies
					}, nil
				}

				// Mock DeleteRolePolicy (if needed)
				m.DeleteRolePolicyFunc = func(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
					return &iam.DeleteRolePolicyOutput{}, nil
				}

				// Mock DeleteRole
				m.DeleteRoleFunc = func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
					return &iam.DeleteRoleOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:        "roles don't exist - no error",
			clusterName: "nonexistent-cluster",
			mockSetup: func(m *MockIAMClient) {
				// Mock discoverIAMRoles - roles not found
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return nil, &iamtypes.NoSuchEntityException{
						Message: aws.String("Role not found"),
					}
				}
			},
			expectError: false, // Should not error when roles don't exist
		},
		{
			name:        "error detaching policy",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123:role/" + *params.RoleName),
						},
					}, nil
				}
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return &iam.ListAttachedRolePoliciesOutput{
						AttachedPolicies: []iamtypes.AttachedPolicy{
							{PolicyArn: aws.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy")},
						},
					}, nil
				}
				m.DetachRolePolicyFunc = func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
					return nil, fmt.Errorf("AccessDenied: User not authorized")
				}
				m.ListRolePoliciesFunc = func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
					return &iam.ListRolePoliciesOutput{PolicyNames: []string{}}, nil
				}
			},
			expectError: true,
			errorMsg:    "failed to delete cluster role",
		},
		{
			name:        "error deleting role",
			clusterName: "test-cluster",
			mockSetup: func(m *MockIAMClient) {
				m.GetRoleFunc = func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
					return &iam.GetRoleOutput{
						Role: &iamtypes.Role{
							RoleName: params.RoleName,
							Arn:      aws.String("arn:aws:iam::123:role/" + *params.RoleName),
						},
					}, nil
				}
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return &iam.ListAttachedRolePoliciesOutput{
						AttachedPolicies: []iamtypes.AttachedPolicy{},
					}, nil
				}
				m.DetachRolePolicyFunc = func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
					return &iam.DetachRolePolicyOutput{}, nil
				}
				m.ListRolePoliciesFunc = func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
					return &iam.ListRolePoliciesOutput{PolicyNames: []string{}}, nil
				}
				m.DeleteRoleFunc = func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
					return nil, fmt.Errorf("DeleteConflict: Role is in use")
				}
			},
			expectError: true,
			errorMsg:    "failed to delete cluster role",
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

			// Test deleteIAMRoles
			ctx := context.Background()
			err := p.deleteIAMRoles(ctx, clients, tt.clusterName)

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

// TestDeleteIAMRole tests the deleteIAMRole function using mocks
func TestDeleteIAMRole(t *testing.T) {
	tests := []struct {
		name        string
		roleName    string
		mockSetup   func(*MockIAMClient)
		expectError bool
		errorMsg    string
	}{
		{
			name:     "successful role deletion with policies",
			roleName: "test-role",
			mockSetup: func(m *MockIAMClient) {
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return &iam.ListAttachedRolePoliciesOutput{
						AttachedPolicies: []iamtypes.AttachedPolicy{
							{PolicyArn: aws.String("arn:aws:iam::aws:policy/Policy1")},
							{PolicyArn: aws.String("arn:aws:iam::aws:policy/Policy2")},
						},
					}, nil
				}
				m.DetachRolePolicyFunc = func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
					return &iam.DetachRolePolicyOutput{}, nil
				}
				m.ListRolePoliciesFunc = func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
					return &iam.ListRolePoliciesOutput{PolicyNames: []string{}}, nil
				}
				m.DeleteRoleFunc = func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
					return &iam.DeleteRoleOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:     "successful role deletion with no policies",
			roleName: "test-role",
			mockSetup: func(m *MockIAMClient) {
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return &iam.ListAttachedRolePoliciesOutput{
						AttachedPolicies: []iamtypes.AttachedPolicy{},
					}, nil
				}
				m.ListRolePoliciesFunc = func(ctx context.Context, params *iam.ListRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListRolePoliciesOutput, error) {
					return &iam.ListRolePoliciesOutput{PolicyNames: []string{}}, nil
				}
				m.DeleteRoleFunc = func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
					return &iam.DeleteRoleOutput{}, nil
				}
			},
			expectError: false,
		},
		{
			name:     "error listing policies",
			roleName: "test-role",
			mockSetup: func(m *MockIAMClient) {
				m.ListAttachedRolePoliciesFunc = func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
					return nil, fmt.Errorf("AccessDenied: Not authorized")
				}
			},
			expectError: true,
			errorMsg:    "failed to list attached policies",
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

			// Test deleteIAMRole
			ctx := context.Background()
			err := p.deleteIAMRole(ctx, clients, tt.roleName)

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
