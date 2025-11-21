package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

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

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Checking VPC").
		WithResource("vpc").
		WithAction("discovering"))

	// Discover the VPC first
	vpc, err := p.DiscoverVPC(ctx, clients, clusterName)
	if err != nil || vpc == nil {
		// VPC doesn't exist - but we should still clean up orphaned EIPs
		span.SetAttributes(attribute.Bool("vpc_exists", false))
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "VPC not found, checking for orphaned resources").
			WithResource("vpc"))

		// Clean up any orphaned EIPs even if VPC is gone
		return p.cleanupOrphanedEIPs(ctx, clients, clusterName)
	}

	vpcID := vpc.VPCID
	span.SetAttributes(
		attribute.Bool("vpc_exists", true),
		attribute.String("vpc_id", vpcID),
	)

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Deleting VPC and networking resources").
		WithResource("vpc").
		WithAction("deleting").
		WithMetadata("vpc_id", vpcID))

	// Delete resources in correct order to respect dependencies:
	// 1. VPC Endpoints (must be deleted before subnets/security groups)
	// 2. NAT Gateways (release Elastic IPs)
	// 3. Internet Gateway
	// 4. Route table associations
	// 5. Route tables (non-main)
	// 6. Subnets
	// 7. Security groups (non-default)
	// 8. VPC

	// 1. Delete VPC Endpoints
	if err := p.deleteVPCEndpoints(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete VPC endpoints: %w", err)
	}

	// 2. Delete NAT Gateways
	if err := p.deleteNATGateways(ctx, clients, vpcID, clusterName); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete NAT gateways: %w", err)
	}

	// 3. Delete Internet Gateway
	if err := p.deleteInternetGateway(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete internet gateway: %w", err)
	}

	// 4. Delete route table associations (handled in deleteRouteTables)
	// 5. Delete route tables
	if err := p.deleteRouteTables(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete route tables: %w", err)
	}

	// 6. Delete subnets
	if err := p.deleteSubnets(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete subnets: %w", err)
	}

	// 7. Delete security groups (except default)
	if err := p.deleteSecurityGroups(ctx, clients, vpcID); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete security groups: %w", err)
	}

	// 8. Delete VPC
	_, err = clients.EC2Client.DeleteVpc(ctx, &ec2.DeleteVpcInput{
		VpcId: &vpcID,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to delete VPC %s: %w", vpcID, err)
	}

	span.SetAttributes(attribute.Bool("deletion_complete", true))

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC and networking resources deleted").
		WithResource("vpc").
		WithAction("deleted").
		WithMetadata("vpc_id", vpcID))

	return nil
}

// cleanupOrphanedEIPs finds and releases any EIPs tagged with this cluster that have no associations
// This is useful for cleaning up EIPs when the VPC/NAT Gateways are already deleted
func (p *Provider) cleanupOrphanedEIPs(ctx context.Context, clients *Clients, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupOrphanedEIPs")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	// Query all EIPs for this cluster
	allClusterEIPs, err := clients.EC2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("domain"),
				Values: []string{"vpc"},
			},
			{
				Name:   strPtr("tag:nic.nebari.dev/cluster-name"),
				Values: []string{clusterName},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe cluster EIPs: %w", err)
	}

	span.SetAttributes(attribute.Int("total_cluster_eips_found", len(allClusterEIPs.Addresses)))

	if len(allClusterEIPs.Addresses) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "No orphaned EIPs found").
			WithResource("eip"))
		return nil
	}

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Releasing orphaned Elastic IPs").
		WithResource("eip").
		WithAction("releasing").
		WithMetadata("count", len(allClusterEIPs.Addresses)))

	// Release all unassociated EIPs
	eipsReleased := 0
	eipsSkipped := 0

	for _, addr := range allClusterEIPs.Addresses {
		// Skip EIPs that are still associated with something
		if addr.AssociationId != nil {
			eipsSkipped++
			span.SetAttributes(attribute.String(fmt.Sprintf("eip_skipped.%s", *addr.AllocationId), "still_associated"))
			continue
		}

		// Release EIP
		if addr.AllocationId != nil {
			_, err := clients.EC2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
				AllocationId: addr.AllocationId,
			})
			if err != nil {
				span.RecordError(err)
				span.SetAttributes(attribute.String(fmt.Sprintf("eip_release_error.%s", *addr.AllocationId), err.Error()))
				// Continue trying to release other EIPs
			} else {
				eipsReleased++
				span.SetAttributes(attribute.String(fmt.Sprintf("eip_released.%s", *addr.AllocationId), *addr.AllocationId))
			}
		}
	}

	span.SetAttributes(
		attribute.Int("eips_released", eipsReleased),
		attribute.Int("eips_skipped", eipsSkipped),
	)

	if eipsReleased > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Orphaned Elastic IPs released").
			WithResource("eip").
			WithAction("released").
			WithMetadata("count", eipsReleased))
	}

	return nil
}

// cleanupOrphanedEIPsWithCount is like cleanupOrphanedEIPs but returns the count of EIPs released
func (p *Provider) cleanupOrphanedEIPsWithCount(ctx context.Context, clients *Clients, clusterName string) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupOrphanedEIPsWithCount")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	// Query all EIPs for this cluster
	allClusterEIPs, err := clients.EC2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("domain"),
				Values: []string{"vpc"},
			},
			{
				Name:   strPtr("tag:nic.nebari.dev/cluster-name"),
				Values: []string{clusterName},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to describe cluster EIPs: %w", err)
	}

	span.SetAttributes(attribute.Int("total_cluster_eips_found", len(allClusterEIPs.Addresses)))

	if len(allClusterEIPs.Addresses) == 0 {
		return 0, nil
	}

	// Release all unassociated EIPs
	eipsReleased := 0

	for _, addr := range allClusterEIPs.Addresses {
		// Skip EIPs that are still associated with something
		if addr.AssociationId != nil {
			continue
		}

		// Release EIP
		if addr.AllocationId != nil {
			_, err := clients.EC2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
				AllocationId: addr.AllocationId,
			})
			if err != nil {
				span.RecordError(err)
				// Continue trying to release other EIPs
			} else {
				eipsReleased++
			}
		}
	}

	if eipsReleased > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Orphaned Elastic IPs released").
			WithResource("eip").
			WithAction("released").
			WithMetadata("count", eipsReleased))
	}

	span.SetAttributes(attribute.Int("eips_released", eipsReleased))

	return eipsReleased, nil
}

// cleanupOrphanedNATGateways finds NAT Gateways tagged with this cluster and ensures they are deleted
// Returns the count of NAT Gateways that were found and needed cleanup
func (p *Provider) cleanupOrphanedNATGateways(ctx context.Context, clients *Clients, clusterName string) (int, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.cleanupOrphanedNATGateways")
	defer span.End()

	span.SetAttributes(attribute.String("cluster_name", clusterName))

	// Query NAT Gateways by cluster tag
	output, err := clients.EC2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []types.Filter{
			{
				Name:   strPtr("tag:nic.nebari.dev/cluster-name"),
				Values: []string{clusterName},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return 0, fmt.Errorf("failed to describe NAT gateways: %w", err)
	}

	span.SetAttributes(attribute.Int("nat_gateways_found", len(output.NatGateways)))

	// Count NAT Gateways that aren't fully deleted yet
	pendingCount := 0
	for _, ng := range output.NatGateways {
		state := string(ng.State)
		if state != "deleted" {
			pendingCount++

			// If still in available/pending state, initiate deletion
			if state == "available" || state == "pending" {
				natGatewayID := *ng.NatGatewayId
				_, err := clients.EC2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
					NatGatewayId: &natGatewayID,
				})
				if err != nil && !strings.Contains(err.Error(), "NatGatewayNotFound") {
					span.RecordError(err)
				}
			}
		}
	}

	span.SetAttributes(attribute.Int("nat_gateways_pending", pendingCount))

	return pendingCount, nil
}

// deleteNATGateways deletes all NAT gateways in the VPC and releases associated EIPs
func (p *Provider) deleteNATGateways(ctx context.Context, clients *Clients, vpcID string, clusterName string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteNATGateways")
	defer span.End()

	span.SetAttributes(
		attribute.String("vpc_id", vpcID),
		attribute.String("cluster_name", clusterName),
	)

	// Step 0: Query ALL EIPs for this cluster first (including orphaned ones)
	// This must be done BEFORE deleting NAT Gateways because AWS stops returning
	// EIP allocation IDs once a NAT Gateway transitions to "deleted" state
	allClusterEIPs, err := clients.EC2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("domain"),
				Values: []string{"vpc"},
			},
			{
				Name:   strPtr("tag:nic.nebari.dev/cluster-name"),
				Values: []string{clusterName},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe cluster EIPs: %w", err)
	}

	span.SetAttributes(attribute.Int("total_cluster_eips_found", len(allClusterEIPs.Addresses)))

	// Describe NAT gateways in this VPC
	// Note: Don't filter by state - we want to find ALL NAT Gateways in this VPC
	// including those in "available", "pending", "failed", or even "deleting" states
	output, err := clients.EC2Client.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{
		Filter: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe NAT gateways: %w", err)
	}

	span.SetAttributes(attribute.Int("nat_gateways_found", len(output.NatGateways)))

	if len(output.NatGateways) == 0 && len(allClusterEIPs.Addresses) == 0 {
		span.SetAttributes(attribute.Int("nat_gateways_deleted", 0))
		return nil
	}

	// Step 1: Initiate NAT Gateway deletions
	natGatewayIDs := make([]string, 0, len(output.NatGateways))

	for _, ng := range output.NatGateways {
		natGatewayID := *ng.NatGatewayId
		state := string(ng.State)

		span.SetAttributes(attribute.String(fmt.Sprintf("nat_gateway.%s.state", natGatewayID), state))

		// Skip deletion if NAT Gateway is already deleted
		if state == "deleted" {
			continue
		}

		// Skip deletion if NAT Gateway is already being deleted, but track it for waiting
		if state == "deleting" {
			natGatewayIDs = append(natGatewayIDs, natGatewayID)
			continue
		}

		natGatewayIDs = append(natGatewayIDs, natGatewayID)

		// Initiate deletion
		_, err := clients.EC2Client.DeleteNatGateway(ctx, &ec2.DeleteNatGatewayInput{
			NatGatewayId: &natGatewayID,
		})
		if err != nil {
			// If deletion fails because it's already being deleted, that's OK
			if !strings.Contains(err.Error(), "NatGatewayNotFound") {
				span.RecordError(err)
				return fmt.Errorf("failed to delete NAT gateway %s: %w", natGatewayID, err)
			}
		}
	}

	span.SetAttributes(attribute.Int("nat_gateways_to_delete", len(natGatewayIDs)))

	// Step 2: Wait for all NAT Gateways to be deleted
	for _, natGatewayID := range natGatewayIDs {
		waiter := ec2.NewNatGatewayDeletedWaiter(clients.EC2Client)
		err := waiter.Wait(ctx, &ec2.DescribeNatGatewaysInput{
			NatGatewayIds: []string{natGatewayID},
		}, 10*time.Minute) // NAT Gateway deletion can take up to 10 minutes
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed waiting for NAT gateway %s deletion: %w", natGatewayID, err)
		}
	}

	// Step 3: Release ALL cluster EIPs (both from NAT Gateways and orphaned ones)
	// We release all EIPs we found in Step 0, not just ones from NAT Gateways
	// This ensures we clean up EIPs even if their NAT Gateway is already deleted
	eipsReleased := 0
	eipsSkipped := 0

	for _, addr := range allClusterEIPs.Addresses {
		// Skip EIPs that are still associated with something
		if addr.AssociationId != nil {
			eipsSkipped++
			span.SetAttributes(attribute.String(fmt.Sprintf("eip_skipped.%s", *addr.AllocationId), "still_associated"))
			continue
		}

		// Release EIP
		if addr.AllocationId != nil {
			_, err := clients.EC2Client.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{
				AllocationId: addr.AllocationId,
			})
			if err != nil {
				span.RecordError(err)
				span.SetAttributes(attribute.String(fmt.Sprintf("eip_release_error.%s", *addr.AllocationId), err.Error()))
				// Continue trying to release other EIPs
			} else {
				eipsReleased++
				span.SetAttributes(attribute.String(fmt.Sprintf("eip_released.%s", *addr.AllocationId), *addr.AllocationId))
			}
		}
	}

	span.SetAttributes(
		attribute.Int("nat_gateways_deleted", len(natGatewayIDs)),
		attribute.Int("total_eips_released", eipsReleased),
		attribute.Int("eips_skipped", eipsSkipped),
	)

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

// deleteVPCEndpoints deletes all VPC endpoints in the VPC
func (p *Provider) deleteVPCEndpoints(ctx context.Context, clients *Clients, vpcID string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.deleteVPCEndpoints")
	defer span.End()

	span.SetAttributes(attribute.String("vpc_id", vpcID))

	// Describe VPC endpoints in this VPC
	output, err := clients.EC2Client.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{
		Filters: []types.Filter{
			{
				Name:   strPtr("vpc-id"),
				Values: []string{vpcID},
			},
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to describe VPC endpoints: %w", err)
	}

	if len(output.VpcEndpoints) == 0 {
		span.SetAttributes(attribute.Int("vpc_endpoints_deleted", 0))
		return nil
	}

	span.SetAttributes(attribute.Int("vpc_endpoints_to_delete", len(output.VpcEndpoints)))

	status.Send(ctx, status.NewUpdate(status.LevelProgress, "Deleting VPC endpoints").
		WithResource("vpc-endpoint").
		WithAction("deleting").
		WithMetadata("count", len(output.VpcEndpoints)))

	// Collect endpoint IDs to delete
	endpointIDs := make([]string, 0, len(output.VpcEndpoints))
	for _, ep := range output.VpcEndpoints {
		if ep.VpcEndpointId != nil {
			endpointIDs = append(endpointIDs, *ep.VpcEndpointId)
		}
	}

	// Delete all VPC endpoints in a single call
	if len(endpointIDs) > 0 {
		_, err := clients.EC2Client.DeleteVpcEndpoints(ctx, &ec2.DeleteVpcEndpointsInput{
			VpcEndpointIds: endpointIDs,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to delete VPC endpoints: %w", err)
		}
	}

	span.SetAttributes(attribute.Int("vpc_endpoints_deleted", len(endpointIDs)))

	return nil
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}
