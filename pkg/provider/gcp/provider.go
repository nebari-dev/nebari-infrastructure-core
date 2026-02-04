package gcp

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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

// ConfigKey returns the YAML configuration key for GCP
func (p *Provider) ConfigKey() string {
	return "google_cloud_platform"
}

// Validate validates the GCP configuration (stub implementation)
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating GCP provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// Deploy deploys GCP infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if rawCfg := cfg.ProviderConfig["google_cloud_platform"]; rawCfg != nil {
		var gcpCfg Config
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &gcpCfg); err == nil {
			span.SetAttributes(
				attribute.String("gcp.project", gcpCfg.Project),
				attribute.String("gcp.region", gcpCfg.Region),
			)
		}
	}

	// Marshal config to JSON for status message
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "GCP provider deployment (stub)").
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("config", string(configJSON)))

	return nil
}

// Destroy tears down GCP infrastructure (stub implementation)
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying GCP provider infrastructure (stub)").
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// GetKubeconfig generates a kubeconfig file (stub implementation)
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("cluster_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelWarning, "GetKubeconfig not yet implemented for GCP provider").
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil, fmt.Errorf("GetKubeconfig not yet implemented")
}

// Summary returns key configuration details for display purposes
func (p *Provider) Summary(cfg *config.NebariConfig) map[string]string {
	result := make(map[string]string)

	rawCfg := cfg.ProviderConfig["google_cloud_platform"]
	if rawCfg == nil {
		return result
	}

	var gcpCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &gcpCfg); err != nil {
		return result
	}

	result["Project"] = gcpCfg.Project
	result["Region"] = gcpCfg.Region
	return result
}
