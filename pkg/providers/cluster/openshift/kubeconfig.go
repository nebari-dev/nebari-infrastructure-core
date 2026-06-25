package openshift

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
)

// GetKubeconfig returns a kubeconfig for the OpenShift cluster.
//
// In existing mode it loads the configured kubeconfig file and filters it to the
// selected context (mirroring the existing provider). In provision mode the
// kubeconfig is derived from the ROSA cluster after apply; that path is not yet
// wired and returns a descriptive error.
//
// Results are cached in-memory per Provider instance.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "openshift.GetKubeconfig")
	defer span.End()

	cfg, err := extractConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("provider", ProviderName),
		attribute.String("mode", cfg.Mode()),
		attribute.String("project_name", projectName),
	)

	switch cfg.Mode() {
	case ModeExisting:
		return p.existingKubeconfig(cfg)
	case ModeProvision:
		return nil, fmt.Errorf("openshift: provision-mode kubeconfig retrieval is not yet implemented; provision the cluster then target it via mode: existing")
	default:
		return nil, fmt.Errorf("invalid openshift mode %q", cfg.Mode())
	}
}

// existingKubeconfig loads + filters the kubeconfig for the configured context,
// caching the serialized result.
func (p *Provider) existingKubeconfig(cfg *Config) ([]byte, error) {
	cacheKey := "existing:" + cfg.Context

	p.kubeconfigMu.RLock()
	if cached, ok := p.kubeconfigCache[cacheKey]; ok {
		p.kubeconfigMu.RUnlock()
		return cached, nil
	}
	p.kubeconfigMu.RUnlock()

	path, err := cfg.GetKubeconfigPath()
	if err != nil {
		return nil, err
	}
	data, err := kubeconfig.LoadFromPath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig from %s: %w", path, err)
	}
	filtered, err := kubeconfig.FilterByContext(data, cfg.Context)
	if err != nil {
		return nil, err
	}
	out, err := kubeconfig.WriteBytes(filtered)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize kubeconfig: %w", err)
	}

	p.kubeconfigMu.Lock()
	p.kubeconfigCache[cacheKey] = out
	p.kubeconfigMu.Unlock()
	return out, nil
}
