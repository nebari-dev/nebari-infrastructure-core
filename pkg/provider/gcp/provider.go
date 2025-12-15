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

	if cfg.GoogleCloudPlatform != nil {
		var gcpCfg Config
		if err := config.UnmarshalProviderConfig(ctx, cfg.GoogleCloudPlatform, &gcpCfg); err == nil {
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

// Reconcile reconciles GCP infrastructure state (stub implementation)
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.Reconcile")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Reconciling GCP provider (stub)").
		WithResource("provider").
		WithAction("reconcile").
		WithMetadata("cluster_name", cfg.ProjectName))
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
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "gcp.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "gcp"),
		attribute.String("cluster_name", clusterName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelWarning, "GetKubeconfig not yet implemented for GCP provider").
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", clusterName))
	return nil, fmt.Errorf("GetKubeconfig not yet implemented")
}
