package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// cleanupKubernetesLoadBalancers deletes any Classic ELBs tagged with the cluster name.
// Kubernetes-created load balancers (e.g. from Envoy Gateway) are not managed by
// Terraform and must be cleaned up before destroying the VPC and subnets.
func cleanupKubernetesLoadBalancers(ctx context.Context, region, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupKubernetesLoadBalancers")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("region", region),
	)

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := elb.NewFromConfig(cfg)
	tagKey := "kubernetes.io/cluster/" + clusterName

	lbs, err := client.DescribeLoadBalancers(ctx, &elb.DescribeLoadBalancersInput{})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe load balancers: %w", err)
	}

	if len(lbs.LoadBalancerDescriptions) == 0 {
		slog.Info("No load balancers found in region", "region", region)
	}

	var names []string
	for _, lb := range lbs.LoadBalancerDescriptions {
		names = append(names, *lb.LoadBalancerName)
	}

	var deleted int
	if len(names) > 0 {
		tagsOutput, err := client.DescribeTags(ctx, &elb.DescribeTagsInput{
			LoadBalancerNames: names,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to describe load balancer tags: %w", err)
		}

		for _, tagDesc := range tagsOutput.TagDescriptions {
			for _, tag := range tagDesc.Tags {
				if tag.Key != nil && *tag.Key == tagKey {
					slog.Info("Deleting Kubernetes-created load balancer",
						"name", *tagDesc.LoadBalancerName,
					)
					_, err := client.DeleteLoadBalancer(ctx, &elb.DeleteLoadBalancerInput{
						LoadBalancerName: tagDesc.LoadBalancerName,
					})
					if err != nil {
						span.RecordError(err)
						return fmt.Errorf("failed to delete load balancer %s: %w", *tagDesc.LoadBalancerName, err)
					}
					deleted++
					break
				}
			}
		}
	}

	span.SetAttributes(attribute.Int("load_balancers_deleted", deleted))
	slog.Info("Kubernetes load balancer cleanup complete", "deleted", deleted)

	// Clean up orphaned k8s-elb-* security groups left behind by deleted load balancers
	ec2Client := ec2.NewFromConfig(cfg)
	sgDeleted, err := cleanupK8sELBSecurityGroups(ctx, ec2Client, tagKey)
	if err != nil {
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Int("security_groups_deleted", sgDeleted))

	return nil
}

// EC2Client defines the EC2 operations needed for cleanup.
type EC2Client interface {
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	RevokeSecurityGroupIngress(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
}

// cleanupK8sELBSecurityGroups deletes security groups with names prefixed with
// "k8s-elb-" that are tagged with the given cluster tag key. These are created
// by Kubernetes for load balancers but not always cleaned up when the ELB is deleted.
func cleanupK8sELBSecurityGroups(ctx context.Context, client EC2Client, tagKey string) (int, error) {
	sgs, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("group-name"),
				Values: []string{"k8s-elb-*"},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to describe security groups: %w", err)
	}

	var deleted int
	for _, sg := range sgs.SecurityGroups {
		if sg.GroupName == nil || !strings.HasPrefix(*sg.GroupName, "k8s-elb-") {
			continue
		}

		// Check for the cluster tag
		for _, tag := range sg.Tags {
			if tag.Key != nil && *tag.Key == tagKey {
				slog.Info("Deleting orphaned Kubernetes ELB security group",
					"id", *sg.GroupId,
					"name", *sg.GroupName,
				)
				// Remove ingress rules in other SGs that reference this one
				if err := revokeReferencingRules(ctx, client, *sg.GroupId); err != nil {
					return deleted, err
				}
				if err := deleteSecurityGroupWithRetry(ctx, client, *sg.GroupId); err != nil {
					return deleted, err
				}
				deleted++
				break
			}
		}
	}

	slog.Info("Kubernetes ELB security group cleanup complete", "deleted", deleted)
	return deleted, nil
}

// revokeReferencingRules finds and removes ingress rules in other security groups
// that reference the given security group ID. This is necessary because Kubernetes
// adds rules to the node security group allowing traffic from the ELB security group,
// and these references prevent the ELB security group from being deleted.
func revokeReferencingRules(ctx context.Context, client EC2Client, groupID string) error {
	// Find security groups that have ingress rules referencing this SG
	sgs, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("ip-permission.group-id"),
				Values: []string{groupID},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to find security groups referencing %s: %w", groupID, err)
	}

	for _, sg := range sgs.SecurityGroups {
		// Build list of ingress rules that reference our target SG
		var permissionsToRevoke []ec2types.IpPermission
		for _, perm := range sg.IpPermissions {
			for _, pair := range perm.UserIdGroupPairs {
				if pair.GroupId != nil && *pair.GroupId == groupID {
					permissionsToRevoke = append(permissionsToRevoke, ec2types.IpPermission{
						IpProtocol:       perm.IpProtocol,
						FromPort:         perm.FromPort,
						ToPort:           perm.ToPort,
						UserIdGroupPairs: []ec2types.UserIdGroupPair{pair},
					})
				}
			}
		}

		if len(permissionsToRevoke) == 0 {
			continue
		}

		slog.Info("Revoking ingress rules referencing ELB security group",
			"source_sg", groupID,
			"target_sg", *sg.GroupId,
			"rules", len(permissionsToRevoke),
		)

		_, err := client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
			GroupId:       sg.GroupId,
			IpPermissions: permissionsToRevoke,
		})
		if err != nil {
			return fmt.Errorf("failed to revoke ingress rules in %s referencing %s: %w", *sg.GroupId, groupID, err)
		}
	}

	return nil
}

// deleteSecurityGroupWithRetry attempts to delete a security group, retrying on
// DependencyViolation errors. After deleting an ELB, AWS takes time to release
// the attached ENIs, so the security group can't be deleted immediately.
func deleteSecurityGroupWithRetry(ctx context.Context, client EC2Client, groupID string) error {
	const maxAttempts = 12
	const retryInterval = 5 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: aws.String(groupID),
		})
		if err == nil {
			return nil
		}

		if !strings.Contains(err.Error(), "DependencyViolation") {
			return fmt.Errorf("failed to delete security group %s: %w", groupID, err)
		}

		if attempt == maxAttempts {
			return fmt.Errorf("failed to delete security group %s after %d attempts: %w", groupID, maxAttempts, err)
		}

		slog.Info("Waiting for ELB cleanup before deleting security group",
			"id", groupID,
			"attempt", attempt,
			"max_attempts", maxAttempts,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
	return nil
}
