package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// IAMClient defines the interface for IAM operations needed for credential validation.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
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
