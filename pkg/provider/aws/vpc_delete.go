package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// deleteVPC deletes the VPC and all associated resources in the correct order
func (p *Provider) deleteVPC(ctx context.Context, clients *Clients, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteVPC")
	defer span.End()

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
	)

	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Checking VPC").
		WithResource("vpc").
		WithAction("discovering"))

	// Discover the VPC first
	vpc, err := p.DiscoverVPC(ctx, clients, clusterName)
	if err != nil {
		// VPC doesn't exist - nothing to delete
		span.SetAttributes(attribute.Bool("vpc_exists", false))
		status.Send(ctx, status.NewStatusUpdate(status.LevelInfo, "VPC not found").
			WithResource("vpc"))
		return nil
	}

	if vpc == nil {
		// VPC doesn't exist - nothing to delete
		span.SetAttributes(attribute.Bool("vpc_exists", false))
		status.Send(ctx, status.NewStatusUpdate(status.LevelInfo, "VPC not found").
			WithResource("vpc"))
		return nil
	}

	vpcID := vpc.VPCID
	span.SetAttributes(
		attribute.Bool("vpc_exists", true),
		attribute.String("vpc_id", vpcID),
	)

	status.Send(ctx, status.NewStatusUpdate(status.LevelProgress, "Deleting VPC and networking resources").
		WithResource("vpc").
		WithAction("deleting").
		WithMetadata("vpc_id", vpcID))

	// Delete resources in correct order to respect dependencies:
	// 1. NAT Gateways (release Elastic IPs)
	// 2. Internet Gateway
	// 3. Route table associations
	// 4. Route tables (non-main)
	// 5. Subnets
	// 6. Security groups (non-default)
	// 7. VPC

	// 1. Delete NAT Gateways
	if err := p.deleteNATGateways(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete NAT gateways: %w", err)
	}

	// 2. Delete Internet Gateway
	if err := p.deleteInternetGateway(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete internet gateway: %w", err)
	}

	// 3. Delete route table associations (handled in deleteRouteTables)
	// 4. Delete route tables
	if err := p.deleteRouteTables(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete route tables: %w", err)
	}

	// 5. Delete subnets
	if err := p.deleteSubnets(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete subnets: %w", err)
	}

	// 6. Delete security groups (except default)
	if err := p.deleteSecurityGroups(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete security groups: %w", err)
	}

	// 7. Delete VPC
	_, err = clients.EC2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: &vpcID,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete VPC %s: %w", vpcID, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	status.Send(ctx, status.NewStatusUpdate(status.LevelSuccess, "VPC and networking resources deleted").
		WithResource("vpc").
		WithAction("deleted").
		WithMetadata("vpc_id", vpcID))

	return nil
}

// deleteNATGateways deletes all NAT gateways in the VPC
func (p *Provider) deleteNATGateways(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteNATGateways")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe NAT gateways in this VPC
	output, err := clients.EC2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   strPtr("state"),
				Values: []string{"available", "pending"},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe NAT gateways: %w", err)
	}

	if len(output.NatGateways) == 0 {
		span.SetAttributes(attribute.Int("nat_gateways_deleted", 0))
		return nil
	}

	span.SetAttributes(attribute.Int("nat_gateways_to_delete", len(output.NatGateways)))

	// Delete each NAT gateway
	for _, ng := range output.NatGateways {
		natGatewayID := *ng.NatGatewayId

		_, err := clients.EC2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
			NatGatewayId: &natGatewayID,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete NAT gateway %s: %w", natGatewayID, err)
		}

		// Release associated Elastic IPs after NAT gateway deletion
		for _, addr := range ng.NatGatewayAddresses {
			if addr.AllocationId != nil {
				_, err := clients.EC2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
					AllocationId: addr.AllocationId,
				})
				if err != nil {
					// Log but don't fail - EIP might already be released
					span.RecordError(err)
				}
			}
		}
	}

	span.SetAttributes(attribute.Int("nat_gateways_deleted", len(output.NatGateways)))

	return nil
}

// deleteInternetGateway deletes the internet gateway attached to the VPC
func (p *Provider) deleteInternetGateway(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteInternetGateway")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe internet gateways attached to this VPC
	output, err := clients.EC2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("attachment.vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe internet gateways: %w", err)
	}

	if len(output.InternetGateways) == 0 {
		span.SetAttributes(attribute.Bool("internet_gateway_exists", false))
		return nil
	}

	// There should only be one IGW per VPC
	igw := output.InternetGateways[0]
	igwID := *igw.InternetGatewayId

	span.SetAttributes(
		attribute.Bool("internet_gateway_exists", true),
		attribute.String("internet_gateway_id", igwID),
	)

	// Detach from VPC first
	_, err = clients.EC2Client.DetachInternetGateway(ctx, &ec2.DetachInternetGatewayInput{
		InternetGatewayId: &igwID,
		VpcId:             &vpcID,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to detach internet gateway %s: %w", igwID, err)
	}

	// Delete internet gateway
	_, err = clients.EC2Client.DeleteInternetGateway(ctx, &ec2.DeleteInternetGatewayInput{
		InternetGatewayId: &igwID,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete internet gateway %s: %w", igwID, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	return nil
}

// deleteRouteTables deletes all non-main route tables in the VPC
func (p *Provider) deleteRouteTables(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteRouteTables")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe route tables in this VPC
	output, err := clients.EC2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe route tables: %w", err)
	}

	deletedCount := 0

	// Delete each non-main route table
	for _, rt := range output.RouteTables {
		// Skip main route table (it's deleted automatically with VPC)
		isMain := false
		for _, assoc := range rt.Associations {
			if assoc.Main != nil && *assoc.Main {
				isMain = true
				break
			}
		}

		if isMain {
			continue
		}

		routeTableID := *rt.RouteTableId

		// Disassociate from subnets first
		for _, assoc := range rt.Associations {
			if assoc.RouteTableAssociationId != nil {
				_, err := clients.EC2Client.DisassociateRouteTable(ctx, &ec2.DisassociateRouteTableInput{
					AssociationId: assoc.RouteTableAssociationId,
				})
				if err != nil {
					span.RecordError(err)
					// Continue trying to delete other associations
				}
			}
		}

		// Delete route table
		_, err := clients.EC2Client.DeleteRouteTable(ctx, &ec2.DeleteRouteTableInput{
			RouteTableId: &routeTableID,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete route table %s: %w", routeTableID, err)
		}

		deletedCount++
	}

	span.SetAttributes(attribute.Int("route_tables_deleted", deletedCount))

	return nil
}

// deleteSubnets deletes all subnets in the VPC
func (p *Provider) deleteSubnets(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteSubnets")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe subnets in this VPC
	output, err := clients.EC2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe subnets: %w", err)
	}

	if len(output.Subnets) == 0 {
		span.SetAttributes(attribute.Int("subnets_deleted", 0))
		return nil
	}

	span.SetAttributes(attribute.Int("subnets_to_delete", len(output.Subnets)))

	// Delete each subnet
	for _, subnet := range output.Subnets {
		subnetID := *subnet.SubnetId

		_, err := clients.EC2Client.DeleteSubnet(ctx, &ec2.DeleteSubnetInput{
			SubnetId: &subnetID,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete subnet %s: %w", subnetID, err)
		}
	}

	span.SetAttributes(attribute.Int("subnets_deleted", len(output.Subnets)))

	return nil
}

// deleteSecurityGroups deletes all non-default security groups in the VPC
func (p *Provider) deleteSecurityGroups(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteSecurityGroups")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe security groups in this VPC
	output, err := clients.EC2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe security groups: %w", err)
	}

	deletedCount := 0

	// Delete each non-default security group
	for _, sg := range output.SecurityGroups {
		// Skip default security group (deleted automatically with VPC)
		if sg.GroupName != nil && *sg.GroupName == "default" {
			continue
		}

		securityGroupID := *sg.GroupId

		_, err := clients.EC2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: &securityGroupID,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete security group %s: %w", securityGroupID, err)
		}

		deletedCount++
	}

	span.SetAttributes(attribute.Int("security_groups_deleted", deletedCount))

	return nil
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}
