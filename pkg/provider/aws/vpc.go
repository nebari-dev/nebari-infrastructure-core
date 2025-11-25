package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const (
	// DefaultVPCCIDR is the default CIDR block if not specified in configuration
	DefaultVPCCIDR = "10.10.0.0/16"

	// DefaultAZCount is the default number of availability zones to use
	DefaultAZCount = 3

	// Subnet CIDR calculations (for /16 VPC):
	// Public subnets: 10.10.0.0/20, 10.10.16.0/20, 10.10.32.0/20
	// Private subnets: 10.10.128.0/20, 10.10.144.0/20, 10.10.160.0/20
)

// createVPC creates a complete VPC with subnets, IGW, NAT gateways, route tables, and security groups
func (p *Provider) createVPC(ctx context.Context, clients *Clients, cfg *config.NebariConfig) (*VPCState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createVPC")
	defer span.End()

	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.String("region", awsCfg.Region),
	)

	// Determine VPC CIDR
	vpcCIDR := DefaultVPCCIDR
	if awsCfg.VPCCIDRBlock != "" {
		vpcCIDR = awsCfg.VPCCIDRBlock
	}

	// Determine availability zones
	azs, err := p.getAvailabilityZones(ctx, clients, awsCfg)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to get availability zones: %w", err)
	}

	// Ensure we have at least 2 AZs (EKS requirement)
	if len(azs) < 2 {
		err := fmt.Errorf("at least 2 availability zones required, got %d", len(azs))
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("vpc_cidr", vpcCIDR),
		attribute.StringSlice("availability_zones", azs),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating VPC").
		WithResource("vpc").
		WithAction("creating").
		WithMetadata("cidr", vpcCIDR).
		WithMetadata("availability_zones", len(azs)))

	// Step 1: Create VPC
	vpcState := &VPCState{
		CIDR:              vpcCIDR,
		AvailabilityZones: azs,
	}

	vpc, err := p.createVPCResource(ctx, clients, clusterName, vpcCIDR, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create VPC: %w", err)
	}
	vpcState.VPCID = *vpc.VpcId

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC created").
		WithResource("vpc").
		WithAction("created").
		WithMetadata("vpc_id", vpcState.VPCID))

	// Step 2: Enable DNS hostnames and DNS support
	if err := p.enableVPCDNS(ctx, clients, vpcState.VPCID); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to enable VPC DNS: %w", err)
	}

	// Step 3: Create Internet Gateway
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating internet gateway").
		WithResource("internet-gateway").
		WithAction("creating"))

	igwID, err := p.createInternetGateway(ctx, clients, clusterName, vpcState.VPCID, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create internet gateway: %w", err)
	}
	vpcState.InternetGatewayID = igwID

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Internet gateway created").
		WithResource("internet-gateway").
		WithAction("created").
		WithMetadata("igw_id", igwID))

	// Step 4: Create public and private subnets
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating public subnets").
		WithResource("subnet").
		WithAction("creating").
		WithMetadata("az_count", len(azs)))

	publicSubnets, err := p.createSubnets(ctx, clients, clusterName, vpcState.VPCID, vpcCIDR, azs, true, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create public subnets: %w", err)
	}
	vpcState.PublicSubnetIDs = publicSubnets

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Public subnets created").
		WithResource("subnet").
		WithAction("created").
		WithMetadata("count", len(publicSubnets)))

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating private subnets").
		WithResource("subnet").
		WithAction("creating").
		WithMetadata("az_count", len(azs)))

	privateSubnets, err := p.createSubnets(ctx, clients, clusterName, vpcState.VPCID, vpcCIDR, azs, false, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create private subnets: %w", err)
	}
	vpcState.PrivateSubnetIDs = privateSubnets

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Private subnets created").
		WithResource("subnet").
		WithAction("created").
		WithMetadata("count", len(privateSubnets)))

	// Step 5: Create NAT Gateways (one per public subnet for HA)
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating NAT gateways").
		WithResource("nat-gateway").
		WithAction("creating").
		WithMetadata("count", len(publicSubnets)))

	natGatewayIDs, err := p.createNATGateways(ctx, clients, clusterName, publicSubnets, azs, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create NAT gateways: %w", err)
	}
	vpcState.NATGatewayIDs = natGatewayIDs

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "NAT gateways created").
		WithResource("nat-gateway").
		WithAction("created").
		WithMetadata("count", len(natGatewayIDs)))

	// Step 6: Create route tables and routes
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating public route table").
		WithResource("route-table").
		WithAction("creating"))

	publicRouteTableID, err := p.createPublicRouteTable(ctx, clients, clusterName, vpcState.VPCID, igwID, publicSubnets, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create public route table: %w", err)
	}
	vpcState.PublicRouteTableID = publicRouteTableID

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Public route table created with IGW route").
		WithResource("route-table").
		WithAction("created").
		WithMetadata("route_table_id", publicRouteTableID))

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating private route tables").
		WithResource("route-table").
		WithAction("creating").
		WithMetadata("count", len(natGatewayIDs)))

	privateRouteTableIDs, err := p.createPrivateRouteTables(ctx, clients, clusterName, vpcState.VPCID, natGatewayIDs, privateSubnets, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create private route tables: %w", err)
	}
	vpcState.PrivateRouteTableIDs = privateRouteTableIDs

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Private route tables created with NAT routes").
		WithResource("route-table").
		WithAction("created").
		WithMetadata("count", len(privateRouteTableIDs)))

	// Step 7: Create security group for cluster
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating cluster security group").
		WithResource("security-group").
		WithAction("creating"))

	sgID, err := p.createClusterSecurityGroup(ctx, clients, clusterName, vpcState.VPCID, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create security group: %w", err)
	}
	vpcState.SecurityGroupIDs = []string{sgID}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster security group created").
		WithResource("security-group").
		WithAction("created").
		WithMetadata("security_group_id", sgID))

	// Step 8: Create VPC endpoints for private cluster access
	// VPC endpoints are required for nodes in private subnets to communicate with AWS services
	// This is critical when using private-only EKS endpoint access
	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating VPC endpoints for private cluster").
		WithResource("vpc-endpoint").
		WithAction("creating"))

	vpcEndpointIDs, err := p.createVPCEndpoints(ctx, clients, clusterName, vpcState.VPCID, vpcState.PrivateSubnetIDs, sgID, awsCfg.Region, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create VPC endpoints: %w", err)
	}
	vpcState.VPCEndpointIDs = vpcEndpointIDs

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC endpoints created").
		WithResource("vpc-endpoint").
		WithAction("created").
		WithMetadata("count", len(vpcEndpointIDs)))

	// Add user tags
	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeVPC)
	vpcState.Tags = MergeTags(ctx, nicTags, awsCfg.Tags)

	span.SetAttributes(
		attribute.String("vpc_id", vpcState.VPCID),
		attribute.Int("public_subnets", len(vpcState.PublicSubnetIDs)),
		attribute.Int("private_subnets", len(vpcState.PrivateSubnetIDs)),
		attribute.Int("nat_gateways", len(vpcState.NATGatewayIDs)),
	)

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC infrastructure created").
		WithResource("vpc").
		WithAction("created").
		WithMetadata("vpc_id", vpcState.VPCID).
		WithMetadata("public_subnets", len(vpcState.PublicSubnetIDs)).
		WithMetadata("private_subnets", len(vpcState.PrivateSubnetIDs)))

	return vpcState, nil
}

// createVPCResource creates the VPC resource
func (p *Provider) createVPCResource(ctx context.Context, clients *Clients, clusterName, cidr string, userTags map[string]string) (*types.Vpc, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createVPCResource")
	defer span.End()

	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeVPC)
	nicTags["Name"] = GenerateResourceName(clusterName, "vpc", "")
	allTags := MergeTags(ctx, nicTags, userTags)

	input := &ec2.CreateVpcInput{
		CidrBlock: aws.String(cidr),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeVpc,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	}

	result, err := clients.EC2Client.CreateVpc(ctx, input)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("CreateVpc API call failed: %w", err)
	}

	span.SetAttributes(attribute.String("vpc_id", *result.Vpc.VpcId))
	return result.Vpc, nil
}

// enableVPCDNS enables DNS hostnames and DNS support for the VPC (required for EKS)
func (p *Provider) enableVPCDNS(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.enableVPCDNS")
	defer span.End()

	// Enable DNS hostnames
	_, err := clients.EC2Client.ModifyVpcAttribute(ctx, &ec2.ModifyVpcAttributeInput{
		VpcId:              aws.String(vpcID),
		EnableDnsHostnames: &types.AttributeBooleanValue{Value: aws.Bool(true)},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to enable DNS hostnames: %w", err)
	}

	// Enable DNS support
	_, err = clients.EC2Client.ModifyVpcAttribute(ctx, &ec2.ModifyVpcAttributeInput{
		VpcId:            aws.String(vpcID),
		EnableDnsSupport: &types.AttributeBooleanValue{Value: aws.Bool(true)},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to enable DNS support: %w", err)
	}

	return nil
}

// createInternetGateway creates and attaches an internet gateway to the VPC
func (p *Provider) createInternetGateway(ctx context.Context, clients *Clients, clusterName, vpcID string, userTags map[string]string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createInternetGateway")
	defer span.End()

	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeInternetGateway)
	nicTags["Name"] = GenerateResourceName(clusterName, "igw", "")
	allTags := MergeTags(ctx, nicTags, userTags)

	// Create IGW
	createResult, err := clients.EC2Client.CreateInternetGateway(ctx, &ec2.CreateInternetGatewayInput{
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeInternetGateway,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("CreateInternetGateway API call failed: %w", err)
	}

	igwID := *createResult.InternetGateway.InternetGatewayId

	// Attach to VPC
	_, err = clients.EC2Client.AttachInternetGateway(ctx, &ec2.AttachInternetGatewayInput{
		VpcId:             aws.String(vpcID),
		InternetGatewayId: aws.String(igwID),
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("AttachInternetGateway API call failed: %w", err)
	}

	span.SetAttributes(attribute.String("igw_id", igwID))
	return igwID, nil
}

// getAvailabilityZones returns the list of availability zones to use
func (p *Provider) getAvailabilityZones(ctx context.Context, clients *Clients, awsCfg *Config) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.getAvailabilityZones")
	defer span.End()

	// If user specified AZs, use those
	if len(awsCfg.AvailabilityZones) > 0 {
		return awsCfg.AvailabilityZones, nil
	}

	// Otherwise, query AWS for available AZs in the region
	result, err := clients.EC2Client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("region-name"),
				Values: []string{awsCfg.Region},
			},
			{
				Name:   aws.String("state"),
				Values: []string{"available"},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("DescribeAvailabilityZones API call failed: %w", err)
	}

	if len(result.AvailabilityZones) == 0 {
		err := fmt.Errorf("no availability zones found in region %s", awsCfg.Region)
		span.RecordError(err)
		return nil, err
	}

	// Take first N AZs (default 3, or less if fewer available)
	maxAZs := DefaultAZCount
	if len(result.AvailabilityZones) < maxAZs {
		maxAZs = len(result.AvailabilityZones)
	}

	azs := make([]string, maxAZs)
	for i := 0; i < maxAZs; i++ {
		azs[i] = *result.AvailabilityZones[i].ZoneName
	}

	return azs, nil
}

// createSubnets creates either public or private subnets across multiple AZs
func (p *Provider) createSubnets(ctx context.Context, clients *Clients, clusterName, vpcID, vpcCIDR string, azs []string, public bool, userTags map[string]string) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createSubnets")
	defer span.End()

	subnetType := "private"
	if public {
		subnetType = "public"
	}

	span.SetAttributes(
		attribute.String("subnet_type", subnetType),
		attribute.Int("az_count", len(azs)),
	)

	subnetIDs := make([]string, 0, len(azs))

	for i, az := range azs {
		// Calculate subnet CIDR
		// For /16 VPC (10.10.0.0/16):
		// Public subnets: 10.10.0.0/20, 10.10.16.0/20, 10.10.32.0/20
		// Private subnets: 10.10.128.0/20, 10.10.144.0/20, 10.10.160.0/20
		subnetCIDR := p.calculateSubnetCIDR(vpcCIDR, i, public)

		nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeSubnet)
		nicTags["Name"] = GenerateResourceName(clusterName, "subnet", fmt.Sprintf("%s-%d", subnetType, i))
		nicTags[fmt.Sprintf("kubernetes.io/role/%s-elb", subnetType)] = "1" // EKS subnet tags
		allTags := MergeTags(ctx, nicTags, userTags)

		input := &ec2.CreateSubnetInput{
			VpcId:            aws.String(vpcID),
			CidrBlock:        aws.String(subnetCIDR),
			AvailabilityZone: aws.String(az),
			TagSpecifications: []types.TagSpecification{
				{
					ResourceType: types.ResourceTypeSubnet,
					Tags:         ConvertToEC2Tags(allTags),
				},
			},
		}

		result, err := clients.EC2Client.CreateSubnet(ctx, input)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("CreateSubnet API call failed for AZ %s: %w", az, err)
		}

		subnetID := *result.Subnet.SubnetId
		subnetIDs = append(subnetIDs, subnetID)

		// Enable auto-assign public IP for public subnets
		if public {
			_, err := clients.EC2Client.ModifySubnetAttribute(ctx, &ec2.ModifySubnetAttributeInput{
				SubnetId:            aws.String(subnetID),
				MapPublicIpOnLaunch: &types.AttributeBooleanValue{Value: aws.Bool(true)},
			})
			if err != nil {
				span.RecordError(err)
				return nil, fmt.Errorf("failed to enable auto-assign public IP for subnet %s: %w", subnetID, err)
			}
		}
	}

	span.SetAttributes(attribute.StringSlice(fmt.Sprintf("%s_subnet_ids", subnetType), subnetIDs))
	return subnetIDs, nil
}

// calculateSubnetCIDR calculates the CIDR for a subnet based on index and type
func (p *Provider) calculateSubnetCIDR(vpcCIDR string, index int, public bool) string {
	// Simple calculation for /16 VPC -> /20 subnets
	// This gives us 16 possible /20 subnets per VPC
	// Public: 0-7 (10.10.0.0/20, 10.10.16.0/20, ...)
	// Private: 8-15 (10.10.128.0/20, 10.10.144.0/20, ...)

	parts := strings.Split(vpcCIDR, "/")
	ipParts := strings.Split(parts[0], ".")

	baseOctet := 0
	if !public {
		baseOctet = 128 // Start private subnets at .128
	}

	thirdOctet := baseOctet + (index * 16)
	return fmt.Sprintf("%s.%s.%d.0/20", ipParts[0], ipParts[1], thirdOctet)
}

// createNATGateways creates NAT gateways in public subnets (one per AZ for HA)
func (p *Provider) createNATGateways(ctx context.Context, clients *Clients, clusterName string, publicSubnetIDs, azs []string, userTags map[string]string) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createNATGateways")
	defer span.End()

	natGatewayIDs := make([]string, 0, len(publicSubnetIDs))

	for i, subnetID := range publicSubnetIDs {
		// Allocate Elastic IP
		nicTagsEIP := GenerateBaseTags(ctx, clusterName, ResourceTypeEIP)
		nicTagsEIP["Name"] = GenerateResourceName(clusterName, "eip", fmt.Sprintf("nat-%d", i))
		allTagsEIP := MergeTags(ctx, nicTagsEIP, userTags)

		eipResult, err := clients.EC2Client.AllocateAddress(ctx, &ec2.AllocateAddressInput{
			Domain: types.DomainTypeVpc,
			TagSpecifications: []types.TagSpecification{
				{
					ResourceType: types.ResourceTypeElasticIp,
					Tags:         ConvertToEC2Tags(allTagsEIP),
				},
			},
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("AllocateAddress API call failed: %w", err)
		}

		// Create NAT Gateway
		nicTagsNAT := GenerateBaseTags(ctx, clusterName, ResourceTypeNATGateway)
		nicTagsNAT["Name"] = GenerateResourceName(clusterName, "nat", fmt.Sprintf("%d", i))
		allTagsNAT := MergeTags(ctx, nicTagsNAT, userTags)

		natResult, err := clients.EC2Client.CreateNatGateway(ctx, &ec2.CreateNatGatewayInput{
			SubnetId:     aws.String(subnetID),
			AllocationId: eipResult.AllocationId,
			TagSpecifications: []types.TagSpecification{
				{
					ResourceType: types.ResourceTypeNatgateway,
					Tags:         ConvertToEC2Tags(allTagsNAT),
				},
			},
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("CreateNatGateway API call failed: %w", err)
		}

		natGatewayIDs = append(natGatewayIDs, *natResult.NatGateway.NatGatewayId)
	}

	// Wait for NAT gateways to become available
	for _, natID := range natGatewayIDs {
		waiter := ec2.NewNatGatewayAvailableWaiter(clients.EC2Client)
		err := waiter.Wait(ctx, &ec2.DescribeNatGatewaysInput{
			NatGatewayIds: []string{natID},
		}, 10*time.Minute) // 10 minutes timeout
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("NAT gateway %s did not become available: %w", natID, err)
		}
	}

	span.SetAttributes(attribute.StringSlice("nat_gateway_ids", natGatewayIDs))
	return natGatewayIDs, nil
}

// createPublicRouteTable creates a route table for public subnets with route to IGW
func (p *Provider) createPublicRouteTable(ctx context.Context, clients *Clients, clusterName, vpcID, igwID string, publicSubnetIDs []string, userTags map[string]string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createPublicRouteTable")
	defer span.End()

	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeRouteTable)
	nicTags["Name"] = GenerateResourceName(clusterName, "rtb", "public")
	allTags := MergeTags(ctx, nicTags, userTags)

	// Create route table
	rtResult, err := clients.EC2Client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
		VpcId: aws.String(vpcID),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeRouteTable,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("CreateRouteTable API call failed: %w", err)
	}

	rtID := *rtResult.RouteTable.RouteTableId

	// Add route to internet gateway (0.0.0.0/0 -> IGW)
	_, err = clients.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
		RouteTableId:         aws.String(rtID),
		DestinationCidrBlock: aws.String("0.0.0.0/0"),
		GatewayId:            aws.String(igwID),
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("CreateRoute API call failed: %w", err)
	}

	// Associate with public subnets
	for _, subnetID := range publicSubnetIDs {
		_, err := clients.EC2Client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
			RouteTableId: aws.String(rtID),
			SubnetId:     aws.String(subnetID),
		})
		if err != nil {
			span.RecordError(err)
			return "", fmt.Errorf("AssociateRouteTable API call failed for subnet %s: %w", subnetID, err)
		}
	}

	span.SetAttributes(attribute.String("route_table_id", rtID))
	return rtID, nil
}

// createPrivateRouteTables creates route tables for private subnets with routes to NAT gateways
func (p *Provider) createPrivateRouteTables(ctx context.Context, clients *Clients, clusterName, vpcID string, natGatewayIDs, privateSubnetIDs []string, userTags map[string]string) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createPrivateRouteTables")
	defer span.End()

	routeTableIDs := make([]string, 0, len(privateSubnetIDs))

	// Create one route table per private subnet (each routes to its AZ's NAT gateway)
	for i, subnetID := range privateSubnetIDs {
		nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeRouteTable)
		nicTags["Name"] = GenerateResourceName(clusterName, "rtb", fmt.Sprintf("private-%d", i))
		allTags := MergeTags(ctx, nicTags, userTags)

		// Create route table
		rtResult, err := clients.EC2Client.CreateRouteTable(ctx, &ec2.CreateRouteTableInput{
			VpcId: aws.String(vpcID),
			TagSpecifications: []types.TagSpecification{
				{
					ResourceType: types.ResourceTypeRouteTable,
					Tags:         ConvertToEC2Tags(allTags),
				},
			},
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("CreateRouteTable API call failed: %w", err)
		}

		rtID := *rtResult.RouteTable.RouteTableId
		routeTableIDs = append(routeTableIDs, rtID)

		// Add route to NAT gateway (0.0.0.0/0 -> NAT)
		natID := natGatewayIDs[i]
		_, err = clients.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
			RouteTableId:         aws.String(rtID),
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			NatGatewayId:         aws.String(natID),
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("CreateRoute API call failed: %w", err)
		}

		// Associate with private subnet
		_, err = clients.EC2Client.AssociateRouteTable(ctx, &ec2.AssociateRouteTableInput{
			RouteTableId: aws.String(rtID),
			SubnetId:     aws.String(subnetID),
		})
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("AssociateRouteTable API call failed for subnet %s: %w", subnetID, err)
		}
	}

	span.SetAttributes(attribute.StringSlice("route_table_ids", routeTableIDs))
	return routeTableIDs, nil
}

// createClusterSecurityGroup creates a security group for the EKS cluster
func (p *Provider) createClusterSecurityGroup(ctx context.Context, clients *Clients, clusterName, vpcID string, userTags map[string]string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createClusterSecurityGroup")
	defer span.End()

	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeSecurityGroup)
	nicTags["Name"] = GenerateResourceName(clusterName, "sg", "cluster")
	allTags := MergeTags(ctx, nicTags, userTags)

	sgResult, err := clients.EC2Client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(GenerateResourceName(clusterName, "sg", "cluster")),
		Description: aws.String(fmt.Sprintf("Security group for %s EKS cluster", clusterName)),
		VpcId:       aws.String(vpcID),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeSecurityGroup,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("CreateSecurityGroup API call failed: %w", err)
	}

	sgID := *sgResult.GroupId
	span.SetAttributes(attribute.String("security_group_id", sgID))

	// Add security group rules for cluster-node communication
	if err := p.configureClusterSecurityGroupRules(ctx, clients, sgID); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to configure security group rules: %w", err)
	}

	return sgID, nil
}

// configureClusterSecurityGroupRules adds ingress and egress rules for EKS cluster-node communication
func (p *Provider) configureClusterSecurityGroupRules(ctx context.Context, clients *Clients, sgID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.configureClusterSecurityGroupRules")
	defer span.End()

	span.SetAttributes(attribute.String("security_group_id", sgID))

	// Ingress rules: Allow nodes to communicate with control plane
	ingressRules := []types.IpPermission{
		{
			// Allow HTTPS (443) from nodes to control plane
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(443),
			ToPort:     aws.Int32(443),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId:     aws.String(sgID),
					Description: aws.String("Allow nodes to communicate with cluster API server"),
				},
			},
		},
		{
			// Allow kubelet API (10250) from control plane to nodes
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(10250),
			ToPort:     aws.Int32(10250),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId:     aws.String(sgID),
					Description: aws.String("Allow control plane to communicate with nodes kubelet"),
				},
			},
		},
		{
			// Allow DNS TCP (53) within cluster
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(53),
			ToPort:     aws.Int32(53),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId:     aws.String(sgID),
					Description: aws.String("Allow DNS TCP communication within cluster"),
				},
			},
		},
		{
			// Allow DNS UDP (53) within cluster
			IpProtocol: aws.String("udp"),
			FromPort:   aws.Int32(53),
			ToPort:     aws.Int32(53),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId:     aws.String(sgID),
					Description: aws.String("Allow DNS UDP communication within cluster"),
				},
			},
		},
		{
			// Allow node-to-node communication on ephemeral ports (for CoreDNS, node port services, etc.)
			IpProtocol: aws.String("tcp"),
			FromPort:   aws.Int32(1025),
			ToPort:     aws.Int32(65535),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId:     aws.String(sgID),
					Description: aws.String("Allow node-to-node communication"),
				},
			},
		},
	}

	_, err := clients.EC2Client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: ingressRules,
	})
	if err != nil {
		// If the rule already exists, that's fine - treat as success (idempotent)
		if !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
			span.RecordError(err)
			return fmt.Errorf("AuthorizeSecurityGroupIngress failed: %w", err)
		}
		span.SetAttributes(attribute.Bool("ingress_rule_already_exists", true))
	}

	// Egress rules: Allow all outbound traffic (required for NAT gateway, pulling images, etc.)
	egressRules := []types.IpPermission{
		{
			IpProtocol: aws.String("-1"), // -1 means all protocols
			IpRanges: []types.IpRange{
				{
					CidrIp:      aws.String("0.0.0.0/0"),
					Description: aws.String("Allow all outbound traffic"),
				},
			},
		},
	}

	_, err = clients.EC2Client.AuthorizeSecurityGroupEgress(ctx, &ec2.AuthorizeSecurityGroupEgressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: egressRules,
	})
	if err != nil {
		// AWS automatically creates a default egress rule (allow all to 0.0.0.0/0)
		// If the rule already exists, that's fine - treat as success
		if !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
			span.RecordError(err)
			return fmt.Errorf("AuthorizeSecurityGroupEgress failed: %w", err)
		}
		span.SetAttributes(attribute.Bool("egress_rule_already_exists", true))
	}

	return nil
}

// createVPCEndpoints creates VPC endpoints required for private EKS clusters
// These endpoints allow nodes in private subnets to communicate with AWS services without internet access
func (p *Provider) createVPCEndpoints(ctx context.Context, clients *Clients, clusterName, vpcID string, privateSubnetIDs []string, securityGroupID, region string, userTags map[string]string) ([]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createVPCEndpoints")
	defer span.End()

	span.SetAttributes(
		attribute.String("vpc_id", vpcID),
		attribute.Int("subnet_count", len(privateSubnetIDs)),
	)

	// Define required VPC endpoints for private EKS clusters
	// Interface endpoints: Required for AWS API calls from nodes
	interfaceEndpoints := []string{
		fmt.Sprintf("com.amazonaws.%s.ec2", region),                  // EC2 API
		fmt.Sprintf("com.amazonaws.%s.ecr.api", region),              // ECR API (pull images)
		fmt.Sprintf("com.amazonaws.%s.ecr.dkr", region),              // ECR Docker registry
		fmt.Sprintf("com.amazonaws.%s.sts", region),                  // STS for IAM authentication
		fmt.Sprintf("com.amazonaws.%s.eks", region),                  // EKS API
		fmt.Sprintf("com.amazonaws.%s.eks-auth", region),             // EKS Pod Identity (2024+)
		fmt.Sprintf("com.amazonaws.%s.logs", region),                 // CloudWatch Logs
		fmt.Sprintf("com.amazonaws.%s.elasticloadbalancing", region), // ELB for load balancers
		fmt.Sprintf("com.amazonaws.%s.autoscaling", region),          // Auto Scaling
	}

	// Gateway endpoints: Required for S3 (pulling image layers)
	gatewayEndpoints := []string{
		fmt.Sprintf("com.amazonaws.%s.s3", region), // S3 for image layers
	}

	var endpointIDs []string

	// Create interface endpoints (one per service, accessible from all private subnets)
	interfaceEndpointIDs := []string{}
	for _, serviceName := range interfaceEndpoints {
		endpointID, err := p.createInterfaceEndpoint(ctx, clients, clusterName, vpcID, privateSubnetIDs, securityGroupID, serviceName, userTags)
		if err != nil {
			span.RecordError(err)
			// Continue creating other endpoints even if one fails
			// Log the error but don't fail the entire VPC creation
			span.SetAttributes(attribute.String(fmt.Sprintf("failed_endpoint.%s", serviceName), err.Error()))
			continue
		}
		interfaceEndpointIDs = append(interfaceEndpointIDs, endpointID)
		endpointIDs = append(endpointIDs, endpointID)
	}

	// Wait for interface endpoints to become available
	// This is critical: nodes cannot bootstrap until VPC endpoints are ready
	if len(interfaceEndpointIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Waiting for VPC endpoints to become available").
			WithResource("vpc-endpoint").
			WithAction("waiting"))

		err := p.waitForVPCEndpointsAvailable(ctx, clients, interfaceEndpointIDs)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed waiting for VPC endpoints to become available: %w", err)
		}

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC endpoints are available").
			WithResource("vpc-endpoint").
			WithAction("available"))
	}

	// Create gateway endpoints (S3) - these are immediately available
	for _, serviceName := range gatewayEndpoints {
		endpointID, err := p.createGatewayEndpoint(ctx, clients, clusterName, vpcID, serviceName, userTags)
		if err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String(fmt.Sprintf("failed_endpoint.%s", serviceName), err.Error()))
			continue
		}
		endpointIDs = append(endpointIDs, endpointID)
	}

	span.SetAttributes(attribute.Int("endpoints_created", len(endpointIDs)))

	return endpointIDs, nil
}

// waitForVPCEndpointsAvailable waits for VPC endpoints to become available
func (p *Provider) waitForVPCEndpointsAvailable(ctx context.Context, clients *Clients, endpointIDs []string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.waitForVPCEndpointsAvailable")
	defer span.End()

	span.SetAttributes(attribute.Int("endpoint_count", len(endpointIDs)))

	// Poll for VPC endpoint availability
	maxWait := 10 * time.Minute
	pollInterval := 15 * time.Second
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		// Check if all endpoints are available
		describeOutput, err := clients.EC2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
			VpcEndpointIds: endpointIDs,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to describe VPC endpoints: %w", err)
		}

		// Check if all are available
		allAvailable := true
		for _, ep := range describeOutput.VpcEndpoints {
			state := string(ep.State)
			if state != "available" {
				allAvailable = false
				span.SetAttributes(attribute.String(fmt.Sprintf("endpoint.%s.state", *ep.VpcEndpointId), state))
				break
			}
		}

		if allAvailable {
			span.SetAttributes(attribute.Bool("all_available", true))
			return nil
		}

		// Wait before polling again
		time.Sleep(pollInterval)
	}

	// Timeout reached
	err := fmt.Errorf("timed out waiting for VPC endpoints to become available after %v", maxWait)
	span.RecordError(err)
	return err
}

// createInterfaceEndpoint creates an interface VPC endpoint for a specific AWS service
func (p *Provider) createInterfaceEndpoint(ctx context.Context, clients *Clients, clusterName, vpcID string, subnetIDs []string, securityGroupID, serviceName string, userTags map[string]string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createInterfaceEndpoint")
	defer span.End()

	span.SetAttributes(
		attribute.String("service_name", serviceName),
		attribute.String("vpc_id", vpcID),
	)

	// Generate tags
	nicTags := GenerateBaseTags(ctx, clusterName, "vpc-endpoint")
	nicTags["Name"] = fmt.Sprintf("%s-vpce-%s", clusterName, serviceName[strings.LastIndex(serviceName, ".")+1:])
	allTags := MergeTags(ctx, nicTags, userTags)

	input := &ec2.CreateVpcEndpointInput{
		VpcId:             aws.String(vpcID),
		ServiceName:       aws.String(serviceName),
		VpcEndpointType:   types.VpcEndpointTypeInterface,
		SubnetIds:         subnetIDs,
		SecurityGroupIds:  []string{securityGroupID},
		PrivateDnsEnabled: aws.Bool(true), // Enable private DNS for seamless AWS service access
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeVpcEndpoint,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	}

	output, err := clients.EC2Client.CreateVpcEndpoint(ctx, input)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create interface endpoint for %s: %w", serviceName, err)
	}

	endpointID := aws.ToString(output.VpcEndpoint.VpcEndpointId)
	span.SetAttributes(attribute.String("endpoint_id", endpointID))

	return endpointID, nil
}

// createGatewayEndpoint creates a gateway VPC endpoint (for S3)
func (p *Provider) createGatewayEndpoint(ctx context.Context, clients *Clients, clusterName, vpcID, serviceName string, userTags map[string]string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.createGatewayEndpoint")
	defer span.End()

	span.SetAttributes(
		attribute.String("service_name", serviceName),
		attribute.String("vpc_id", vpcID),
	)

	// Generate tags
	nicTags := GenerateBaseTags(ctx, clusterName, "vpc-endpoint")
	nicTags["Name"] = fmt.Sprintf("%s-vpce-s3", clusterName)
	allTags := MergeTags(ctx, nicTags, userTags)

	// Gateway endpoints need route table IDs (not subnet IDs)
	// We'll discover route tables tagged with this cluster
	routeTablesInput := &ec2.DescribeRouteTablesInput{
		Filters: BuildTagFilter(clusterName, "route-table"),
	}

	routeTablesOutput, err := clients.EC2Client.DescribeRouteTables(ctx, routeTablesInput)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to describe route tables: %w", err)
	}

	var routeTableIDs []string
	for _, rt := range routeTablesOutput.RouteTables {
		routeTableIDs = append(routeTableIDs, aws.ToString(rt.RouteTableId))
	}

	if len(routeTableIDs) == 0 {
		err := fmt.Errorf("no route tables found for cluster %s", clusterName)
		span.RecordError(err)
		return "", err
	}

	input := &ec2.CreateVpcEndpointInput{
		VpcId:           aws.String(vpcID),
		ServiceName:     aws.String(serviceName),
		VpcEndpointType: types.VpcEndpointTypeGateway,
		RouteTableIds:   routeTableIDs,
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeVpcEndpoint,
				Tags:         ConvertToEC2Tags(allTags),
			},
		},
	}

	output, err := clients.EC2Client.CreateVpcEndpoint(ctx, input)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("failed to create gateway endpoint for %s: %w", serviceName, err)
	}

	endpointID := aws.ToString(output.VpcEndpoint.VpcEndpointId)
	span.SetAttributes(attribute.String("endpoint_id", endpointID))

	return endpointID, nil
}

// addSecurityGroupToVPCEndpoints adds a security group to existing VPC endpoints
// This is used to add the EKS-managed cluster security group after cluster creation
func (p *Provider) addSecurityGroupToVPCEndpoints(ctx context.Context, clients *Clients, endpointIDs []string, securityGroupID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.addSecurityGroupToVPCEndpoints")
	defer span.End()

	span.SetAttributes(
		attribute.Int("endpoint_count", len(endpointIDs)),
		attribute.String("security_group_id", securityGroupID),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Adding EKS-managed security group to VPC endpoints").
		WithResource("vpc-endpoint").
		WithAction("updating").
		WithMetadata("security_group_id", securityGroupID).
		WithMetadata("endpoint_count", len(endpointIDs)))

	// Filter to only interface endpoints (gateway endpoints don't use security groups)
	interfaceEndpoints := []string{}
	describeOutput, err := clients.EC2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		VpcEndpointIds: endpointIDs,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe VPC endpoints: %w", err)
	}

	for _, ep := range describeOutput.VpcEndpoints {
		if ep.VpcEndpointType == types.VpcEndpointTypeInterface {
			interfaceEndpoints = append(interfaceEndpoints, aws.ToString(ep.VpcEndpointId))
		}
	}

	// Add security group to each interface endpoint
	for _, endpointID := range interfaceEndpoints {
		input := &ec2.ModifyVpcEndpointInput{
			VpcEndpointId:       aws.String(endpointID),
			AddSecurityGroupIds: []string{securityGroupID},
		}

		_, err := clients.EC2Client.ModifyVpcEndpoint(ctx, input)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to add security group to VPC endpoint %s: %w", endpointID, err)
		}

		span.SetAttributes(attribute.String(fmt.Sprintf("endpoint.%s.updated", endpointID), "true"))
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "EKS-managed security group added to VPC endpoints").
		WithResource("vpc-endpoint").
		WithAction("updated").
		WithMetadata("endpoint_count", len(interfaceEndpoints)))

	return nil
}
