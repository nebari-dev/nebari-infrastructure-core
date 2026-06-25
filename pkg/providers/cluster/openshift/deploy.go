package openshift

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// Deploy provisions (provision mode) or connects to (existing mode) the cluster
// and applies OpenShift prerequisites. Implemented in Task B8.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts cluster.DeployOptions) error {
	_ = ctx
	_ = projectName
	_ = clusterConfig
	_ = opts
	return fmt.Errorf("openshift: Deploy not yet implemented")
}

// Destroy tears down provisioned infrastructure (provision mode) or is a no-op
// (existing mode). Implemented in Task B8.
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts cluster.DestroyOptions) error {
	_ = ctx
	_ = projectName
	_ = clusterConfig
	_ = opts
	return fmt.Errorf("openshift: Destroy not yet implemented")
}

// Summary returns key configuration details for display. Implemented in Task B8.
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	result := make(map[string]string)
	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg == nil {
		return result
	}
	var cfg Config
	if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &cfg); err != nil {
		return result
	}
	result["Mode"] = cfg.Mode()
	return result
}
