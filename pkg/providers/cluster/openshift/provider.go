// Package openshift implements a dual-mode NIC cluster provider for Red Hat
// OpenShift. In "provision" mode it stands up a ROSA HCP cluster via OpenTofu
// (mirroring the aws provider); in "existing" mode it connects to an
// already-running OpenShift cluster via a kubeconfig context (mirroring the
// existing provider). Either way it applies the OpenShift-specific
// SecurityContextConstraints bindings Nebari's foundational workloads need.
package openshift

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// ProviderName is the identifier for the OpenShift provider.
const ProviderName = "openshift"

// Provider implements the OpenShift cluster provider. Not safe to copy once
// constructed (embeds a mutex). Always pass *Provider.
type Provider struct {
	kubeconfigMu    sync.RWMutex
	kubeconfigCache map[string][]byte
}

// compile-time assertion that *Provider satisfies the cluster.Provider contract.
var _ cluster.Provider = (*Provider)(nil)

// NewProvider creates a new OpenShift provider.
func NewProvider() *Provider {
	return &Provider{
		kubeconfigCache: make(map[string][]byte),
	}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return ProviderName
}

// extractConfig converts the generic provider config to the OpenShift Config type.
func extractConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (*Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "openshift.extractConfig")
	defer span.End()

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		err := fmt.Errorf("openshift configuration is required")
		span.RecordError(err)
		return nil, err
	}

	var cfg Config
	if err := config.UnmarshalProviderConfig(ctx, rawCfg, &cfg); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("failed to unmarshal openshift config: %w", err)
	}
	return &cfg, nil
}

// Validate is implemented in validate.go.
// Deploy/Destroy/Summary are implemented in deploy.go.
// GetKubeconfig is implemented in kubeconfig.go.
// InfraSettings is implemented in infra.go.
