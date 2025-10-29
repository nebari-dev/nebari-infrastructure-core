package local

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider implements the local K3s provider
type Provider struct{}

// NewProvider creates a new local provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "local"
}

// Deploy deploys local K3s infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if cfg.Local != nil {
		span.SetAttributes(attribute.String("local.kube_context", cfg.Local.KubeContext))
	}

	// Marshal config to JSON for pretty printing
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Printf("local.Deploy called with the following parameters:\n%s\n", string(configJSON))

	return nil
}
