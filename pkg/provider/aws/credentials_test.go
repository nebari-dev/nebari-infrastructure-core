package aws

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/iam"
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
