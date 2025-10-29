package gcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider implements the GCP provider
type Provider struct{}

// NewProvider creates a new GCP provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "gcp"
}

// Deploy deploys GCP infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "gcp.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if cfg.GoogleCloudPlatform != nil {
		span.SetAttributes(
			attribute.String("gcp.project", cfg.GoogleCloudPlatform.Project),
			attribute.String("gcp.region", cfg.GoogleCloudPlatform.Region),
		)
	}

	// Marshal config to JSON for pretty printing
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Printf("gcp.Deploy called with the following parameters:\n%s\n", string(configJSON))

	return nil
}
