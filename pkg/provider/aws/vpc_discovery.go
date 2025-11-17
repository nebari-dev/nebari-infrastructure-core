package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// DiscoverVPC discovers existing VPC infrastructure by querying AWS with NIC tags
func (p *Provider) DiscoverVPC(ctx context.Context, clients *Clients, clusterName string) (*VPCState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.DiscoverVPC")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	// Query VPCs with NIC tags
	filters := BuildTagFilter(clusterName, ResourceTypeVPC)

	result, err := clients.EC2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: filters,
	})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("DescribeVpcs API call failed: %w", err)
	}

	if len(result.Vpcs) == 0 {
		// No VPC found - this is ok, means infrastructure doesn't exist yet
		return nil, nil
	}

	if len(result.Vpcs) > 1 {
		err := fmt.Errorf("multiple VPCs found for cluster %s (expected 0 or 1, found %d)", clusterName, len(result.Vpcs))
		span.RecordError(err)
		return nil, err
	}

	vpc := result.Vpcs[0]

	vpcState := &VPCState{
		VPCID: *vpc.VpcId,
		CIDR:  *vpc.CidrBlock,
		Tags:  convertEC2TagsToMap(vpc.Tags),
	}

	// Discover associated resources
	if err := p.discoverVPCResources(ctx, clients, clusterName, vpcState); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to discover VPC resources: %w", err)
	}

	span.SetAttributes(
		attribute.String("vpc_id", vpcState.VPCID),
		attribute.String("vpc_cidr", vpcState.CIDR),
		attribute.Int("public_subnets", len(vpcState.PublicSubnetIDs)),
		attribute.Int("private_subnets", len(vpcState.PrivateSubnetIDs)),
	)

	return vpcState, nil
}

// discoverVPCResources discovers all resources associated with the VPC
func (p *Provider) discoverVPCResources(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverVPCResources")
	defer span.End()

	// Discover subnets
	if err := p.discoverSubnets(ctx, clients, clusterName, vpcState); err != nil {
		return fmt.Errorf("failed to discover subnets: %w", err)
	}

	// Discover internet gateway
	if err := p.discoverInternetGateway(ctx, clients, clusterName, vpcState); err != nil {
		return fmt.Errorf("failed to discover internet gateway: %w", err)
	}

	// Discover NAT gateways
	if err := p.discoverNATGateways(ctx, clients, clusterName, vpcState); err != nil {
		return fmt.Errorf("failed to discover NAT gateways: %w", err)
	}

	// Discover route tables
	if err := p.discoverRouteTables(ctx, clients, clusterName, vpcState); err != nil {
		return fmt.Errorf("failed to discover route tables: %w", err)
	}

	// Discover security groups
	if err := p.discoverSecurityGroups(ctx, clients, clusterName, vpcState); err != nil {
		return fmt.Errorf("failed to discover security groups: %w", err)
	}

	return nil
}

// discoverSubnets discovers subnets tagged with NIC tags
func (p *Provider) discoverSubnets(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverSubnets")
	defer span.End()

	filters := BuildTagFilter(clusterName, ResourceTypeSubnet)
	filters = append(filters, types.Filter{
		Name:   aws.String("vpc-id"),
		Values: []string{vpcState.VPCID},
	})

	result, err := clients.EC2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: filters,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DescribeSubnets API call failed: %w", err)
	}

	publicSubnets := []string{}
	privateSubnets := []string{}
	azs := []string{}

	for _, subnet := range result.Subnets {
		subnetID := *subnet.SubnetId
		az := *subnet.AvailabilityZone

		// Check if public or private by looking at tags
		tags := convertEC2TagsToMap(subnet.Tags)
		if _, ok := tags["kubernetes.io/role/public-elb"]; ok {
			publicSubnets = append(publicSubnets, subnetID)
		} else {
			privateSubnets = append(privateSubnets, subnetID)
		}

		// Collect unique AZs
		if !contains(azs, az) {
			azs = append(azs, az)
		}
	}

	vpcState.PublicSubnetIDs = publicSubnets
	vpcState.PrivateSubnetIDs = privateSubnets
	vpcState.AvailabilityZones = azs

	span.SetAttributes(
		attribute.Int("public_subnets", len(publicSubnets)),
		attribute.Int("private_subnets", len(privateSubnets)),
		attribute.Int("availability_zones", len(azs)),
	)

	return nil
}

// discoverInternetGateway discovers the internet gateway attached to the VPC
func (p *Provider) discoverInternetGateway(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverInternetGateway")
	defer span.End()

	filters := BuildTagFilter(clusterName, ResourceTypeInternetGateway)

	result, err := clients.EC2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: filters,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DescribeInternetGateways API call failed: %w", err)
	}

	if len(result.InternetGateways) > 0 {
		vpcState.InternetGatewayID = *result.InternetGateways[0].InternetGatewayId
		span.SetAttributes(attribute.String("igw_id", vpcState.InternetGatewayID))
	}

	return nil
}

// discoverNATGateways discovers NAT gateways in the VPC
func (p *Provider) discoverNATGateways(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverNATGateways")
	defer span.End()

	filters := BuildTagFilter(clusterName, ResourceTypeNATGateway)

	result, err := clients.EC2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: filters,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DescribeNatGateways API call failed: %w", err)
	}

	natIDs := []string{}
	for _, nat := range result.NatGateways {
		// Only include active NAT gateways
		if nat.State == types.NatGatewayStateAvailable {
			natIDs = append(natIDs, *nat.NatGatewayId)
		}
	}

	vpcState.NATGatewayIDs = natIDs
	span.SetAttributes(attribute.Int("nat_gateways", len(natIDs)))

	return nil
}

// discoverRouteTables discovers route tables in the VPC
func (p *Provider) discoverRouteTables(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverRouteTables")
	defer span.End()

	filters := BuildTagFilter(clusterName, ResourceTypeRouteTable)
	filters = append(filters, types.Filter{
		Name:   aws.String("vpc-id"),
		Values: []string{vpcState.VPCID},
	})

	result, err := clients.EC2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: filters,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DescribeRouteTables API call failed: %w", err)
	}

	publicRT := ""
	privateRTs := []string{}

	for _, rt := range result.RouteTables {
		rtID := *rt.RouteTableId
		tags := convertEC2TagsToMap(rt.Tags)

		// Check name tag to determine if public or private
		if name, ok := tags["Name"]; ok {
			if containsSubstring([]string{name}, "public") {
				publicRT = rtID
			} else if containsSubstring([]string{name}, "private") {
				privateRTs = append(privateRTs, rtID)
			}
		}
	}

	vpcState.PublicRouteTableID = publicRT
	vpcState.PrivateRouteTableIDs = privateRTs

	span.SetAttributes(
		attribute.Bool("public_rt_found", publicRT != ""),
		attribute.Int("private_rts", len(privateRTs)),
	)

	return nil
}

// discoverSecurityGroups discovers security groups in the VPC
func (p *Provider) discoverSecurityGroups(ctx context.Context, clients *Clients, clusterName string, vpcState *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.discoverSecurityGroups")
	defer span.End()

	filters := BuildTagFilter(clusterName, ResourceTypeSecurityGroup)
	filters = append(filters, types.Filter{
		Name:   aws.String("vpc-id"),
		Values: []string{vpcState.VPCID},
	})

	result, err := clients.EC2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("DescribeSecurityGroups API call failed: %w", err)
	}

	sgIDs := []string{}
	for _, sg := range result.SecurityGroups {
		sgIDs = append(sgIDs, *sg.GroupId)
	}

	vpcState.SecurityGroupIDs = sgIDs
	span.SetAttributes(attribute.Int("security_groups", len(sgIDs)))

	return nil
}

// convertEC2TagsToMap converts EC2 tags to a map
func convertEC2TagsToMap(tags []types.Tag) map[string]string {
	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			tagMap[*tag.Key] = *tag.Value
		}
	}
	return tagMap
}

// contains checks if a string slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// containsSubstring checks if any string in the slice contains the substring
func containsSubstring(slice []string, substr string) bool {
	for _, s := range slice {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
