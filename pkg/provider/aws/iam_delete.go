package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// deleteIAMRoles deletes all IAM roles for the cluster
func (p *Provider) deleteIAMRoles(ctx context.Context, clients *Clients, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteIAMRoles")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Checking IAM roles").
		WithResource("iam-role").
		WithAction("discovering"))

	// Try to discover existing roles first
	iamRoles, err := p.discoverIAMRoles(ctx, clients, clusterName)
	if err != nil {
		// Roles don't exist - nothing to delete
		span.SetAttributes(attribute.Bool("roles_exist", false))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "IAM roles not found").
			WithResource("iam-role"))
		return nil
	}

	if iamRoles == nil {
		// Roles don't exist - nothing to delete
		span.SetAttributes(attribute.Bool("roles_exist", false))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "IAM roles not found").
			WithResource("iam-role"))
		return nil
	}

	span.SetAttributes(attribute.Bool("roles_exist", true))

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Deleting IAM roles").
		WithResource("iam-role").
		WithAction("deleting"))

	rolesDeleted := 0

	// Delete cluster role
	if iamRoles.ClusterRoleARN != "" {
		clusterRoleName := GenerateResourceName(clusterName, "cluster-role", "")
		if err := p.deleteIAMRole(ctx, clients, clusterRoleName); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete cluster role: %w", err)
		}
		rolesDeleted++
	}

	// Delete node role
	if iamRoles.NodeRoleARN != "" {
		nodeRoleName := GenerateResourceName(clusterName, "node-role", "")
		if err := p.deleteIAMRole(ctx, clients, nodeRoleName); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete node role: %w", err)
		}
		rolesDeleted++
	}

	span.SetAttributes(
		attribute.Int("roles_deleted", rolesDeleted),
		attribute.Bool("deletion_complete", true),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "IAM roles deleted").
		WithResource("iam-role").
		WithAction("deleted").
		WithMetadata("count", rolesDeleted))

	return nil
}

// deleteIAMRole deletes a single IAM role after detaching all policies
func (p *Provider) deleteIAMRole(ctx context.Context, clients *Clients, roleName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteIAMRole")
	defer span.End()

	span.SetAttributes(
		attribute.String("role_name", roleName),
	)

	// List attached managed policies
	attachedPoliciesOutput, err := clients.IAMClient.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
		RoleName: &roleName,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list attached policies for role %s: %w", roleName, err)
	}

	// Detach all managed policies
	for _, policy := range attachedPoliciesOutput.AttachedPolicies {
		_, err := clients.IAMClient.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  &roleName,
			PolicyArn: policy.PolicyArn,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to detach policy %s from role %s: %w", *policy.PolicyArn, roleName, err)
		}
	}

	span.SetAttributes(attribute.Int("policies_detached", len(attachedPoliciesOutput.AttachedPolicies)))

	// List inline policies
	inlinePoliciesOutput, err := clients.IAMClient.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: &roleName,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to list inline policies for role %s: %w", roleName, err)
	}

	// Delete all inline policies
	for _, policyName := range inlinePoliciesOutput.PolicyNames {
		_, err := clients.IAMClient.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   &roleName,
			PolicyName: &policyName,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete inline policy %s from role %s: %w", policyName, roleName, err)
		}
	}

	span.SetAttributes(attribute.Int("inline_policies_deleted", len(inlinePoliciesOutput.PolicyNames)))

	// Delete the role
	_, err = clients.IAMClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: &roleName,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete role %s: %w", roleName, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	return nil
}
