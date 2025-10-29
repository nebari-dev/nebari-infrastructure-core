package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider implements the AWS provider
type Provider struct{}

// NewProvider creates a new AWS provider
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "aws"
}

// Deploy deploys AWS infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "aws.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "aws"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if cfg.AmazonWebServices != nil {
		span.SetAttributes(attribute.String("aws.region", cfg.AmazonWebServices.Region))
	}

	// Marshal config to JSON for pretty printing
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Printf("aws.Deploy called with the following parameters:\n%s\n", string(configJSON))

	return nil
}
