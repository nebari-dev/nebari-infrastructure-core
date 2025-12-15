package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// reconcileVPC compares desired VPC configuration against actual state and reconciles differences
func (p *Provider) reconcileVPC(ctx context.Context, clients *Clients, cfg *config.NebariConfig, actual *VPCState) (*VPCState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.reconcileVPC")
	defer span.End()

	awsCfg, err := extractAWSConfig(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	clusterName := cfg.ProjectName

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Bool("vpc_exists", actual != nil),
	)

	// Case 1: VPC doesn't exist → Create full VPC
	if actual == nil {
		span.SetAttributes(attribute.String("action", "create"))
		vpcState, err := p.createVPC(ctx, clients, cfg)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create VPC: %w", err)
		}
		return vpcState, nil
	}

	// Case 2: VPC exists → Validate immutable fields
	desiredCIDR := DefaultVPCCIDR
	if awsCfg.VPCCIDRBlock != "" {
		desiredCIDR = awsCfg.VPCCIDRBlock
	}

	if actual.CIDR != desiredCIDR {
		// VPC CIDR is immutable - cannot be changed without recreation
		err := fmt.Errorf(
			"VPC CIDR mismatch detected (actual: %s, desired: %s). "+
				"VPC CIDR is immutable and requires manual intervention to change. "+
				"You must destroy the existing VPC and recreate it, or update your configuration to match the actual CIDR",
			actual.CIDR,
			desiredCIDR,
		)
		span.RecordError(err)
		return nil, err
	}

	// Validate availability zones are immutable (if specified in config)
	// AZs determine subnet placement which cannot be changed without recreation
	if len(awsCfg.AvailabilityZones) > 0 && len(actual.AvailabilityZones) > 0 {
		if !stringSlicesEqualVPC(awsCfg.AvailabilityZones, actual.AvailabilityZones) {
			err := fmt.Errorf(
				"VPC availability zones mismatch detected (actual: %v, desired: %v). "+
					"Availability zones are immutable and require manual intervention to change. "+
					"You must destroy the existing VPC and recreate it, or update your configuration to match the actual AZs",
				actual.AvailabilityZones,
				awsCfg.AvailabilityZones,
			)
			span.RecordError(err)
			return nil, err
		}
	}

	// Case 3: VPC exists and immutable fields match → Reconcile missing networking components
	// Only reconcile if we have actual clients (not in test mode with nil clients)
	if clients == nil {
		// Test mode or VPC exists with all immutable fields matching - no reconciliation needed
		span.SetAttributes(attribute.String("action", "none"))
		return actual, nil
	}

	updated := false

	// Get availability zones for creating missing resources
	azs := actual.AvailabilityZones
	if len(azs) == 0 {
		azs, err = p.getAvailabilityZones(ctx, clients, awsCfg)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to get availability zones: %w", err)
		}
	}

	// Reconcile Internet Gateway
	if actual.InternetGatewayID == "" {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing internet gateway").
			WithResource("internet-gateway").
			WithAction("creating"))

		igwID, err := p.createInternetGateway(ctx, clients, clusterName, actual.VPCID, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create internet gateway: %w", err)
		}
		actual.InternetGatewayID = igwID
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Internet gateway created").
			WithResource("internet-gateway").
			WithAction("created").
			WithMetadata("igw_id", igwID))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Internet gateway already exists").
			WithResource("internet-gateway").
			WithAction("skipped").
			WithMetadata("igw_id", actual.InternetGatewayID))
	}

	// Reconcile Subnets
	if len(actual.PublicSubnetIDs) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing public subnets").
			WithResource("subnet").
			WithAction("creating"))

		publicSubnets, err := p.createSubnets(ctx, clients, clusterName, actual.VPCID, actual.CIDR, azs, true, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create public subnets: %w", err)
		}
		actual.PublicSubnetIDs = publicSubnets
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Public subnets created").
			WithResource("subnet").
			WithAction("created").
			WithMetadata("count", len(publicSubnets)))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Public subnets already exist").
			WithResource("subnet").
			WithAction("skipped").
			WithMetadata("count", len(actual.PublicSubnetIDs)))
	}

	if len(actual.PrivateSubnetIDs) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing private subnets").
			WithResource("subnet").
			WithAction("creating"))

		privateSubnets, err := p.createSubnets(ctx, clients, clusterName, actual.VPCID, actual.CIDR, azs, false, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create private subnets: %w", err)
		}
		actual.PrivateSubnetIDs = privateSubnets
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Private subnets created").
			WithResource("subnet").
			WithAction("created").
			WithMetadata("count", len(privateSubnets)))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Private subnets already exist").
			WithResource("subnet").
			WithAction("skipped").
			WithMetadata("count", len(actual.PrivateSubnetIDs)))
	}

	// Reconcile NAT Gateways (require public subnets)
	if len(actual.NATGatewayIDs) == 0 && len(actual.PublicSubnetIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing NAT gateways").
			WithResource("nat-gateway").
			WithAction("creating"))

		natGatewayIDs, err := p.createNATGateways(ctx, clients, clusterName, actual.PublicSubnetIDs, azs, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create NAT gateways: %w", err)
		}
		actual.NATGatewayIDs = natGatewayIDs
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "NAT gateways created").
			WithResource("nat-gateway").
			WithAction("created").
			WithMetadata("count", len(natGatewayIDs)))
	} else if len(actual.NATGatewayIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "NAT gateways already exist").
			WithResource("nat-gateway").
			WithAction("skipped").
			WithMetadata("count", len(actual.NATGatewayIDs)))
	}

	// Reconcile Route Tables (require IGW and NAT gateways)
	if actual.PublicRouteTableID == "" && actual.InternetGatewayID != "" && len(actual.PublicSubnetIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing public route table").
			WithResource("route-table").
			WithAction("creating"))

		publicRouteTableID, err := p.createPublicRouteTable(ctx, clients, clusterName, actual.VPCID, actual.InternetGatewayID, actual.PublicSubnetIDs, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create public route table: %w", err)
		}
		actual.PublicRouteTableID = publicRouteTableID
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Public route table created with IGW route").
			WithResource("route-table").
			WithAction("created").
			WithMetadata("route_table_id", publicRouteTableID))
	} else if actual.PublicRouteTableID != "" {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Public route table already exists").
			WithResource("route-table").
			WithAction("skipped").
			WithMetadata("route_table_id", actual.PublicRouteTableID))
	}

	if len(actual.PrivateRouteTableIDs) == 0 && len(actual.NATGatewayIDs) > 0 && len(actual.PrivateSubnetIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing private route tables").
			WithResource("route-table").
			WithAction("creating"))

		privateRouteTableIDs, err := p.createPrivateRouteTables(ctx, clients, clusterName, actual.VPCID, actual.NATGatewayIDs, actual.PrivateSubnetIDs, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create private route tables: %w", err)
		}
		actual.PrivateRouteTableIDs = privateRouteTableIDs
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Private route tables created with NAT routes").
			WithResource("route-table").
			WithAction("created").
			WithMetadata("count", len(privateRouteTableIDs)))
	} else if len(actual.PrivateRouteTableIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Private route tables already exist").
			WithResource("route-table").
			WithAction("skipped").
			WithMetadata("count", len(actual.PrivateRouteTableIDs)))
	}

	// Reconcile Security Groups
	if len(actual.SecurityGroupIDs) == 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing security group").
			WithResource("security-group").
			WithAction("creating"))

		sgID, err := p.createClusterSecurityGroup(ctx, clients, clusterName, actual.VPCID, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create security group: %w", err)
		}
		actual.SecurityGroupIDs = []string{sgID}
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Cluster security group created").
			WithResource("security-group").
			WithAction("created").
			WithMetadata("security_group_id", sgID))
	} else {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Security group already exists").
			WithResource("security-group").
			WithAction("skipped").
			WithMetadata("security_group_id", actual.SecurityGroupIDs[0]))
	}

	// Reconcile VPC Endpoints
	if len(actual.VPCEndpointIDs) == 0 && len(actual.PrivateSubnetIDs) > 0 && len(actual.SecurityGroupIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating missing VPC endpoints").
			WithResource("vpc-endpoint").
			WithAction("creating"))

		vpcEndpointIDs, err := p.createVPCEndpoints(ctx, clients, clusterName, actual.VPCID, actual.PrivateSubnetIDs, actual.SecurityGroupIDs[0], awsCfg.Region, awsCfg.Tags)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to create VPC endpoints: %w", err)
		}
		actual.VPCEndpointIDs = vpcEndpointIDs
		updated = true

		status.Send(ctx, status.NewUpdate(status.LevelSuccess, "VPC endpoints created and available").
			WithResource("vpc-endpoint").
			WithAction("created").
			WithMetadata("count", len(vpcEndpointIDs)))
	} else if len(actual.VPCEndpointIDs) > 0 {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "VPC endpoints already exist").
			WithResource("vpc-endpoint").
			WithAction("skipped").
			WithMetadata("count", len(actual.VPCEndpointIDs)))
	}

	if updated {
		span.SetAttributes(attribute.String("action", "reconciled"))
	} else {
		span.SetAttributes(attribute.String("action", "none"))
	}

	return actual, nil
}

// stringSlicesEqualVPC compares two string slices for equality (order-independent)
func stringSlicesEqualVPC(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison (order-independent)
	aMap := make(map[string]bool, len(a))
	for _, v := range a {
		aMap[v] = true
	}

	for _, v := range b {
		if !aMap[v] {
			return false
		}
	}

	return true
}
