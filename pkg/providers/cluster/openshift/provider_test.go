package openshift

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestProviderName(t *testing.T) {
	if got := NewProvider().Name(); got != "openshift" {
		t.Errorf("Name() = %q, want openshift", got)
	}
}

// clusterConfig builds a *config.ClusterConfig with the openshift provider key,
// matching how the config layer hands provider config to the provider.
func clusterConfig(providerCfg map[string]any) *config.ClusterConfig {
	return &config.ClusterConfig{
		Providers: map[string]any{"openshift": providerCfg},
	}
}

func TestSummaryReportsMode(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "existing", "context": "ctx"})
	got := NewProvider().Summary(cc)
	if got["Mode"] != "existing" {
		t.Errorf("Summary Mode = %q, want existing", got["Mode"])
	}
}
