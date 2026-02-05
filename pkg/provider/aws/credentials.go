package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// IAMClient defines the interface for IAM operations needed for credential validation.
type IAMClient interface {
	SimulatePrincipalPolicy(ctx context.Context, params *iam.SimulatePrincipalPolicyInput, optFns ...func(*iam.Options)) (*iam.SimulatePrincipalPolicyOutput, error)
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
