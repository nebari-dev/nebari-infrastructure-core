package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Validate checks that cfg is well-formed and references providers that are
// actually registered. It performs no I/O against cloud APIs. Returns nil
// when cfg is valid, or an error describing the first validation failure.
func (c *Client) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Validate")
	defer span.End()

	if err := cfg.Validate(validateOptions(ctx, c.registry)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Reject Longhorn backups on a cluster whose storage layer is not Longhorn.
	// InfraSettings is a pure getter (no cloud I/O), so we can consult the
	// registered provider here and catch the misconfiguration at validate time
	// rather than mid-deploy.
	clusterProvider, err := c.registry.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get cluster provider %q: %w", cfg.Cluster.ProviderName(), err)
	}
	if err := ensureBackupsHaveLonghorn(cfg, clusterProvider.InfraSettings(cfg.Cluster).StorageClass); err != nil {
		span.RecordError(err)
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	return nil
}
