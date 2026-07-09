package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Teardown of a ROSA HCP cluster can leave resources in the customer VPC that
// block `DeleteVpc` but are NOT in NIC's OpenTofu state, so `tofu destroy` cannot
// remove them:
//
//   - The security group ROSA's PrivateLink VPC endpoint creates
//     (…-vpce-private-router). ROSA deletes the endpoint on uninstall but can
//     leave its security group behind, and a non-default SG blocks VPC deletion.
//   - Load balancers created in-cluster by the AWS cloud controller for
//     Service type=LoadBalancer (e.g. the Envoy Gateway NLB), plus the ENIs they
//     attach to the VPC's subnets.
//
// sweepVPCOrphans removes these directly via the AWS SDK. It is best-effort and
// idempotent: callers run it between `tofu destroy` attempts.

const (
	eniDrainTimeout = 5 * time.Minute
	eniDrainPoll    = 15 * time.Second
	sgDeleteRetries = 6
)

func sweepVPCOrphans(ctx context.Context, region, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "openshift.sweepVPCOrphans")
	defer span.End()

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	ec2c := ec2.NewFromConfig(cfg)
	v2 := elbv2.NewFromConfig(cfg)
	classic := elb.NewFromConfig(cfg)

	if err := deleteVPCLoadBalancers(ctx, v2, classic, vpcID); err != nil {
		return err
	}
	// ENIs from a just-deleted LB take a little while to detach; wait so the
	// security-group and VPC deletes that follow don't hit DependencyViolation.
	if err := waitENIsDrained(ctx, ec2c, vpcID); err != nil {
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("ENIs still draining in %s: %v (continuing)", vpcID, err)).
			WithResource("eni").WithAction("draining"))
	}
	return deleteNonDefaultSecurityGroups(ctx, ec2c, vpcID)
}

// deleteVPCLoadBalancers deletes every NLB/ALB (elbv2) and Classic ELB in the VPC.
func deleteVPCLoadBalancers(ctx context.Context, v2 *elbv2.Client, classic *elb.Client, vpcID string) error {
	v2out, err := v2.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("describe elbv2 load balancers: %w", err)
	}
	for _, lb := range v2out.LoadBalancers {
		if aws.ToString(lb.VpcId) != vpcID {
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Deleting orphaned load balancer %s", aws.ToString(lb.LoadBalancerName))).
			WithResource("load-balancer").WithAction("deleting"))
		if _, derr := v2.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{
			LoadBalancerArn: lb.LoadBalancerArn,
		}); derr != nil {
			return fmt.Errorf("delete elbv2 %s: %w", aws.ToString(lb.LoadBalancerArn), derr)
		}
	}

	cout, err := classic.DescribeLoadBalancers(ctx, &elb.DescribeLoadBalancersInput{})
	if err != nil {
		return fmt.Errorf("describe classic load balancers: %w", err)
	}
	for _, lb := range cout.LoadBalancerDescriptions {
		if aws.ToString(lb.VPCId) != vpcID {
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Deleting orphaned classic ELB %s", aws.ToString(lb.LoadBalancerName))).
			WithResource("load-balancer").WithAction("deleting"))
		if _, derr := classic.DeleteLoadBalancer(ctx, &elb.DeleteLoadBalancerInput{
			LoadBalancerName: lb.LoadBalancerName,
		}); derr != nil {
			return fmt.Errorf("delete classic elb %s: %w", aws.ToString(lb.LoadBalancerName), derr)
		}
	}
	return nil
}

// waitENIsDrained polls until no network interfaces remain in the VPC (deleting
// any that have detached to "available"), or the timeout elapses.
func waitENIsDrained(ctx context.Context, ec2c *ec2.Client, vpcID string) error {
	deadline := time.Now().Add(eniDrainTimeout)
	for {
		out, err := ec2c.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{
			Filters: []ec2types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
		})
		if err != nil {
			return fmt.Errorf("describe network interfaces: %w", err)
		}
		if len(out.NetworkInterfaces) == 0 {
			return nil
		}
		// Best-effort delete of detached ENIs (LB-managed ones auto-delete once
		// the LB is gone; manual delete is a no-op error we ignore).
		for _, eni := range out.NetworkInterfaces {
			if eni.Status == ec2types.NetworkInterfaceStatusAvailable {
				_, _ = ec2c.DeleteNetworkInterface(ctx, &ec2.DeleteNetworkInterfaceInput{
					NetworkInterfaceId: eni.NetworkInterfaceId,
				})
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d network interface(s) still present after %s", len(out.NetworkInterfaces), eniDrainTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(eniDrainPoll):
		}
	}
}

// deleteNonDefaultSecurityGroups revokes all rules on the VPC's non-default
// security groups (to break cross-references) and then deletes them, retrying to
// absorb the lag while ENIs finish detaching.
func deleteNonDefaultSecurityGroups(ctx context.Context, ec2c *ec2.Client, vpcID string) error {
	out, err := ec2c.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{{Name: aws.String("vpc-id"), Values: []string{vpcID}}},
	})
	if err != nil {
		return fmt.Errorf("describe security groups: %w", err)
	}

	var groups []ec2types.SecurityGroup
	for _, sg := range out.SecurityGroups {
		if aws.ToString(sg.GroupName) != "default" {
			groups = append(groups, sg)
		}
	}

	// Pass 1: strip all rules so inter-group references don't block deletion.
	for _, sg := range groups {
		if len(sg.IpPermissions) > 0 {
			_, _ = ec2c.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissions,
			})
		}
		if len(sg.IpPermissionsEgress) > 0 {
			_, _ = ec2c.RevokeSecurityGroupEgress(ctx, &ec2.RevokeSecurityGroupEgressInput{
				GroupId:       sg.GroupId,
				IpPermissions: sg.IpPermissionsEgress,
			})
		}
	}

	// Pass 2: delete, retrying while attached ENIs finish detaching.
	for _, sg := range groups {
		var derr error
		for i := 0; i < sgDeleteRetries; i++ {
			if _, derr = ec2c.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
				GroupId: sg.GroupId,
			}); derr == nil {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(eniDrainPoll):
			}
		}
		if derr != nil {
			return fmt.Errorf("delete security group %s: %w", aws.ToString(sg.GroupId), derr)
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Deleted orphaned security group %s", aws.ToString(sg.GroupId))).
			WithResource("security-group").WithAction("deleting"))
	}
	return nil
}
