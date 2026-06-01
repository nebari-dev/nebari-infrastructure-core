package existing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

const ProviderName = "existing"

// Provider implements the existing-cluster provider.
// It connects to a pre-existing Kubernetes cluster via a kubeconfig context,
// skipping all infrastructure provisioning.
type Provider struct{}

// NewProvider creates a new existing-cluster provider.
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return ProviderName
}

// extractConfig converts the generic provider config to the existing-cluster Config type.
func extractConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.extractConfig")
	defer span.End()

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		err := fmt.Errorf("existing cluster configuration is required")
		span.RecordError(err)
		return nil, err
	}

	var existingCfg Config
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &existingCfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal existing cluster config: %w", err)
	}

	return &existingCfg, nil
}

// loadAndValidateContext loads the kubeconfig file and validates that the
// required context exists. Returns the kubeconfig path and context name.
func loadAndValidateContext(ctx context.Context, cfg *Config) (string, string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.loadAndValidateContext")
	defer span.End()

	if cfg.Context == "" {
		err := fmt.Errorf("context is required for existing cluster provider")
		span.RecordError(err)
		return "", "", err
	}

	path, err := cfg.GetKubeconfigPath()
	if err != nil {
		span.RecordError(err)
		return "", "", err
	}

	if err := kubeconfig.ValidateContext(path, cfg.Context); err != nil {
		span.RecordError(err)
		return "", "", err
	}

	span.SetAttributes(
		attribute.String("kubeconfig_path", path),
		attribute.String("kube_context", cfg.Context),
	)
	return path, cfg.Context, nil
}

// Validate checks that the existing-cluster configuration is valid.
// It verifies the kubeconfig file can be loaded and the resolved context exists.
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.Validate")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
	)

	existingCfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	path, contextName, err := loadAndValidateContext(ctx, existingCfg)
	if err != nil {
		span.RecordError(err)
		return err
	}

	span.SetAttributes(
		attribute.String("kubeconfig_path", path),
		attribute.String("kube_context", contextName),
		attribute.Bool("validation_passed", true),
	)
	return nil
}

// Deploy installs cluster-side prerequisites for the existing cluster.
// Infrastructure is assumed to already exist (the cluster was provisioned
// out-of-band), so this is mostly a no-op except for opt-in components like
// Longhorn that the user can request via the existing-cluster config.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts provider.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "existing.Deploy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
		attribute.Bool("dry_run", opts.DryRun),
	)

	if opts.DryRun {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Dry run: would deploy to existing cluster").
			WithResource("provider").WithAction("deploy"))
		return nil
	}

	existingCfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if existingCfg.Longhorn.IsEnabled() {
		kubeconfigBytes, err := p.GetKubeconfig(ctx, projectName, clusterConfig)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to get kubeconfig for Longhorn install: %w", err)
		}
		if err := longhorn.Install(ctx, kubeconfigBytes, existingCfg.Longhorn); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to install Longhorn: %w", err)
		}
	}

	return nil
}

// Destroy is a no-op for existing clusters.
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts provider.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.Destroy")
	defer span.End()

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("project_name", projectName),
		attribute.Bool("dry_run", opts.DryRun),
	)

	return nil
}

// GetKubeconfig returns the kubeconfig for the resolved context.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "existing.GetKubeconfig")
	defer span.End()

	existingCfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	path, err := existingCfg.GetKubeconfigPath()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	kubeconfigData, err := kubeconfig.LoadFromPath(path)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", path, err)
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("kubeconfig_path", path),
		attribute.String("kube_context", existingCfg.Context),
	)

	filtered, err := kubeconfig.FilterByContext(kubeconfigData, existingCfg.Context)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	kubeconfigBytes, err := kubeconfig.WriteBytes(filtered)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	span.SetAttributes(attribute.Int("kubeconfig_size_bytes", len(kubeconfigBytes)))
	return kubeconfigBytes, nil
}

// Summary returns key configuration details for display purposes.
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	result := make(map[string]string)

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		return result
	}

	var existingCfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &existingCfg); err != nil {
		return result
	}

	result["Provider"] = "Existing Cluster"
	if path, err := existingCfg.GetKubeconfigPath(); err == nil {
		result["Kubeconfig"] = path
	}

	result["Context"] = existingCfg.Context

	if existingCfg.StorageClass != "" {
		result["Storage Class"] = existingCfg.StorageClass
	}

	return result
}

// InfraSettings returns infrastructure settings for the existing cluster.
func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) provider.InfraSettings {
	settings := provider.InfraSettings{
		StorageClass: defaultStorageClass,
		NeedsMetalLB: false,
	}

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg != nil {
		var existingCfg Config
		if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &existingCfg); err == nil {
			settings.StorageClass = existingCfg.GetStorageClass()
			settings.LoadBalancerAnnotations = existingCfg.LoadBalancerAnnotations
		}
	}

	return settings
}
