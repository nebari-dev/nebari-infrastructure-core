package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// reconcileVPC compares desired VPC configuration against actual state and reconciles differences
func (p *Provider) reconcileVPC(ctx context.Context, clients *Clients, cfg *config.NebariConfig, actual *VPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "aws.reconcileVPC")
	defer span.End()

	clusterName := cfg.ProjectName
	awsCfg := cfg.AmazonWebServices

	span.SetAttributes(
		attribute.String("cluster_name", clusterName),
		attribute.Bool("vpc_exists", actual != nil),
	)

	// Case 1: VPC doesn't exist → Create
	if actual == nil {
		span.SetAttributes(attribute.String("action", "create"))
		_, err := p.createVPC(ctx, clients, cfg)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create VPC: %w", err)
		}
		return nil
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
		return err
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
			return err
		}
	}

	// Case 3: VPC exists and immutable fields match → No action needed
	span.SetAttributes(attribute.String("action", "none"))

	return nil
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
