# AWS Credential Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add credential validation to the AWS provider with fast default check (GetCallerIdentity) and thorough optional check via `--validate-creds` flag.

**Architecture:** Enhance existing `Validate()` method to call `sts:GetCallerIdentity`. Add optional `CredentialValidator` interface for thorough validation using IAM Policy Simulator. CLI flag triggers thorough check only on `validate` command.

**Tech Stack:** Go, AWS SDK v2 (STS, IAM), Cobra CLI, OpenTelemetry

---

## Task 1: Add CredentialValidator Interface

**Files:**
- Modify: `pkg/provider/provider.go`

**Step 1: Write the interface**

Add after the existing `Provider` interface (around line 54):

```go
// CredentialValidator is an optional interface for providers that support
// thorough credential validation beyond basic authentication.
// Providers implement this to enable the --validate-creds flag.
// Providers that don't implement this (e.g., local) will show a message
// indicating the flag is not supported.
type CredentialValidator interface {
	// ValidateCredentials performs thorough credential validation including
	// permission checks using provider-specific APIs (e.g., IAM Policy Simulator).
	// Returns nil if all required permissions are present.
	ValidateCredentials(ctx context.Context, cfg *config.NebariConfig) error
}
```

**Step 2: Run tests to verify no breakage**

Run: `go test ./pkg/provider/... -v`
Expected: PASS (no existing tests for provider.go, but verify compilation)

**Step 3: Commit**

```bash
git add pkg/provider/provider.go
git commit -m "feat(provider): add CredentialValidator interface for thorough credential checks"
```

---

## Task 2: Add --validate-creds Flag to Validate Command

**Files:**
- Modify: `cmd/nic/validate.go`

**Step 1: Add flag variable and registration**

Add to the var block (around line 14):

```go
var (
	validateConfigFile string
	validateCreds      bool

	validateCmd = &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Long: `Validate the nebari-config.yaml file without deploying any infrastructure.
This command checks that the configuration file is properly formatted and contains
all required fields.

Use --validate-creds to perform thorough AWS credential validation including
permission checks via IAM Policy Simulator.`,
		RunE: runValidate,
	}
)
```

Add in init() after the existing flag:

```go
func init() {
	validateCmd.Flags().StringVarP(&validateConfigFile, "file", "f", "", "Path to nebari-config.yaml file (required)")
	if err := validateCmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	validateCmd.Flags().BoolVar(&validateCreds, "validate-creds", false, "Perform thorough credential validation (AWS only)")
}
```

**Step 2: Update runValidate to use the flag**

Replace the runValidate function:

```go
func runValidate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", validateConfigFile),
		attribute.Bool("validate_creds", validateCreds),
	)

	slog.Info("Validating configuration", "config_file", validateConfigFile)

	// Parse configuration
	cfg, err := config.ParseConfig(ctx, validateConfigFile)
	if err != nil {
		span.RecordError(err)
		slog.Error("Configuration validation failed", "error", err, "file", validateConfigFile)
		return err
	}

	slog.Info("Configuration is valid",
		"provider", cfg.Provider,
		"project_name", cfg.ProjectName,
	)

	// Get provider
	p, err := registry.Get(ctx, cfg.Provider)
	if err != nil {
		span.RecordError(err)
		slog.Error("Provider not available", "error", err, "provider", cfg.Provider)
		return err
	}

	// Run provider validation
	if err := p.Validate(ctx, cfg); err != nil {
		span.RecordError(err)
		slog.Error("Provider validation failed", "error", err)
		return err
	}

	// Run thorough credential validation if requested
	if validateCreds {
		if cv, ok := p.(provider.CredentialValidator); ok {
			if err := cv.ValidateCredentials(ctx, cfg); err != nil {
				span.RecordError(err)
				slog.Error("Credential validation failed", "error", err)
				return err
			}
		} else {
			fmt.Printf("Note: The %s provider does not support --validate-creds\n", p.Name())
		}
	}

	fmt.Printf("✓ Configuration file is valid\n")
	fmt.Printf("  Provider: %s\n", cfg.Provider)
	fmt.Printf("  Project: %s\n", cfg.ProjectName)

	return nil
}
```

**Step 3: Add import for provider package**

Update imports:

```go
import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)
```

**Step 4: Verify compilation**

Run: `go build ./cmd/nic`
Expected: Build succeeds

**Step 5: Commit**

```bash
git add cmd/nic/validate.go
git commit -m "feat(cli): add --validate-creds flag to validate command"
```

---

## Task 3: Create IAM Client Interface and Mock

**Files:**
- Create: `pkg/provider/aws/credentials.go`
- Create: `pkg/provider/aws/credentials_test.go`

**Step 1: Write the IAM client interface**

Create `pkg/provider/aws/credentials.go`:

```go
package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// IAMClient defines the interface for IAM operations needed for credential validation.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}
```

**Step 2: Write mock for testing**

Create `pkg/provider/aws/credentials_test.go`:

```go
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
```

**Step 3: Run test to verify mock compiles**

Run: `go test ./pkg/provider/aws/... -v -run TestMockIAMClient`
Expected: PASS

**Step 4: Commit**

```bash
git add pkg/provider/aws/credentials.go pkg/provider/aws/credentials_test.go
git commit -m "feat(aws): add IAM client interface for credential validation"
```

---

## Task 4: Implement getRequiredPermissions Function

**Files:**
- Modify: `pkg/provider/aws/credentials.go`
- Modify: `pkg/provider/aws/credentials_test.go`

**Step 1: Write the failing test**

Add to `pkg/provider/aws/credentials_test.go`:

```go
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
}

func containsPermission(perms []string, perm string) bool {
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/provider/aws/... -v -run TestGetRequiredPermissions`
Expected: FAIL with "undefined: getRequiredPermissions"

**Step 3: Implement getRequiredPermissions**

Add to `pkg/provider/aws/credentials.go`:

```go
// getRequiredPermissions returns the list of AWS IAM permissions required
// based on the configuration. Permissions are config-aware:
// - VPC permissions skipped if ExistingVPCID is set
// - IAM permissions skipped if ExistingClusterRoleArn is set
// - EFS permissions only included if EFS.Enabled is true
func getRequiredPermissions(cfg *Config) []string {
	perms := []string{
		// STS - always required
		"sts:GetCallerIdentity",

		// S3 - state bucket management (always required)
		"s3:HeadBucket",
		"s3:CreateBucket",
		"s3:PutBucketVersioning",
		"s3:PutPublicAccessBlock",
		"s3:ListObjectVersions",
		"s3:DeleteObject",
		"s3:DeleteBucket",

		// EC2 - core (always required)
		"ec2:DescribeAvailabilityZones",
		"ec2:CreateTags",
		"ec2:DeleteTags",

		// EKS - always required
		"eks:CreateCluster",
		"eks:DeleteCluster",
		"eks:DescribeCluster",
		"eks:UpdateClusterVersion",
		"eks:UpdateClusterConfig",
		"eks:CreateNodegroup",
		"eks:DeleteNodegroup",
		"eks:DescribeNodegroup",
		"eks:ListNodegroups",
		"eks:UpdateNodegroupConfig",
		"eks:TagResource",
		"eks:UntagResource",
	}

	// VPC permissions - skip if using existing VPC
	if cfg.ExistingVPCID == "" {
		perms = append(perms,
			"ec2:CreateVpc",
			"ec2:DeleteVpc",
			"ec2:DescribeVpcs",
			"ec2:ModifyVpcAttribute",
			"ec2:CreateSubnet",
			"ec2:DeleteSubnet",
			"ec2:DescribeSubnets",
			"ec2:CreateInternetGateway",
			"ec2:DeleteInternetGateway",
			"ec2:AttachInternetGateway",
			"ec2:DetachInternetGateway",
			"ec2:DescribeInternetGateways",
			"ec2:AllocateAddress",
			"ec2:ReleaseAddress",
			"ec2:DescribeAddresses",
			"ec2:CreateNatGateway",
			"ec2:DeleteNatGateway",
			"ec2:DescribeNatGateways",
			"ec2:CreateRouteTable",
			"ec2:DeleteRouteTable",
			"ec2:DescribeRouteTables",
			"ec2:CreateRoute",
			"ec2:AssociateRouteTable",
			"ec2:DisassociateRouteTable",
			"ec2:CreateSecurityGroup",
			"ec2:DeleteSecurityGroup",
			"ec2:DescribeSecurityGroups",
			"ec2:AuthorizeSecurityGroupIngress",
			"ec2:AuthorizeSecurityGroupEgress",
			"ec2:CreateVpcEndpoint",
			"ec2:DeleteVpcEndpoints",
			"ec2:DescribeVpcEndpoints",
			"ec2:DescribeNetworkInterfaces",
		)
	}

	// IAM permissions - skip if using existing roles
	if cfg.ExistingClusterRoleArn == "" {
		perms = append(perms,
			"iam:CreateRole",
			"iam:DeleteRole",
			"iam:GetRole",
			"iam:AttachRolePolicy",
			"iam:DetachRolePolicy",
			"iam:ListAttachedRolePolicies",
			"iam:PassRole",
			"iam:TagRole",
		)
	}

	// EFS permissions - only if enabled
	if cfg.EFS != nil && cfg.EFS.Enabled {
		perms = append(perms,
			"elasticfilesystem:CreateFileSystem",
			"elasticfilesystem:DeleteFileSystem",
			"elasticfilesystem:DescribeFileSystems",
			"elasticfilesystem:CreateMountTarget",
			"elasticfilesystem:DeleteMountTarget",
			"elasticfilesystem:DescribeMountTargets",
			"elasticfilesystem:TagResource",
		)
	}

	return perms
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/provider/aws/... -v -run TestGetRequiredPermissions`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/provider/aws/credentials.go pkg/provider/aws/credentials_test.go
git commit -m "feat(aws): implement config-aware permission list"
```

---

## Task 5: Add Config-Aware Permission Tests

**Files:**
- Modify: `pkg/provider/aws/credentials_test.go`

**Step 1: Write test for existing VPC**

Add to `pkg/provider/aws/credentials_test.go`:

```go
func TestGetRequiredPermissions_WithExistingVPC(t *testing.T) {
	cfg := &Config{
		Region:        "us-east-1",
		ExistingVPCID: "vpc-12345",
		NodeGroups: map[string]NodeGroup{
			"general": {Instance: "t3.medium"},
		},
	}

	perms := getRequiredPermissions(cfg)

	// Should NOT contain VPC creation permissions
	if containsPermission(perms, "ec2:CreateVpc") {
		t.Error("should not have ec2:CreateVpc when using existing VPC")
	}
	if containsPermission(perms, "ec2:CreateSubnet") {
		t.Error("should not have ec2:CreateSubnet when using existing VPC")
	}

	// Should still contain EKS permissions
	if !containsPermission(perms, "eks:CreateCluster") {
		t.Error("missing eks:CreateCluster")
	}
}
```

**Step 2: Write test for existing IAM roles**

```go
func TestGetRequiredPermissions_WithExistingRoles(t *testing.T) {
	cfg := &Config{
		Region:                 "us-east-1",
		ExistingClusterRoleArn: "arn:aws:iam::123456789012:role/eks-cluster-role",
		NodeGroups: map[string]NodeGroup{
			"general": {Instance: "t3.medium"},
		},
	}

	perms := getRequiredPermissions(cfg)

	// Should NOT contain IAM role creation permissions
	if containsPermission(perms, "iam:CreateRole") {
		t.Error("should not have iam:CreateRole when using existing roles")
	}

	// Should still contain EKS permissions
	if !containsPermission(perms, "eks:CreateCluster") {
		t.Error("missing eks:CreateCluster")
	}
}
```

**Step 3: Write test for EFS enabled**

```go
func TestGetRequiredPermissions_WithEFSEnabled(t *testing.T) {
	cfg := &Config{
		Region: "us-east-1",
		NodeGroups: map[string]NodeGroup{
			"general": {Instance: "t3.medium"},
		},
		EFS: &EFSConfig{Enabled: true},
	}

	perms := getRequiredPermissions(cfg)

	// Should contain EFS permissions
	if !containsPermission(perms, "elasticfilesystem:CreateFileSystem") {
		t.Error("missing elasticfilesystem:CreateFileSystem when EFS enabled")
	}
	if !containsPermission(perms, "elasticfilesystem:CreateMountTarget") {
		t.Error("missing elasticfilesystem:CreateMountTarget when EFS enabled")
	}
}
```

**Step 4: Run all permission tests**

Run: `go test ./pkg/provider/aws/... -v -run TestGetRequiredPermissions`
Expected: PASS (all 4 tests)

**Step 5: Commit**

```bash
git add pkg/provider/aws/credentials_test.go
git commit -m "test(aws): add config-aware permission tests"
```

---

## Task 6: Implement ValidateCredentials Method

**Files:**
- Modify: `pkg/provider/aws/credentials.go`
- Modify: `pkg/provider/aws/credentials_test.go`

**Step 1: Write the failing test**

Add to `pkg/provider/aws/credentials_test.go`:

```go
func TestValidateCredentials_Success(t *testing.T) {
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
			// All permissions allowed
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
	if len(result.MissingPermissions) != 0 {
		t.Errorf("MissingPermissions = %v, want empty", result.MissingPermissions)
	}
}
```

Add import at top of test file:

```go
import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/provider/aws/... -v -run TestValidateCredentials_Success`
Expected: FAIL with "undefined: validateCredentialsWithClients"

**Step 3: Implement validateCredentialsWithClients**

Add to `pkg/provider/aws/credentials.go`:

```go
import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// CredentialValidationResult contains the results of credential validation.
type CredentialValidationResult struct {
	AccountID          string
	Arn                string
	MissingPermissions []string
}

// validateCredentialsWithClients performs thorough credential validation using
// provided clients (for testability).
func validateCredentialsWithClients(ctx context.Context, stsClient STSClient, iamClient IAMClient, cfg *Config) (*CredentialValidationResult, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.validateCredentialsWithClients")
	defer span.End()

	// Get caller identity
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	result := &CredentialValidationResult{
		AccountID: aws.ToString(identity.Account),
		Arn:       aws.ToString(identity.Arn),
	}

	span.SetAttributes(
		attribute.String("aws.account_id", result.AccountID),
		attribute.String("aws.arn", result.Arn),
	)

	// Get required permissions based on config
	requiredPerms := getRequiredPermissions(cfg)

	// Check permissions using IAM Policy Simulator
	// Process in batches of 100 (API limit)
	const batchSize = 100
	var missingPerms []string

	for i := 0; i < len(requiredPerms); i += batchSize {
		end := i + batchSize
		if end > len(requiredPerms) {
			end = len(requiredPerms)
		}
		batch := requiredPerms[i:end]

		simResult, err := iamClient.SimulatePrincipalPolicy(ctx, &iam.SimulatePrincipalPolicyInput{
			PolicySourceArn: identity.Arn,
			ActionNames:     batch,
			ResourceArns:    []string{"*"},
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to simulate IAM policy: %w", err)
		}

		for _, evalResult := range simResult.EvaluationResults {
			if evalResult.EvalDecision != iamtypes.PolicyEvaluationDecisionTypeAllowed {
				missingPerms = append(missingPerms, aws.ToString(evalResult.EvalActionName))
			}
		}
	}

	result.MissingPermissions = missingPerms

	span.SetAttributes(
		attribute.Int("permissions.checked", len(requiredPerms)),
		attribute.Int("permissions.missing", len(missingPerms)),
	)

	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/provider/aws/... -v -run TestValidateCredentials_Success`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/provider/aws/credentials.go pkg/provider/aws/credentials_test.go
git commit -m "feat(aws): implement credential validation with IAM simulator"
```

---

## Task 7: Add ValidateCredentials Error Tests

**Files:**
- Modify: `pkg/provider/aws/credentials_test.go`

**Step 1: Write test for invalid credentials**

```go
func TestValidateCredentials_InvalidCreds(t *testing.T) {
	stsMock := &mockSTSClient{
		GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return nil, fmt.Errorf("ExpiredToken: The security token included in the request is expired")
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
		t.Error("expected error for invalid credentials")
	}
	if !strings.Contains(err.Error(), "failed to get caller identity") {
		t.Errorf("error = %q, should contain 'failed to get caller identity'", err.Error())
	}
}
```

Add `"fmt"` and `"strings"` to imports in test file.

**Step 2: Write test for missing permissions**

```go
func TestValidateCredentials_MissingPermissions(t *testing.T) {
	stsMock := &mockSTSClient{
		GetCallerIdentityFunc: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Account: aws.String("123456789012"),
				Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
			}, nil
		},
	}

	iamMock := &mockIAMClient{
		SimulatePrincipalPolicyFunc: func(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error) {
			results := make([]iamtypes.EvaluationResult, len(params.ActionNames))
			for i, action := range params.ActionNames {
				decision := iamtypes.PolicyEvaluationDecisionTypeAllowed
				// Deny specific permissions
				if action == "eks:CreateCluster" || action == "iam:PassRole" {
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
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.MissingPermissions) != 2 {
		t.Errorf("MissingPermissions count = %d, want 2", len(result.MissingPermissions))
	}
	if !containsPermission(result.MissingPermissions, "eks:CreateCluster") {
		t.Error("should report eks:CreateCluster as missing")
	}
	if !containsPermission(result.MissingPermissions, "iam:PassRole") {
		t.Error("should report iam:PassRole as missing")
	}
}
```

**Step 3: Run tests**

Run: `go test ./pkg/provider/aws/... -v -run TestValidateCredentials`
Expected: PASS (all 3 tests)

**Step 4: Commit**

```bash
git add pkg/provider/aws/credentials_test.go
git commit -m "test(aws): add credential validation error tests"
```

---

## Task 8: Implement Provider.ValidateCredentials Method

**Files:**
- Modify: `pkg/provider/aws/credentials.go`

**Step 1: Add newIAMClient helper**

Add to `pkg/provider/aws/credentials.go`:

```go
// newIAMClient creates a new IAM client for the specified region.
func newIAMClient(ctx context.Context, region string) (IAMClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return iam.NewFromConfig(cfg), nil
}
```

**Step 2: Implement ValidateCredentials on Provider**

Add to `pkg/provider/aws/credentials.go`:

```go
// ValidateCredentials implements provider.CredentialValidator for thorough
// credential validation including IAM permission checks.
func (p *Provider) ValidateCredentials(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ValidateCredentials")
	defer span.End()

	// Extract AWS configuration
	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Create clients
	stsClient, err := newSTSClient(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create STS client: %w", err)
	}

	iamClient, err := newIAMClient(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create IAM client: %w", err)
	}

	// Run validation
	result, err := validateCredentialsWithClients(ctx, stsClient, iamClient, awsCfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Print success info
	fmt.Printf("AWS credentials validated\n")
	fmt.Printf("  Identity: %s\n", result.Arn)
	fmt.Printf("  Account:  %s\n", result.AccountID)

	// Check for missing permissions
	if len(result.MissingPermissions) > 0 {
		fmt.Printf("\nMissing permissions:\n")
		for _, perm := range result.MissingPermissions {
			fmt.Printf("  - %s\n", perm)
		}
		return fmt.Errorf("credential validation failed: %d missing permissions", len(result.MissingPermissions))
	}

	return nil
}
```

**Step 3: Verify Provider implements CredentialValidator**

Add compile-time check at the end of `pkg/provider/aws/credentials.go`:

```go
// Compile-time check that Provider implements CredentialValidator.
var _ provider.CredentialValidator = (*Provider)(nil)
```

Add import:

```go
import (
	// ... existing imports ...
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)
```

**Step 4: Run all tests**

Run: `go test ./pkg/provider/aws/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/provider/aws/credentials.go
git commit -m "feat(aws): implement CredentialValidator interface on Provider"
```

---

## Task 9: Update Default Validation with GetCallerIdentity

**Files:**
- Modify: `pkg/provider/aws/provider.go`

**Step 1: Update Validate() to use GetCallerIdentity**

In `pkg/provider/aws/provider.go`, replace the existing credential validation code (lines 197-206):

```go
	// Validate AWS credentials using GetCallerIdentity
	stsClient, err := newSTSClient(ctx, awsCfg.Region)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create STS client: %w", err)
	}

	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("AWS credential validation failed: %w", err)
	}

	span.SetAttributes(
		attribute.String("aws.account_id", aws.ToString(identity.Account)),
		attribute.String("aws.arn", aws.ToString(identity.Arn)),
		attribute.Bool("validation_passed", true),
		attribute.String("aws.region", awsCfg.Region),
	)
```

Add imports at top of file:

```go
import (
	// ... existing imports ...
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)
```

**Step 2: Run all tests**

Run: `go test ./... -v`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/provider/aws/provider.go
git commit -m "feat(aws): use GetCallerIdentity for default credential validation"
```

---

## Task 10: Final Integration Test and Cleanup

**Files:**
- All modified files

**Step 1: Run full test suite**

Run: `go test ./... -v -cover`
Expected: PASS with coverage report

**Step 2: Run linting**

Run: `golangci-lint run`
Expected: No errors

**Step 3: Build binary**

Run: `go build ./cmd/nic`
Expected: Build succeeds

**Step 4: Test CLI help**

Run: `./nic validate --help`
Expected: Shows `--validate-creds` flag in help output

**Step 5: Commit design doc**

```bash
git add docs/plans/
git commit -m "docs: add AWS credential validation design and implementation plan"
```

---

## Summary

| Task | Description | Key Files |
|------|-------------|-----------|
| 1 | Add CredentialValidator interface | `pkg/provider/provider.go` |
| 2 | Add --validate-creds flag | `cmd/nic/validate.go` |
| 3 | Create IAM client interface | `pkg/provider/aws/credentials.go` |
| 4 | Implement getRequiredPermissions | `pkg/provider/aws/credentials.go` |
| 5 | Add config-aware permission tests | `pkg/provider/aws/credentials_test.go` |
| 6 | Implement ValidateCredentials | `pkg/provider/aws/credentials.go` |
| 7 | Add error tests | `pkg/provider/aws/credentials_test.go` |
| 8 | Wire up Provider method | `pkg/provider/aws/credentials.go` |
| 9 | Update default validation | `pkg/provider/aws/provider.go` |
| 10 | Final integration test | All files |
