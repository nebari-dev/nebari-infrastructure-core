package aws

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// mockIAMClient implements IAMClient for testing.
type mockIAMClient struct {
	SimulatePrincipalPolicyFunc func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}

func (m *mockIAMClient) SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
	if m.SimulatePrincipalPolicyFunc != nil {
		return m.SimulatePrincipalPolicyFunc(ctx, params, optFns...)
	}
	return &iam.SimulatePrincipalPolicyOutput{}, nil
}

func TestMockIAMClient(t *testing.T) {
	// Verify mock implements interface
	var _ IAMClient = &mockIAMClient{}
}

func TestGetRequiredPermissions(t *testing.T) {
	t.Run("returns base permissions for minimal config", func(t *testing.T) {
		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
			},
		}

		perms := getRequiredPermissions(cfg)

		// Should contain STS permission
		if !containsPermission(perms, "sts:GetCallerIdentity") {
			t.Error("missing sts:GetCallerIdentity")
		}

		// Should contain S3 permissions for state bucket
		if !containsPermission(perms, "s3:CreateBucket") {
			t.Error("missing s3:CreateBucket")
		}

		// Should contain EKS permissions
		if !containsPermission(perms, "eks:CreateCluster") {
			t.Error("missing eks:CreateCluster")
		}

		// Should contain VPC permissions (no existing VPC)
		if !containsPermission(perms, "ec2:CreateVpc") {
			t.Error("missing ec2:CreateVpc")
		}

		// Should contain IAM permissions (no existing roles)
		if !containsPermission(perms, "iam:CreateRole") {
			t.Error("missing iam:CreateRole")
		}

		// Should NOT contain EFS permissions (not enabled)
		if containsPermission(perms, "elasticfilesystem:CreateFileSystem") {
			t.Error("should not have EFS permissions when not enabled")
		}
	})

	t.Run("skips VPC permissions when existing VPC provided", func(t *testing.T) {
		cfg := &Config{
			Region:        "us-east-1",
			ExistingVPCID: "vpc-12345678",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
			},
		}

		perms := getRequiredPermissions(cfg)

		// Should NOT contain VPC creation permissions
		if containsPermission(perms, "ec2:CreateVpc") {
			t.Error("should not have ec2:CreateVpc when using existing VPC")
		}
		if containsPermission(perms, "ec2:DeleteVpc") {
			t.Error("should not have ec2:DeleteVpc when using existing VPC")
		}

		// Should still contain EKS permissions
		if !containsPermission(perms, "eks:CreateCluster") {
			t.Error("missing eks:CreateCluster")
		}
	})

	t.Run("skips IAM permissions when existing cluster role provided", func(t *testing.T) {
		cfg := &Config{
			Region:                 "us-east-1",
			ExistingClusterRoleArn: "arn:aws:iam::123456789012:role/existing-role",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
			},
		}

		perms := getRequiredPermissions(cfg)

		// Should NOT contain IAM creation permissions
		if containsPermission(perms, "iam:CreateRole") {
			t.Error("should not have iam:CreateRole when using existing role")
		}
		if containsPermission(perms, "iam:DeleteRole") {
			t.Error("should not have iam:DeleteRole when using existing role")
		}

		// Should still contain EKS permissions
		if !containsPermission(perms, "eks:CreateCluster") {
			t.Error("missing eks:CreateCluster")
		}
	})

	t.Run("includes EFS permissions when enabled", func(t *testing.T) {
		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
			},
			EFS: &EFSConfig{
				Enabled: true,
			},
		}

		perms := getRequiredPermissions(cfg)

		// Should contain EFS permissions
		if !containsPermission(perms, "elasticfilesystem:CreateFileSystem") {
			t.Error("missing elasticfilesystem:CreateFileSystem when EFS enabled")
		}
		if !containsPermission(perms, "elasticfilesystem:DeleteFileSystem") {
			t.Error("missing elasticfilesystem:DeleteFileSystem when EFS enabled")
		}
		if !containsPermission(perms, "elasticfilesystem:CreateMountTarget") {
			t.Error("missing elasticfilesystem:CreateMountTarget when EFS enabled")
		}
	})

	t.Run("excludes EFS permissions when EFS exists but disabled", func(t *testing.T) {
		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium", MinNodes: 1, MaxNodes: 3},
			},
			EFS: &EFSConfig{
				Enabled: false,
			},
		}

		perms := getRequiredPermissions(cfg)

		// Should NOT contain EFS permissions
		if containsPermission(perms, "elasticfilesystem:CreateFileSystem") {
			t.Error("should not have EFS permissions when EFS disabled")
		}
	})
}

func containsPermission(perms []string, perm string) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func TestValidateCredentialsWithClients(t *testing.T) {
	t.Run("success with all permissions allowed", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return &sts.GetCallerIdentityOutput{
					Account: aws.String("123456789012"),
					Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					UserId:  aws.String("AIDAEXAMPLE"),
				}, nil
			},
		}

		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
				for i, action := range params.ActionNames {
					results[i] = iamtypes.EvaluationResult{
						EvalActionName: aws.String(action),
						EvalDecision:   iamtypes.PolicyEvaluationDecisionTypeAllowed,
					}
				}
				return &iam.SimulatePrincipalPolicyOutput{
					EvaluationResults: results,
				}, nil
			},
		}

		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium"},
			},
		}

		result, err := validateCredentialsWithClients(context.Background(), stsMock, iamMock, cfg)
		if err != nil {
			t.Errorf("validateCredentialsWithClients() error = %v", err)
		}
		if result.AccountID != "123456789012" {
			t.Errorf("AccountID = %q, want %q", result.AccountID, "123456789012")
		}
		if result.Arn != "arn:aws:iam::123456789012:user/test-user" {
			t.Errorf("Arn = %q, want %q", result.Arn, "arn:aws:iam::123456789012:user/test-user")
		}
		if len(result.MissingPermissions) != 0 {
			t.Errorf("MissingPermissions = %v, want empty", result.MissingPermissions)
		}
	})

	t.Run("reports missing permissions", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return &sts.GetCallerIdentityOutput{
					Account: aws.String("123456789012"),
					Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					UserId:  aws.String("AIDAEXAMPLE"),
				}, nil
			},
		}

		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
				for i, action := range params.ActionNames {
					decision := iamtypes.PolicyEvaluationDecisionTypeAllowed
					// Deny eks:CreateCluster and iam:CreateRole
					if action == "eks:CreateCluster" || action == "iam:CreateRole" {
						decision = iamtypes.PolicyEvaluationDecisionTypeImplicitDeny
					}
					results[i] = iamtypes.EvaluationResult{
						EvalActionName: aws.String(action),
						EvalDecision:   decision,
					}
				}
				return &iam.SimulatePrincipalPolicyOutput{
					EvaluationResults: results,
				}, nil
			},
		}

		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium"},
			},
		}

		result, err := validateCredentialsWithClients(context.Background(), stsMock, iamMock, cfg)
		if err != nil {
			t.Errorf("validateCredentialsWithClients() error = %v", err)
		}
		if len(result.MissingPermissions) != 2 {
			t.Errorf("MissingPermissions count = %d, want 2", len(result.MissingPermissions))
		}
		if !containsPermission(result.MissingPermissions, "eks:CreateCluster") {
			t.Errorf("MissingPermissions should contain eks:CreateCluster")
		}
		if !containsPermission(result.MissingPermissions, "iam:CreateRole") {
			t.Errorf("MissingPermissions should contain iam:CreateRole")
		}
	})

	t.Run("error on STS GetCallerIdentity failure", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return nil, fmt.Errorf("invalid credentials")
			},
		}

		iamMock := &mockIAMClient{}

		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium"},
			},
		}

		_, err := validateCredentialsWithClients(context.Background(), stsMock, iamMock, cfg)
		if err == nil {
			t.Error("validateCredentialsWithClients() expected error, got nil")
		}
	})

	t.Run("error on IAM SimulatePrincipalPolicy failure", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return &sts.GetCallerIdentityOutput{
					Account: aws.String("123456789012"),
					Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					UserId:  aws.String("AIDAEXAMPLE"),
				}, nil
			},
		}

		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				return nil, fmt.Errorf("access denied to simulate policy")
			},
		}

		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium"},
			},
		}

		_, err := validateCredentialsWithClients(context.Background(), stsMock, iamMock, cfg)
		if err == nil {
			t.Error("validateCredentialsWithClients() expected error, got nil")
		}
	})

	t.Run("batches permissions in groups of 100", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return &sts.GetCallerIdentityOutput{
					Account: aws.String("123456789012"),
					Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					UserId:  aws.String("AIDAEXAMPLE"),
				}, nil
			},
		}

		callCount := 0
		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				callCount++
				if len(params.ActionNames) > 100 {
					t.Errorf("batch size = %d, want <= 100", len(params.ActionNames))
				}
				results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
				for i, action := range params.ActionNames {
					results[i] = iamtypes.EvaluationResult{
						EvalActionName: aws.String(action),
						EvalDecision:   iamtypes.PolicyEvaluationDecisionTypeAllowed,
					}
				}
				return &iam.SimulatePrincipalPolicyOutput{
					EvaluationResults: results,
				}, nil
			},
		}

		cfg := &Config{
			Region: "us-east-1",
			NodeGroups: map[string]NodeGroup{
				"general": {Instance: "t3.medium"},
			},
			// Include all possible permissions (VPC, IAM, EFS) to get a larger list
			EFS: &EFSConfig{Enabled: true},
		}

		_, err := validateCredentialsWithClients(context.Background(), stsMock, iamMock, cfg)
		if err != nil {
			t.Errorf("validateCredentialsWithClients() error = %v", err)
		}

		// With a full config, we expect at least one call
		if callCount == 0 {
			t.Error("expected SimulatePrincipalPolicy to be called at least once")
		}
	})
}
