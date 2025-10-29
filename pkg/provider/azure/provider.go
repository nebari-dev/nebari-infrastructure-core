package azure

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider implements the Azure provider
type Provider struct{}

// NewProvider creates a new Azure provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "azure"
}

// Deploy deploys Azure infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if cfg.Azure != nil {
		span.SetAttributes(attribute.String("azure.region", cfg.Azure.Region))
	}

	// Marshal config to JSON for pretty printing
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Printf("azure.Deploy called with the following parameters:\n%s\n", string(configJSON))

	return nil
}
