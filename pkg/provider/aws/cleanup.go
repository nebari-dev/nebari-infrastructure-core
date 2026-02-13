package aws

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/smithy-go"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func newELBClient(ctx context.Context, region string) (ELBClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return elb.NewFromConfig(cfg), nil
}

func newEC2Client(ctx context.Context, region string) (EC2Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return ec2.NewFromConfig(cfg), nil
}

// ELBClient defines the Classic ELB operations needed for cleanup.
type ELBClient interface {
	DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error)
}

// ELBPaginator defines the paginator interface for listing load balancers.
type ELBPaginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
}

// cleanupKubernetesLoadBalancers deletes any Classic ELBs tagged with the cluster name.
// Kubernetes-created load balancers (e.g. from Envoy Gateway) are not managed by
// Terraform and must be cleaned up before destroying the VPC and subnets.
func cleanupKubernetesLoadBalancers(ctx context.Context, elbClient ELBClient, ec2Client EC2Client, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupKubernetesLoadBalancers")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	tagKey := "kubernetes.io/cluster/" + clusterName

	// Use paginator to handle accounts with many ELBs
	paginator := elb.NewDescribeLoadBalancersPaginator(elbClient, &elb.DescribeLoadBalancersInput{})
	var names []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to describe load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancerDescriptions {
			names = append(names, *lb.LoadBalancerName)
		}
	}

	if len(names) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "No load balancers found").
			WithResource("load-balancer").
			WithAction("discovering"))
	}

	// Collect all tag descriptions, batching in chunks of 20 (API limit)
	const maxTagBatch = 20
	var allTagDescs []elbtypes.TagDescription
	for i := 0; i < len(names); i += maxTagBatch {
		end := min(i+maxTagBatch, len(names))
		batch := names[i:end]

		tagsOutput, err := elbClient.DescribeTags(ctx, &elb.DescribeTagsInput{
			LoadBalancerNames: batch,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to describe load balancer tags: %w", err)
		}
		allTagDescs = append(allTagDescs, tagsOutput.TagDescriptions...)
	}

	var deleted int
	for _, tagDesc := range allTagDescs {
		for _, tag := range tagDesc.Tags {
			if tag.Key != nil && *tag.Key == tagKey {
				status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleting Kubernetes-created load balancer: %s", *tagDesc.LoadBalancerName)).
					WithResource("load-balancer").
					WithAction("deleting"))
				_, err := elbClient.DeleteLoadBalancer(ctx, &elb.DeleteLoadBalancerInput{
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

	span.SetAttributes(attribute.Int("load_balancers_deleted", deleted))
	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kubernetes load balancer cleanup complete: %d deleted", deleted)).
		WithResource("load-balancer").
		WithAction("cleanup"))

	// Clean up orphaned k8s-elb-* security groups left behind by deleted load balancers
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
		if sg.GroupName == nil || !hasPrefix(*sg.GroupName, "k8s-elb-") {
			continue
		}

		// Check for the cluster tag
		for _, tag := range sg.Tags {
			if tag.Key != nil && *tag.Key == tagKey {
				status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleting orphaned Kubernetes ELB security group: %s (%s)", *sg.GroupName, *sg.GroupId)).
					WithResource("security-group").
					WithAction("deleting"))
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

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kubernetes ELB security group cleanup complete: %d deleted", deleted)).
		WithResource("security-group").
		WithAction("cleanup"))
	return deleted, nil
}

// hasPrefix checks if s starts with prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
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

		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Revoking %d ingress rules referencing ELB security group %s in %s", len(permissionsToRevoke), groupID, *sg.GroupId)).
			WithResource("security-group").
			WithAction("revoking"))

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

		// Check for DependencyViolation using smithy error codes
		var apiErr smithy.APIError
		if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "DependencyViolation" {
			return fmt.Errorf("failed to delete security group %s: %w", groupID, err)
		}

		if attempt == maxAttempts {
			return fmt.Errorf("failed to delete security group %s after %d attempts: %w", groupID, maxAttempts, err)
		}

		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Waiting for ELB cleanup before deleting security group %s (attempt %d/%d)", groupID, attempt, maxAttempts)).
			WithResource("security-group").
			WithAction("waiting"))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
	return nil
}
