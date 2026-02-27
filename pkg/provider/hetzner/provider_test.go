package hetzner

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

func TestProvider_Name(t *testing.T) {
	p := NewProvider()
	if p.Name() != "hetzner" {
		t.Errorf("Name() = %q, want %q", p.Name(), "hetzner")
	}
}

func TestProvider_ConfigKey(t *testing.T) {
	p := NewProvider()
	if p.ConfigKey() != "hetzner_cloud" {
		t.Errorf("ConfigKey() = %q, want %q", p.ConfigKey(), "hetzner_cloud")
	}
}

func TestProvider_InfraSettings(t *testing.T) {
	p := NewProvider()

	tests := []struct {
		name    string
		cfg     *config.NebariConfig
		wantSC  string
		wantLBA map[string]string
		wantKBP string
		wantMLB bool
	}{
		{
			name: "default settings with location",
			cfg: &config.NebariConfig{
				Provider: "hetzner",
				ProviderConfig: map[string]any{
					"hetzner_cloud": map[string]any{
						"location": "ash",
					},
				},
			},
			wantSC:  "hcloud-volumes",
			wantLBA: map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
			wantKBP: "/auth",
			wantMLB: false,
		},
		{
			name: "nil provider config uses defaults",
			cfg: &config.NebariConfig{
				Provider: "hetzner",
			},
			wantSC:  "hcloud-volumes",
			wantKBP: "/auth",
			wantMLB: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := p.InfraSettings(tt.cfg)
			if settings.StorageClass != tt.wantSC {
				t.Errorf("StorageClass = %q, want %q", settings.StorageClass, tt.wantSC)
			}
			if settings.NeedsMetalLB != tt.wantMLB {
				t.Errorf("NeedsMetalLB = %v, want %v", settings.NeedsMetalLB, tt.wantMLB)
			}
			if settings.KeycloakBasePath != tt.wantKBP {
				t.Errorf("KeycloakBasePath = %q, want %q", settings.KeycloakBasePath, tt.wantKBP)
			}
			if tt.wantLBA != nil {
				for k, v := range tt.wantLBA {
					if settings.LoadBalancerAnnotations[k] != v {
						t.Errorf("LB annotation %q = %q, want %q", k, settings.LoadBalancerAnnotations[k], v)
					}
				}
			}
		})
	}
}

// Compile-time check that Provider implements the interface
var _ provider.Provider = (*Provider)(nil)
