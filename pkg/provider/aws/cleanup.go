package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
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

//nolint:unused // Used in future tasks for ELBv2 cleanup
func newELBv2Client(ctx context.Context, region string) (ELBv2Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return elbv2.NewFromConfig(cfg), nil
}

// ELBClient defines the Classic ELB operations needed for cleanup.
type ELBClient interface {
	DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elb.DeleteLoadBalancerInput, optFns ...func(*elb.Options)) (*elb.DeleteLoadBalancerOutput, error)
}

// cleanupKubernetesLoadBalancers deletes any Classic ELBs tagged with the cluster name.
// Kubernetes-created load balancers (e.g. from Envoy Gateway) are not managed by
// Terraform and must be cleaned up before destroying the VPC and subnets.
func cleanupKubernetesLoadBalancers(ctx context.Context, elbClient ELBClient, ec2Client EC2Client, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.cleanupKubernetesLoadBalancers")
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
	sgDeleted, err := cleanupK8sSecurityGroupsByPrefix(ctx, ec2Client, tagKey, "k8s-elb-")
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

// ELBv2Client defines the Application/Network Load Balancer operations needed
// for cleanup. Scoped to just the verbs we call so the interface is mockable.
type ELBv2Client interface {
	DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
	DeleteLoadBalancer(ctx context.Context, params *elbv2.DeleteLoadBalancerInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteLoadBalancerOutput, error)
	DescribeTargetGroups(ctx context.Context, params *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
	DeleteTargetGroup(ctx context.Context, params *elbv2.DeleteTargetGroupInput, optFns ...func(*elbv2.Options)) (*elbv2.DeleteTargetGroupOutput, error)
}

// clusterTagELBv2 is the tag key the AWS Load Balancer Controller attaches to
// every NLB/ALB/TargetGroup it creates, scoped to the EKS cluster name.
const clusterTagELBv2 = "elbv2.k8s.aws/cluster"

// cleanupELBv2LoadBalancers deletes all NLBs/ALBs tagged
// elbv2.k8s.aws/cluster=<clusterName>. Waits for each deletion to complete via
// the elbv2 LoadBalancersDeletedWaiter so ENIs are released before the caller
// moves on to VPC teardown.
func cleanupELBv2LoadBalancers(ctx context.Context, client ELBv2Client, clusterName string) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.cleanupELBv2LoadBalancers")
	defer span.End()
	span.SetAttributes(attribute.String("cluster_name", clusterName))

	var arns []string
	nameByARN := map[string]string{}
	paginator := elbv2.NewDescribeLoadBalancersPaginator(client, &elbv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return 0, fmt.Errorf("failed to describe elbv2 load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			if lb.LoadBalancerArn == nil {
				continue
			}
			arns = append(arns, *lb.LoadBalancerArn)
			if lb.LoadBalancerName != nil {
				nameByARN[*lb.LoadBalancerArn] = *lb.LoadBalancerName
			}
		}
	}

	if len(arns) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "No elbv2 load balancers found").
			WithResource("load-balancer").WithAction("discovering"))
		span.SetAttributes(attribute.Int("load_balancers_deleted", 0))
		return 0, nil
	}

	// elbv2 DescribeTags accepts up to 20 ARNs per call.
	const maxTagBatch = 20
	var matchingARNs []string
	for i := 0; i < len(arns); i += maxTagBatch {
		end := min(i+maxTagBatch, len(arns))
		batch := arns[i:end]
		out, err := client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: batch})
		if err != nil {
			span.RecordError(err)
			return 0, fmt.Errorf("failed to describe elbv2 tags: %w", err)
		}
		for _, desc := range out.TagDescriptions {
			if desc.ResourceArn == nil {
				continue
			}
			for _, tag := range desc.Tags {
				if tag.Key != nil && *tag.Key == clusterTagELBv2 && tag.Value != nil && *tag.Value == clusterName {
					matchingARNs = append(matchingARNs, *desc.ResourceArn)
					break
				}
			}
		}
	}

	var deleted int
	waiter := elbv2.NewLoadBalancersDeletedWaiter(client)
	for _, arn := range matchingARNs {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleting elbv2 load balancer: %s", nameByARN[arn])).
			WithResource("load-balancer").WithAction("deleting"))

		if _, err := client.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{LoadBalancerArn: aws.String(arn)}); err != nil {
			span.RecordError(err)
			return deleted, fmt.Errorf("failed to delete elbv2 load balancer %s: %w", arn, err)
		}

		if err := waiter.Wait(ctx, &elbv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{arn}}, 5*time.Minute); err != nil {
			span.RecordError(err)
			return deleted, fmt.Errorf("timed out waiting for elbv2 load balancer %s to delete: %w", arn, err)
		}
		deleted++
	}

	span.SetAttributes(attribute.Int("load_balancers_deleted", deleted))
	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("elbv2 load balancer cleanup complete: %d deleted", deleted)).
		WithResource("load-balancer").WithAction("cleanup"))
	return deleted, nil
}

// cleanupELBv2TargetGroups deletes all target groups tagged
// elbv2.k8s.aws/cluster=<clusterName>. Must be called after
// cleanupELBv2LoadBalancers so no load balancer still references them.
func cleanupELBv2TargetGroups(ctx context.Context, client ELBv2Client, clusterName string) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.cleanupELBv2TargetGroups")
	defer span.End()
	span.SetAttributes(attribute.String("cluster_name", clusterName))

	var arns []string
	paginator := elbv2.NewDescribeTargetGroupsPaginator(client, &elbv2.DescribeTargetGroupsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			span.RecordError(err)
			return 0, fmt.Errorf("failed to describe target groups: %w", err)
		}
		for _, tg := range page.TargetGroups {
			if tg.TargetGroupArn != nil {
				arns = append(arns, *tg.TargetGroupArn)
			}
		}
	}

	if len(arns) == 0 {
		span.SetAttributes(attribute.Int("target_groups_deleted", 0))
		return 0, nil
	}

	const maxTagBatch = 20
	var matchingARNs []string
	for i := 0; i < len(arns); i += maxTagBatch {
		end := min(i+maxTagBatch, len(arns))
		batch := arns[i:end]
		out, err := client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: batch})
		if err != nil {
			span.RecordError(err)
			return 0, fmt.Errorf("failed to describe target group tags: %w", err)
		}
		for _, desc := range out.TagDescriptions {
			if desc.ResourceArn == nil {
				continue
			}
			for _, tag := range desc.Tags {
				if tag.Key != nil && *tag.Key == clusterTagELBv2 && tag.Value != nil && *tag.Value == clusterName {
					matchingARNs = append(matchingARNs, *desc.ResourceArn)
					break
				}
			}
		}
	}

	var deleted int
	for _, arn := range matchingARNs {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleting elbv2 target group: %s", arn)).
			WithResource("target-group").WithAction("deleting"))
		if _, err := client.DeleteTargetGroup(ctx, &elbv2.DeleteTargetGroupInput{TargetGroupArn: aws.String(arn)}); err != nil {
			span.RecordError(err)
			return deleted, fmt.Errorf("failed to delete target group %s: %w", arn, err)
		}
		deleted++
	}

	span.SetAttributes(attribute.Int("target_groups_deleted", deleted))
	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Target group cleanup complete: %d deleted", deleted)).
		WithResource("target-group").WithAction("cleanup"))
	return deleted, nil
}

// cleanupK8sSecurityGroupsByPrefix deletes security groups whose GroupName starts
// with the given prefix AND carry the given cluster tag key. Used to clean up
// Kubernetes-created ELB/NLB security groups (k8s-elb-*, k8s-traffic-*) that
// are not always removed when their owning load balancer is deleted.
func cleanupK8sSecurityGroupsByPrefix(ctx context.Context, client EC2Client, tagKey, namePrefix string) (int, error) {
	sgs, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("group-name"),
				Values: []string{namePrefix + "*"},
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to describe security groups: %w", err)
	}

	var deleted int
	for _, sg := range sgs.SecurityGroups {
		if sg.GroupName == nil || !strings.HasPrefix(*sg.GroupName, namePrefix) {
			continue
		}

		for _, tag := range sg.Tags {
			if tag.Key != nil && *tag.Key == tagKey {
				status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Deleting orphaned Kubernetes security group: %s (%s)", *sg.GroupName, *sg.GroupId)).
					WithResource("security-group").
					WithAction("deleting"))
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

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, fmt.Sprintf("Kubernetes security group cleanup complete for prefix %s: %d deleted", namePrefix, deleted)).
		WithResource("security-group").
		WithAction("cleanup"))
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
