package local

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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

// ConfigKey returns the YAML configuration key for local
func (p *Provider) ConfigKey() string {
	return "local"
}

// Validate validates the local configuration (stub implementation)
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating local provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// Deploy deploys local K3s infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if rawCfg := cfg.ProviderConfig["local"]; rawCfg != nil {
		var localCfg Config
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &localCfg); err == nil {
			span.SetAttributes(attribute.String("local.kube_context", localCfg.KubeContext))
		}
	}

	// Marshal config to JSON for status message
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Local provider deployment (stub)").
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("config", string(configJSON)))

	return nil
}

// Reconcile reconciles local infrastructure state (stub implementation)
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Reconcile")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Reconciling local provider (stub)").
		WithResource("provider").
		WithAction("reconcile").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// Destroy tears down local infrastructure (stub implementation)
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying local provider infrastructure (stub)").
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// GetKubeconfig generates a kubeconfig file (stub implementation)
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "local.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("cluster_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelWarning, "GetKubeconfig not yet implemented for local provider").
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil, fmt.Errorf("GetKubeconfig not yet implemented")
}
