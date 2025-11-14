package aws

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// reconcileVPC compares desired VPC configuration against actual state and reconciles differences
func (p *Provider) reconcileVPC(ctx context.Context, clients *AWSClients, cfg *config.NebariConfig, actual *AWSVPCState) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.reconcileVPC")
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

	// Case 3: VPC exists and CIDR matches → No action needed
	span.SetAttributes(attribute.String("action", "none"))

	return nil
}
