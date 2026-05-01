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

	tests := []struct {
		name           string
		providerConfig map[string]any
		wantSC         string
		wantMetalLB    bool
		wantPool       string
		wantHTTPSPort  int
	}{
		{
			name:           "no local config block returns defaults",
			providerConfig: nil,
			wantSC:         "standard",
			wantMetalLB:    true,
			wantPool:       "192.168.1.100-192.168.1.110",
			wantHTTPSPort:  0,
		},
		{
			name:           "empty local config returns defaults",
			providerConfig: map[string]any{"local": map[string]any{}},
			wantSC:         "standard",
			wantMetalLB:    true,
			wantPool:       "192.168.1.100-192.168.1.110",
			wantHTTPSPort:  0,
		},
		{
			name: "storage_class override",
			providerConfig: map[string]any{
				"local": map[string]any{"storage_class": "local-path"},
			},
			wantSC:        "local-path",
			wantMetalLB:   true,
			wantPool:      "192.168.1.100-192.168.1.110",
			wantHTTPSPort: 0,
		},
		{
			name: "metallb disabled",
			providerConfig: map[string]any{
				"local": map[string]any{
					"metallb": map[string]any{"enabled": false},
				},
			},
			wantSC:        "standard",
			wantMetalLB:   false,
			wantPool:      "192.168.1.100-192.168.1.110",
			wantHTTPSPort: 0,
		},
		{
			name: "metallb address_pool override",
			providerConfig: map[string]any{
				"local": map[string]any{
					"metallb": map[string]any{
						"address_pool": "172.18.255.100-172.18.255.110",
					},
				},
			},
			wantSC:        "standard",
			wantMetalLB:   true,
			wantPool:      "172.18.255.100-172.18.255.110",
			wantHTTPSPort: 0,
		},
		{
			name: "https_port override",
			providerConfig: map[string]any{
				"local": map[string]any{"https_port": 8443},
			},
			wantSC:        "standard",
			wantMetalLB:   true,
			wantPool:      "192.168.1.100-192.168.1.110",
			wantHTTPSPort: 8443,
		},
		{
			name: "full override",
			providerConfig: map[string]any{
				"local": map[string]any{
					"storage_class": "local-path",
					"https_port":    8443,
					"metallb": map[string]any{
						"enabled":      false,
						"address_pool": "10.0.0.100-10.0.0.110",
					},
				},
			},
			wantSC:        "local-path",
			wantMetalLB:   false,
			wantPool:      "10.0.0.100-10.0.0.110",
			wantHTTPSPort: 8443,
		},
		{
			name: "unmarshal error returns defaults",
			providerConfig: map[string]any{
				"local": "not-a-map",
			},
			wantSC:        "standard",
			wantMetalLB:   true,
			wantPool:      "192.168.1.100-192.168.1.110",
			wantHTTPSPort: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{
				Providers: tt.providerConfig,
			}

			settings := p.InfraSettings(cfg)

			if settings.StorageClass != tt.wantSC {
				t.Errorf("StorageClass = %q, want %q", settings.StorageClass, tt.wantSC)
			}
			if settings.NeedsMetalLB != tt.wantMetalLB {
				t.Errorf("NeedsMetalLB = %v, want %v", settings.NeedsMetalLB, tt.wantMetalLB)
			}
			if settings.MetalLBAddressPool != tt.wantPool {
				t.Errorf("MetalLBAddressPool = %q, want %q", settings.MetalLBAddressPool, tt.wantPool)
			}
			if settings.HTTPSPort != tt.wantHTTPSPort {
				t.Errorf("HTTPSPort = %d, want %d", settings.HTTPSPort, tt.wantHTTPSPort)
			}
			// Fields not set by local provider should always be zero values
			if len(settings.LoadBalancerAnnotations) != 0 {
				t.Errorf("LoadBalancerAnnotations = %v, want empty", settings.LoadBalancerAnnotations)
			}
			if settings.KeycloakBasePath != "" {
				t.Errorf("KeycloakBasePath = %q, want empty", settings.KeycloakBasePath)
			}
			if !settings.SupportsLocalGitOps {
				t.Error("SupportsLocalGitOps = false, want true")
			}
		})
	}
}
