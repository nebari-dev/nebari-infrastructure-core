package local

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// Compile-time interface compliance check
var _ provider.Provider = (*Provider)(nil)

func TestInfraSettings(t *testing.T) {
	p := NewProvider()
	cfg := &config.NebariConfig{
		ProjectName: "test",
		Cluster: &config.ClusterConfig{
			Providers: map[string]any{"local": map[string]any{}},
		},
	}

	settings := p.InfraSettings(cfg)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"StorageClass", settings.StorageClass, "standard"},
		{"NeedsMetalLB", settings.NeedsMetalLB, true},
		{"MetalLBAddressPool", settings.MetalLBAddressPool, "192.168.1.100-192.168.1.110"},
		{"LoadBalancerAnnotations is empty", len(settings.LoadBalancerAnnotations), 0},
		{"KeycloakBasePath is empty", settings.KeycloakBasePath, ""},
		{"SupportsLocalGitOps", settings.SupportsLocalGitOps, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
