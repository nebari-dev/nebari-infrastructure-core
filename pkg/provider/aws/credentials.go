package aws

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// iamSimulateBatchSize is the maximum number of action names per
// SimulatePrincipalPolicy call. The AWS API caps this at 100.
const iamSimulateBatchSize = 100

// IAMClient defines the IAM operations needed for credential validation.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
}

// newIAMClient creates a new IAM client for the specified region.
func newIAMClient(ctx context.Context, region string) (IAMClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return iam.NewFromConfig(cfg), nil
}

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

	requiredPerms := getRequiredPermissions(cfg)

	var missingPerms []string
	for batch := range slices.Chunk(requiredPerms, iamSimulateBatchSize) {
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

// ValidateCredentials implements provider.CredentialValidator for thorough
// credential validation including IAM permission checks.
func (p *Provider) ValidateCredentials(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.ValidateCredentials")
	defer span.End()

	span.SetAttributes(attribute.String("project_name", projectName))

	awsCfg, err := extractAWSConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

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

	result, err := validateCredentialsWithClients(ctx, stsClient, iamClient, awsCfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "AWS credentials validated").
		WithResource("credentials").
		WithAction("validated").
		WithMetadata("identity", result.Arn).
		WithMetadata("account", result.AccountID))

	if len(result.MissingPermissions) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelError, fmt.Sprintf("Missing %d required permissions: %s", len(result.MissingPermissions), strings.Join(result.MissingPermissions, ", "))).
			WithResource("credentials").
			WithAction("validated").
			WithMetadata("missing_permissions", result.MissingPermissions))
		return fmt.Errorf("credential validation failed: missing %d permissions: %s", len(result.MissingPermissions), strings.Join(result.MissingPermissions, ", "))
	}

	return nil
}

// Compile-time check that Provider implements CredentialValidator.
var _ provider.CredentialValidator = (*Provider)(nil)
