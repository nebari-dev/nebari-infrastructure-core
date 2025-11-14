package local

import (
	"context"
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
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

// Validate validates the local configuration (stub implementation)
func (p *Provider) Validate(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	fmt.Printf("local.Validate called for cluster: %s\n", cfg.ProjectName)
	return nil
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

// Query discovers the current state of local infrastructure (stub implementation)
func (p *Provider) Query(ctx context.Context, clusterName string) (*provider.InfrastructureState, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Query")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("cluster_name", clusterName),
	)

	fmt.Printf("local.Query called for cluster: %s\n", clusterName)
	return nil, nil
}

// Reconcile reconciles local infrastructure state (stub implementation)
func (p *Provider) Reconcile(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Reconcile")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	fmt.Printf("local.Reconcile called for cluster: %s\n", cfg.ProjectName)
	return nil
}

// Destroy tears down local infrastructure (stub implementation)
func (p *Provider) Destroy(ctx context.Context, cfg *config.NebariConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("project_name", cfg.ProjectName),
	)

	fmt.Printf("local.Destroy called for cluster: %s\n", cfg.ProjectName)
	return nil
}

// GetKubeconfig generates a kubeconfig file (stub implementation)
func (p *Provider) GetKubeconfig(ctx context.Context, clusterName string) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.GetKubeconfig")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", "local"),
		attribute.String("cluster_name", clusterName),
	)

	fmt.Printf("local.GetKubeconfig called for cluster: %s\n", clusterName)
	return nil, fmt.Errorf("GetKubeconfig not yet implemented")
}
