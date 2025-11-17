package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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

	clusterName := cfg.ProjectName
	awsCfg := cfg.AmazonWebServices

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

	// Step 2: Enable DNS hostnames and DNS support
	if err := p.enableVPCDNS(ctx, clients, vpcState.VPCID); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to enable VPC DNS: %w", err)
	}

	// Step 3: Create Internet Gateway
	igwID, err := p.createInternetGateway(ctx, clients, clusterName, vpcState.VPCID, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create internet gateway: %w", err)
	}
	vpcState.InternetGatewayID = igwID

	// Step 4: Create public and private subnets
	publicSubnets, err := p.createSubnets(ctx, clients, clusterName, vpcState.VPCID, vpcCIDR, azs, true, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create public subnets: %w", err)
	}
	vpcState.PublicSubnetIDs = publicSubnets

	privateSubnets, err := p.createSubnets(ctx, clients, clusterName, vpcState.VPCID, vpcCIDR, azs, false, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create private subnets: %w", err)
	}
	vpcState.PrivateSubnetIDs = privateSubnets

	// Step 5: Create NAT Gateways (one per public subnet for HA)
	natGatewayIDs, err := p.createNATGateways(ctx, clients, clusterName, publicSubnets, azs, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create NAT gateways: %w", err)
	}
	vpcState.NATGatewayIDs = natGatewayIDs

	// Step 6: Create route tables and routes
	publicRouteTableID, err := p.createPublicRouteTable(ctx, clients, clusterName, vpcState.VPCID, igwID, publicSubnets, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create public route table: %w", err)
	}
	vpcState.PublicRouteTableID = publicRouteTableID

	privateRouteTableIDs, err := p.createPrivateRouteTables(ctx, clients, clusterName, vpcState.VPCID, natGatewayIDs, privateSubnets, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create private route tables: %w", err)
	}
	vpcState.PrivateRouteTableIDs = privateRouteTableIDs

	// Step 7: Create security group for cluster
	sgID, err := p.createClusterSecurityGroup(ctx, clients, clusterName, vpcState.VPCID, awsCfg.Tags)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to create security group: %w", err)
	}
	vpcState.SecurityGroupIDs = []string{sgID}

	// Add user tags
	nicTags := GenerateBaseTags(ctx, clusterName, ResourceTypeVPC)
	vpcState.Tags = MergeTags(ctx, nicTags, awsCfg.Tags)

	span.SetAttributes(
		attribute.String("vpc_id", vpcState.VPCID),
		attribute.Int("public_subnets", len(vpcState.PublicSubnetIDs)),
		attribute.Int("private_subnets", len(vpcState.PrivateSubnetIDs)),
		attribute.Int("nat_gateways", len(vpcState.NATGatewayIDs)),
	)

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
func (p *Provider) getAvailabilityZones(ctx context.Context, clients *Clients, awsCfg *config.AWSConfig) ([]string, error) {
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
		}, 5*60) // 5 minutes timeout (in seconds)
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
	return sgID, nil
}
