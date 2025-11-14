package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// ResourceTypeIAMClusterRole is the resource type for EKS cluster IAM role
	ResourceTypeIAMClusterRole = "iam-cluster-role"
	// ResourceTypeIAMNodeRole is the resource type for EKS node IAM role
	ResourceTypeIAMNodeRole = "iam-node-role"
	// ResourceTypeIAMOIDCProvider is the resource type for OIDC provider
	ResourceTypeIAMOIDCProvider = "iam-oidc-provider"
)

// EKS cluster role trust policy - allows EKS service to assume this role
const eksClusterTrustPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "eks.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}`

// EKS node role trust policy - allows EC2 instances to assume this role
const eksNodeTrustPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "ec2.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}`

// AWS managed policies for EKS cluster role
var eksClusterManagedPolicies = []string{
	"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
	"arn:aws:iam::aws:policy/AmazonEKSVPCResourceController",
}

// AWS managed policies for EKS node role
var eksNodeManagedPolicies = []string{
	"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
	"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
	"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
}

// createIAMRoles creates both the EKS cluster role and node role
func (p *Provider) createIAMRoles(ctx context.Context, clients *Clients, clusterName string) (*IAMRoles, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createIAMRoles")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	// Create cluster role
	clusterRoleARN, err := p.createEKSClusterRole(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create EKS cluster role: %w", err)
	}

	// Create node role
	nodeRoleARN, err := p.createEKSNodeRole(ctx, clients, clusterName)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create EKS node role: %w", err)
	}

	iamRoles := &IAMRoles{
		ClusterRoleARN:      clusterRoleARN,
		NodeRoleARN:         nodeRoleARN,
		ServiceAccountRoles: make(map[string]string),
	}

	span.SetAttributes(
		attribute.String("cluster_role_arn", clusterRoleARN),
		attribute.String("node_role_arn", nodeRoleARN),
	)

	return iamRoles, nil
}

// createEKSClusterRole creates the IAM role for the EKS cluster
func (p *Provider) createEKSClusterRole(ctx context.Context, clients *Clients, clusterName string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createEKSClusterRole")
	defer span.End()

	roleName := GenerateResourceName(clusterName, "cluster-role", "")

	span.SetAttributes(
		attribute.String("role_name", roleName),
	)

	// Generate tags
	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeIAMClusterRole)
	iamTags := convertToIAMTags(nicTags)

	// Create the role
	createRoleInput := &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(eksClusterTrustPolicy),
		Description:              aws.String(fmt.Sprintf("EKS cluster role for %s managed by NIC", clusterName)),
		Tags:                     iamTags,
	}

	createOutput, err := clients.IAMClient.CreateRole(ctx, createRoleInput)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create IAM role %s: %w", roleName, err)
	}

	roleARN := aws.ToString(createOutput.Role.Arn)

	// Attach AWS managed policies
	for _, policyARN := range eksClusterManagedPolicies {
		attachInput := &iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(policyARN),
		}

		_, err := clients.IAMClient.AttachRolePolicy(ctx, attachInput)
		if err != nil {
			span.RecordError(err)
			return "", fmt.Errorf("failed to attach policy %s to role %s: %w", policyARN, roleName, err)
		}

		span.SetAttributes(
			attribute.String(fmt.Sprintf("attached_policy.%s", policyARN), "true"),
		)
	}

	span.SetAttributes(
		attribute.String("role_arn", roleARN),
	)

	return roleARN, nil
}

// createEKSNodeRole creates the IAM role for EKS worker nodes
func (p *Provider) createEKSNodeRole(ctx context.Context, clients *Clients, clusterName string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.createEKSNodeRole")
	defer span.End()

	roleName := GenerateResourceName(clusterName, "node-role", "")

	span.SetAttributes(
		attribute.String("role_name", roleName),
	)

	// Generate tags
	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeIAMNodeRole)
	iamTags := convertToIAMTags(nicTags)

	// Create the role
	createRoleInput := &iam.CreateRoleInput{
		RoleName:                 aws.String(roleName),
		AssumeRolePolicyDocument: aws.String(eksNodeTrustPolicy),
		Description:              aws.String(fmt.Sprintf("EKS node role for %s managed by NIC", clusterName)),
		Tags:                     iamTags,
	}

	createOutput, err := clients.IAMClient.CreateRole(ctx, createRoleInput)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create IAM role %s: %w", roleName, err)
	}

	roleARN := aws.ToString(createOutput.Role.Arn)

	// Attach AWS managed policies
	for _, policyARN := range eksNodeManagedPolicies {
		attachInput := &iam.AttachRolePolicyInput{
			RoleName:  aws.String(roleName),
			PolicyArn: aws.String(policyARN),
		}

		_, err := clients.IAMClient.AttachRolePolicy(ctx, attachInput)
		if err != nil {
			span.RecordError(err)
			return "", fmt.Errorf("failed to attach policy %s to role %s: %w", policyARN, roleName, err)
		}

		span.SetAttributes(
			attribute.String(fmt.Sprintf("attached_policy.%s", policyARN), "true"),
		)
	}

	span.SetAttributes(
		attribute.String("role_arn", roleARN),
	)

	return roleARN, nil
}

// convertToIAMTags converts NIC tags to IAM tag format
func convertToIAMTags(nicTags map[string]string) []iamtypes.Tag {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(context.Background(), "aws.convertToIAMTags")
	defer span.End()

	tags := make([]iamtypes.Tag, 0, len(nicTags))
	for key, value := range nicTags {
		tags = append(tags, iamtypes.Tag{
			Key:   aws.String(key),
			Value: aws.String(value),
		})
	}

	span.SetAttributes(
		attribute.Int("tag_count", len(tags)),
	)

	return tags
}
