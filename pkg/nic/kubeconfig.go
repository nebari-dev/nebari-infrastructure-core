package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// Kubeconfig returns the raw kubeconfig bytes for the cluster described by
// cfg. The caller decides where to write them (stdout, file, or merge into
// an existing kubeconfig).
func (c *Client) Kubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Kubeconfig")
	defer span.End()

	reg := c.registry

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Configuration validated").
		WithResource("config").
		WithAction("validated").
		WithMetadata("provider", cfg.Cluster.ProviderName()).
		WithMetadata("project_name", cfg.ProjectName))

	clusterProvider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get cluster provider: %w", err)
	}

	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	return kubeconfigBytes, nil
}
