package nic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns/cloudflare"
	repositoryexisting "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository/existing"
	repositorylocal "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository/local"
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
		Repository: &config.RepositoryConfig{
			Providers: map[string]any{"existing": map[string]any{}},
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

// testRepoConfig returns a minimal config with the given repository provider
// config. repoProviders nil means no repository block.
func testRepoConfig(repoProviders map[string]any) *config.NebariConfig {
	cfg := &config.NebariConfig{
		ProjectName: "test-project",
		Cluster: &config.ClusterConfig{
			Providers: map[string]any{"aws": map[string]any{}},
		},
	}
	if repoProviders != nil {
		cfg.Repository = &config.RepositoryConfig{Providers: repoProviders}
	}
	return cfg
}

func TestValidateRepositoryProvider(t *testing.T) {
	ctx := context.Background()

	reg := registry.NewRegistry()
	if err := reg.RepositoryProviders.Register(ctx, repositoryexisting.ProviderName, repositoryexisting.NewProvider()); err != nil {
		t.Fatalf("register existing repository provider: %v", err)
	}
	if err := reg.RepositoryProviders.Register(ctx, repositorylocal.ProviderName, repositorylocal.NewProvider()); err != nil {
		t.Fatalf("register local repository provider: %v", err)
	}

	occupiedPath := filepath.Join(t.TempDir(), "gitops")
	if err := os.WriteFile(occupiedPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "no repository block is a no-op",
			cfg:     testRepoConfig(nil),
			wantErr: false,
		},
		{
			name: "existing with url and token",
			cfg: testRepoConfig(map[string]any{
				"existing": map[string]any{
					"url":  "https://github.com/org/repo.git",
					"auth": map[string]any{"token": map[string]any{"env": "GIT_TOKEN"}},
				},
			}),
			wantErr: false,
		},
		{
			name: "existing without url rejected",
			cfg: testRepoConfig(map[string]any{
				"existing": map[string]any{
					"auth": map[string]any{"token": map[string]any{"env": "GIT_TOKEN"}},
				},
			}),
			wantErr:     true,
			errContains: "repository url is required",
		},
		{
			name: "existing without auth rejected",
			cfg: testRepoConfig(map[string]any{
				"existing": map[string]any{
					"url": "https://github.com/org/repo.git",
				},
			}),
			wantErr:     true,
			errContains: "one of token or ssh is required",
		},
		{
			name: "existing with both token and ssh rejected",
			cfg: testRepoConfig(map[string]any{
				"existing": map[string]any{
					"url": "https://github.com/org/repo.git",
					"auth": map[string]any{
						"token": map[string]any{"env": "GIT_TOKEN"},
						"ssh":   map[string]any{"env": "GIT_SSH_KEY"},
					},
				},
			}),
			wantErr:     true,
			errContains: "only one of token or ssh may be set",
		},
		{
			name:    "local with empty config",
			cfg:     testRepoConfig(map[string]any{"local": map[string]any{}}),
			wantErr: false,
		},
		{
			name: "local with relative path rejected",
			cfg: testRepoConfig(map[string]any{
				"local": map[string]any{"path": "relative/gitops"},
			}),
			wantErr:     true,
			errContains: "path must be an absolute directory",
		},
		{
			name: "local with path occupied by a file rejected",
			cfg: testRepoConfig(map[string]any{
				"local": map[string]any{"path": occupiedPath},
			}),
			wantErr:     true,
			errContains: "is not a directory",
		},
		{
			name:        "unregistered provider",
			cfg:         testRepoConfig(map[string]any{"notreal": map[string]any{}}),
			wantErr:     true,
			errContains: "get repository provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepositoryProvider(ctx, tt.cfg, reg)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateRepositoryProvider() expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateRepositoryProvider() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateRepositoryProvider() unexpected error: %v", err)
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

// TestValidateAllowsLocalRepositoryOnNonLocalCluster pins the contract that a
// local repository is usable with any cluster provider, not just the kind-backed
// local one. NIC cannot tell whether a cluster's nodes can read a host path: a
// k3d or minikube cluster reached through the `existing` provider can, when the
// operator mounted the directory into the nodes, so the pairing is the
// operator's call to make.
func TestValidateAllowsLocalRepositoryOnNonLocalCluster(t *testing.T) {
	ctx := context.Background()

	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	cfg := &config.NebariConfig{
		ProjectName: "test-project",
		Cluster: &config.ClusterConfig{
			Providers: map[string]any{
				"existing": map[string]any{"context": "k3d-test-project"},
			},
		},
		Repository: &config.RepositoryConfig{
			Providers: map[string]any{
				"local": map[string]any{"path": t.TempDir()},
			},
		},
	}

	if err := client.Validate(ctx, cfg); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}
