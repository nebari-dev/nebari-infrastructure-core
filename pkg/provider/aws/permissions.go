package aws

// awsBasePermissions is the set of AWS IAM permissions required for every
// Nebari deployment, regardless of configuration. Permissions for optional
// resources (VPC, IAM, EFS) are added by getRequiredPermissions based on
// the provided Config.
var awsBasePermissions = []string{
	// STS
	"sts:GetCallerIdentity",

	// S3 - state bucket management
	"s3:HeadBucket",
	"s3:CreateBucket",
	"s3:PutBucketVersioning",
	"s3:PutPublicAccessBlock",
	"s3:ListObjectVersions",
	"s3:DeleteObject",
	"s3:DeleteBucket",

	// EC2 - core
	"ec2:DescribeAvailabilityZones",
	"ec2:CreateTags",
	"ec2:DeleteTags",

	// EKS
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

// awsVPCPermissions are required when Nebari provisions its own VPC. Skip
// when ExistingVPCID is provided.
var awsVPCPermissions = []string{
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
}

// awsIAMPermissions are required when Nebari creates the EKS cluster role.
// Skip when ExistingClusterRoleArn is provided.
var awsIAMPermissions = []string{
	"iam:CreateRole",
	"iam:DeleteRole",
	"iam:GetRole",
	"iam:AttachRolePolicy",
	"iam:DetachRolePolicy",
	"iam:ListAttachedRolePolicies",
	"iam:PassRole",
	"iam:TagRole",
}

// awsEFSPermissions are required when EFS is enabled.
var awsEFSPermissions = []string{
	"elasticfilesystem:CreateFileSystem",
	"elasticfilesystem:DeleteFileSystem",
	"elasticfilesystem:DescribeFileSystems",
	"elasticfilesystem:CreateMountTarget",
	"elasticfilesystem:DeleteMountTarget",
	"elasticfilesystem:DescribeMountTargets",
	"elasticfilesystem:TagResource",
}

// getRequiredPermissions returns the list of AWS IAM permissions required
// based on the configuration. Permission sets are defined in this file and
// composed conditionally:
//   - VPC permissions skipped when ExistingVPCID is set
//   - IAM permissions skipped when ExistingClusterRoleArn is set
//   - EFS permissions only included when EFS.Enabled is true
func getRequiredPermissions(cfg *Config) []string {
	perms := append([]string(nil), awsBasePermissions...)

	if cfg.ExistingVPCID == "" {
		perms = append(perms, awsVPCPermissions...)
	}

	if cfg.ExistingClusterRoleArn == "" {
		perms = append(perms, awsIAMPermissions...)
	}

	if cfg.EFS != nil && cfg.EFS.Enabled {
		perms = append(perms, awsEFSPermissions...)
	}

	return perms
}
