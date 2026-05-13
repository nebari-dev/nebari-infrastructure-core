package aws

import (
	"context"
	"fmt"
	"slices"
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
	var _ IAMClient = &mockIAMClient{}
}

func TestGetRequiredPermissions(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		mustHave    []string
		mustNotHave []string
	}{
		{
			name: "minimal config includes base, VPC, IAM and excludes EFS",
			cfg: &Config{
				Region:     "us-east-1",
				NodeGroups: map[string]NodeGroup{"general": {Instance: "t3.medium"}},
			},
			mustHave: []string{
				"sts:GetCallerIdentity",
				"s3:CreateBucket",
				"eks:CreateCluster",
				"ec2:CreateVpc",
				"iam:CreateRole",
			},
			mustNotHave: []string{"elasticfilesystem:CreateFileSystem"},
		},
		{
			name: "skips VPC permissions when existing VPC provided",
			cfg: &Config{
				Region:        "us-east-1",
				ExistingVPCID: "vpc-12345678",
				NodeGroups:    map[string]NodeGroup{"general": {Instance: "t3.medium"}},
			},
			mustHave:    []string{"eks:CreateCluster", "iam:CreateRole"},
			mustNotHave: []string{"ec2:CreateVpc", "ec2:DeleteVpc"},
		},
		{
			name: "skips IAM permissions when existing cluster role provided",
			cfg: &Config{
				Region:                 "us-east-1",
				ExistingClusterRoleArn: "arn:aws:iam::123456789012:role/existing-role",
				NodeGroups:             map[string]NodeGroup{"general": {Instance: "t3.medium"}},
			},
			mustHave:    []string{"eks:CreateCluster", "ec2:CreateVpc"},
			mustNotHave: []string{"iam:CreateRole", "iam:DeleteRole"},
		},
		{
			name: "includes EFS permissions when EFS enabled",
			cfg: &Config{
				Region:     "us-east-1",
				NodeGroups: map[string]NodeGroup{"general": {Instance: "t3.medium"}},
				EFS:        &EFSConfig{Enabled: true},
			},
			mustHave: []string{
				"elasticfilesystem:CreateFileSystem",
				"elasticfilesystem:DeleteFileSystem",
				"elasticfilesystem:CreateMountTarget",
			},
		},
		{
			name: "excludes EFS permissions when EFS disabled",
			cfg: &Config{
				Region:     "us-east-1",
				NodeGroups: map[string]NodeGroup{"general": {Instance: "t3.medium"}},
				EFS:        &EFSConfig{Enabled: false},
			},
			mustNotHave: []string{"elasticfilesystem:CreateFileSystem"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := getRequiredPermissions(tt.cfg)
			for _, p := range tt.mustHave {
				if !slices.Contains(perms, p) {
					t.Errorf("missing permission %q", p)
				}
			}
			for _, p := range tt.mustNotHave {
				if slices.Contains(perms, p) {
					t.Errorf("should not have permission %q", p)
				}
			}
		})
	}
}

func newTestSTSMock() *mockSTSClient {
	return &mockSTSClient{
		GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Account: aws.String("123456789012"),
				Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
				UserId:  aws.String("AIDAEXAMPLE"),
			}, nil
		},
	}
}

func allowAllIAMMock() *mockIAMClient {
	return &mockIAMClient{
		SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
			for i, action := range params.ActionNames {
				results[i] = iamtypes.EvaluationResult{
					EvalActionName: aws.String(action),
					EvalDecision:   iamtypes.PolicyEvaluationDecisionTypeAllowed,
				}
			}
			return &iam.SimulatePrincipalPolicyOutput{EvaluationResults: results}, nil
		},
	}
}

func TestValidateCredentialsWithClients(t *testing.T) {
	baseCfg := &Config{
		Region:     "us-east-1",
		NodeGroups: map[string]NodeGroup{"general": {Instance: "t3.medium"}},
	}

	t.Run("success with all permissions allowed", func(t *testing.T) {
		result, err := validateCredentialsWithClients(context.Background(), newTestSTSMock(), allowAllIAMMock(), baseCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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
		denied := map[string]bool{"eks:CreateCluster": true, "iam:CreateRole": true}
		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
				for i, action := range params.ActionNames {
					decision := iamtypes.PolicyEvaluationDecisionTypeAllowed
					if denied[action] {
						decision = iamtypes.PolicyEvaluationDecisionTypeImplicitDeny
					}
					results[i] = iamtypes.EvaluationResult{
						EvalActionName: aws.String(action),
						EvalDecision:   decision,
					}
				}
				return &iam.SimulatePrincipalPolicyOutput{EvaluationResults: results}, nil
			},
		}

		result, err := validateCredentialsWithClients(context.Background(), newTestSTSMock(), iamMock, baseCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.MissingPermissions) != 2 {
			t.Errorf("MissingPermissions count = %d, want 2", len(result.MissingPermissions))
		}
		for _, want := range []string{"eks:CreateCluster", "iam:CreateRole"} {
			if !slices.Contains(result.MissingPermissions, want) {
				t.Errorf("MissingPermissions should contain %q", want)
			}
		}
	})

	t.Run("error on STS GetCallerIdentity failure", func(t *testing.T) {
		stsMock := &mockSTSClient{
			GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
				return nil, fmt.Errorf("invalid credentials")
			},
		}
		if _, err := validateCredentialsWithClients(context.Background(), stsMock, &mockIAMClient{}, baseCfg); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("error on IAM SimulatePrincipalPolicy failure", func(t *testing.T) {
		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				return nil, fmt.Errorf("access denied to simulate policy")
			},
		}
		if _, err := validateCredentialsWithClients(context.Background(), newTestSTSMock(), iamMock, baseCfg); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("batches permissions in groups of 100", func(t *testing.T) {
		callCount := 0
		iamMock := &mockIAMClient{
			SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
				callCount++
				if len(params.ActionNames) > iamSimulateBatchSize {
					t.Errorf("batch size = %d, want <= %d", len(params.ActionNames), iamSimulateBatchSize)
				}
				results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
				for i, action := range params.ActionNames {
					results[i] = iamtypes.EvaluationResult{
						EvalActionName: aws.String(action),
						EvalDecision:   iamtypes.PolicyEvaluationDecisionTypeAllowed,
					}
				}
				return &iam.SimulatePrincipalPolicyOutput{EvaluationResults: results}, nil
			},
		}

		cfg := &Config{
			Region:     "us-east-1",
			NodeGroups: map[string]NodeGroup{"general": {Instance: "t3.medium"}},
			EFS:        &EFSConfig{Enabled: true},
		}

		if _, err := validateCredentialsWithClients(context.Background(), newTestSTSMock(), iamMock, cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount == 0 {
			t.Error("expected SimulatePrincipalPolicy to be called at least once")
		}
	})
}
