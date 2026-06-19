package azure

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestProviderName(t *testing.T) {
	p := NewProvider()
	if got := p.Name(); got != providerName {
		t.Errorf("Name() = %q, want \"azure\"", got)
	}
}

func TestProviderValidateRejectsBadConfig(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			providerName: map[string]any{
				// missing region — must fail
				"node_groups": map[string]any{
					"system": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      modeSystem,
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "00000000-0000-0000-0000-000000000000")
	err := p.Validate(context.Background(), "myproj", cc)
	if err == nil {
		t.Fatal("expected validation error for missing region")
	}
}

func TestProviderValidateRequiresSubscriptionID(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			providerName: map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"system": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      modeSystem,
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Validate(context.Background(), "myproj", cc)
	if err == nil {
		t.Fatal("expected error when AZURE_SUBSCRIPTION_ID is unset")
	}
}

func TestProviderDeployFailsWithoutSubscription(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			providerName: map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"s": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      modeSystem,
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Deploy(context.Background(), "p", cc, cluster.DeployOptions{})
	if err == nil {
		t.Fatal("expected Deploy to fail without subscription ID")
	}
}

func TestProviderDestroyFailsWithoutSubscription(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			providerName: map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"s": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      modeSystem,
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Destroy(context.Background(), "p", cc, cluster.DestroyOptions{})
	if err == nil {
		t.Fatal("expected Destroy to fail without subscription ID")
	}
}

func TestProviderInfraSettings(t *testing.T) {
	p := NewProvider()
	settings := p.InfraSettings(nil)
	if settings.StorageClass != "managed-csi" {
		t.Errorf("StorageClass = %q, want managed-csi", settings.StorageClass)
	}
	if settings.NeedsMetalLB {
		t.Error("NeedsMetalLB = true, want false")
	}
}

func TestProviderSummaryWithConfig(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			providerName: map[string]any{
				"region":              "eastus",
				"resource_group_name": "rg-1",
				"node_groups": map[string]any{
					"a": map[string]any{},
					"b": map[string]any{},
				},
			},
		},
	}
	s := p.Summary(cc)
	if s["Region"] != "eastus" {
		t.Errorf("Region = %q", s["Region"])
	}
	if s["ResourceGroup"] != "rg-1" {
		t.Errorf("ResourceGroup = %q", s["ResourceGroup"])
	}
	if s["NodeGroupCount"] != "2" {
		t.Errorf("NodeGroupCount = %q, want 2", s["NodeGroupCount"])
	}
}
