package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// ClusterSummary returns the cluster provider's short description of the
// cluster cfg describes (region, location, kubeconfig context, ...), as
// shown in the destroy confirmation. It reads the config only; it makes no
// provider API call.
func (c *Client) ClusterSummary(ctx context.Context, cfg *config.NebariConfig) (map[string]string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.ClusterSummary")
	defer span.End()

	// ProviderName is "" without a cluster block, which the registry rejects.
	name := cfg.Cluster.ProviderName()
	span.SetAttributes(attribute.String("provider", name))

	provider, err := c.registry.ClusterProviders.Get(ctx, name)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get cluster provider: %w", err)
	}
	return provider.Summary(cfg.Cluster), nil
}
