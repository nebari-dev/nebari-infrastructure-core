package azure

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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

// ConfigKey returns the YAML configuration key for Azure
func (p *Provider) ConfigKey() string {
	return "azure"
}

// Validate validates the Azure configuration (stub implementation)
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "azure.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Validating Azure provider configuration").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// Deploy deploys Azure infrastructure (stub implementation)
func (p *Provider) Deploy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "azure.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", cfg.ProjectName),
	)

	if rawCfg := cfg.ProviderConfig["azure"]; rawCfg != nil {
		var azureCfg Config
		if err := config.UnmarshalProviderConfig(ctx, rawCfg, &azureCfg); err == nil {
			span.SetAttributes(attribute.String("azure.region", azureCfg.Region))
		}
	}

	// Marshal config to JSON for status message
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Azure provider deployment (stub)").
		WithResource("provider").
		WithAction("deploy").
		WithMetadata("cluster_name", cfg.ProjectName).
		WithMetadata("config", string(configJSON)))

	return nil
}

// Destroy tears down Azure infrastructure (stub implementation)
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "azure.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying Azure provider infrastructure (stub)").
		WithResource("provider").
		WithAction("destroy").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil
}

// GetKubeconfig generates a kubeconfig file (stub implementation)
func (p *Provider) GetKubeconfig(ctx context.Context, cfg *config.NebariConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "azure.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("cluster_name", cfg.ProjectName),
	)

	status.Send(ctx, status.NewUpdate(status.LevelWarning, "GetKubeconfig not yet implemented for Azure provider").
		WithResource("provider").
		WithAction("get-kubeconfig").
		WithMetadata("cluster_name", cfg.ProjectName))
	return nil, fmt.Errorf("GetKubeconfig not yet implemented")
}

// Summary returns key configuration details for display purposes
func (p *Provider) Summary(cfg *config.NebariConfig) map[string]string {
	result := make(map[string]string)

	rawCfg := cfg.ProviderConfig["azure"]
	if rawCfg == nil {
		return result
	}

	var azureCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &azureCfg); err != nil {
		return result
	}

	result["Region"] = azureCfg.Region
	return result
}
