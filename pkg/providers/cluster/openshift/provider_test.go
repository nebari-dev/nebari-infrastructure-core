package openshift

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
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
	if got["Context"] != "ctx" {
		t.Errorf("Summary Context = %q, want ctx", got["Context"])
	}
	if got["SCC"] != "privileged" {
		t.Errorf("Summary SCC = %q, want privileged", got["SCC"])
	}
}

func TestDeployDryRunExistingIsNoop(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "existing", "context": "ctx"})
	err := NewProvider().Deploy(context.Background(), "proj", cc, cluster.DeployOptions{DryRun: true})
	if err != nil {
		t.Errorf("dry-run Deploy = %v, want nil", err)
	}
}

// TestDeployProvisionRequiresRegion exercises the fast-fail guard at the top of
// the provision path: with no region we must error before touching AWS/STS, so
// the test stays hermetic (no credentials or network).
func TestDeployProvisionRequiresRegion(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "provision"})
	err := NewProvider().Deploy(context.Background(), "proj", cc, cluster.DeployOptions{})
	if err == nil {
		t.Fatal("provision Deploy without region = nil, want error")
	}
}

// TestDestroyExistingIsNoop confirms existing-mode Destroy does nothing (NIC did
// not provision the cluster).
func TestDestroyExistingIsNoop(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "existing", "context": "ctx"})
	if err := NewProvider().Destroy(context.Background(), "proj", cc, cluster.DestroyOptions{}); err != nil {
		t.Errorf("existing Destroy = %v, want nil", err)
	}
}
