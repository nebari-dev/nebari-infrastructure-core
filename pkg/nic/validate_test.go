package nic

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns/cloudflare"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// testConfig returns a minimal valid config with the given domain and DNS
// provider config. dnsConfig nil means no dns block.
func testConfig(domain string, dnsProviders map[string]any) *config.NebariConfig {
	cfg := &config.NebariConfig{
		ProjectName: "test-project",
		Domain:      domain,
		Cluster: &config.ClusterConfig{
			Providers: map[string]any{"aws": map[string]any{}},
		},
	}
	if dnsProviders != nil {
		cfg.DNS = &config.DNSConfig{Providers: dnsProviders}
	}
	return cfg
}

func TestValidateDNSProvider(t *testing.T) {
	ctx := context.Background()

	reg := registry.NewRegistry()
	if err := reg.DNSProviders.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
		t.Fatalf("register cloudflare dns provider: %v", err)
	}

	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "no dns block is a no-op",
			cfg:     testConfig("", nil),
			wantErr: false,
		},
		{
			name: "domain within zone",
			cfg: testConfig("nebari.example.com", map[string]any{
				"cloudflare": map[string]any{"zone_name": "example.com"},
			}),
			wantErr: false,
		},
		{
			name: "domain outside zone rejected",
			cfg: testConfig("nebari.other.com", map[string]any{
				"cloudflare": map[string]any{"zone_name": "example.com"},
			}),
			wantErr:     true,
			errContains: "is not within DNS zone_name",
		},
		{
			name: "missing domain rejected",
			cfg: testConfig("", map[string]any{
				"cloudflare": map[string]any{"zone_name": "example.com"},
			}),
			wantErr:     true,
			errContains: "domain is required",
		},
		{
			name: "unregistered provider",
			cfg: testConfig("nebari.example.com", map[string]any{
				"notreal": map[string]any{"zone_name": "example.com"},
			}),
			wantErr:     true,
			errContains: "get dns provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDNSProvider(ctx, tt.cfg, reg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateDNSProvider() expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateDNSProvider() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateDNSProvider() unexpected error: %v", err)
			}
		})
	}
}

// TestStructuralValidatePermitsZoneInconsistency pins the destroy/kubeconfig
// behavior: those commands validate via cfg.Validate(validateOptions(...))
// only and never call validateDNSProvider, so a config whose domain is
// missing or outside the DNS zone must still pass structural validation.
// This keeps a cluster with a stale DNS config destroyable.
func TestStructuralValidatePermitsZoneInconsistency(t *testing.T) {
	opts := config.ValidateOptions{
		ClusterProviders: []string{"aws"},
		DNSProviders:     []string{"cloudflare"},
	}

	for _, cfg := range []*config.NebariConfig{
		// domain outside zone
		testConfig("nebari.other.com", map[string]any{
			"cloudflare": map[string]any{"zone_name": "example.com"},
		}),
		// dns block with no domain
		testConfig("", map[string]any{
			"cloudflare": map[string]any{"zone_name": "example.com"},
		}),
		// dns block with no zone_name
		testConfig("nebari.example.com", map[string]any{
			"cloudflare": map[string]any{},
		}),
	} {
		if err := cfg.Validate(opts); err != nil {
			t.Errorf("structural Validate() unexpected error for %q: %v", cfg.Domain, err)
		}
	}
}
