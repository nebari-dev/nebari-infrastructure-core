package nic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestEnsureLocalRepositorySupported(t *testing.T) {
	tests := []struct {
		name                string
		repoProviders       map[string]any
		supportsLocalGitOps bool
		wantErr             bool
	}{
		{
			name:                "local repository on unsupported cluster rejected",
			repoProviders:       map[string]any{"local": map[string]any{}},
			supportsLocalGitOps: false,
			wantErr:             true,
		},
		{
			name:                "local repository on supported cluster",
			repoProviders:       map[string]any{"local": map[string]any{}},
			supportsLocalGitOps: true,
			wantErr:             false,
		},
		{
			name:                "existing repository never gated",
			repoProviders:       map[string]any{"existing": map[string]any{}},
			supportsLocalGitOps: false,
			wantErr:             false,
		},
		{
			name:                "no repository block is a no-op",
			repoProviders:       nil,
			supportsLocalGitOps: false,
			wantErr:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureLocalRepositorySupported(testRepoConfig(tt.repoProviders), tt.supportsLocalGitOps)
			if tt.wantErr && err == nil {
				t.Fatal("ensureLocalRepositorySupported() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ensureLocalRepositorySupported() unexpected error: %v", err)
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

// TestRejectPlaceholders covers the placeholder gate at the layer that owns it.
// The end-to-end wiring is pinned by the cmd tests and by
// TestExampleConfigsValidate; this pins the behavior itself, including the two
// cases that have no YAML source to scan.
func TestRejectPlaceholders(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		path        string
		wantErr     bool
		wantFields  []string
		wantMention string
	}{
		{
			name:        "placeholder in a scalar is rejected and names the file",
			raw:         "project_name: CHANGEME\n",
			path:        "/tmp/nebari-config.yaml",
			wantErr:     true,
			wantFields:  []string{"project_name"},
			wantMention: "/tmp/nebari-config.yaml",
		},
		{
			name:       "placeholder in a mapping key is rejected",
			raw:        "cluster:\n  aws:\n    node_groups:\n      CHANGEME:\n        instance: m5.large\n",
			path:       "cfg.yaml",
			wantErr:    true,
			wantFields: []string{"cluster.aws.node_groups.CHANGEME"},
		},
		{
			name: "filled config passes",
			raw:  "project_name: my-nebari\n",
			path: "cfg.yaml",
		},
		{
			// A config built in Go has no YAML text, so there is nothing to scan
			// and the gate must not invent a failure.
			name: "config with no source is a no-op",
			raw:  "",
			path: "",
		},
		{
			// Parsed from bytes rather than from a file: the gate must still
			// run, or a caller using ParseConfigBytes would deploy an unedited
			// config with no check and no signal that one was skipped. There is
			// no path to report, so the error names fields only.
			name:       "parsed from bytes is still gated",
			raw:        "bytes-only",
			wantErr:    true,
			wantFields: []string{"project_name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *config.NebariConfig
			switch tt.raw {
			case "":
				cfg = &config.NebariConfig{ProjectName: "CHANGEME"}
			case "bytes-only":
				var err error
				cfg, err = config.ParseConfigBytes([]byte("project_name: CHANGEME\n"))
				if err != nil {
					t.Fatalf("ParseConfigBytes() error = %v", err)
				}
			default:
				dir := t.TempDir()
				path := filepath.Join(dir, "nebari-config.yaml")
				if err := os.WriteFile(path, []byte(tt.raw), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				cfg, err = config.ParseConfig(context.Background(), path)
				if err != nil {
					t.Fatalf("ParseConfig() error = %v", err)
				}
				tt.wantMention = strings.Replace(tt.wantMention, "/tmp/nebari-config.yaml", path, 1)
			}

			err := rejectPlaceholders(cfg)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("rejectPlaceholders() = %v, want nil", err)
				}
				return
			}

			var placeholderErr *config.PlaceholderError
			if !errors.As(err, &placeholderErr) {
				t.Fatalf("rejectPlaceholders() = %v, want *config.PlaceholderError", err)
			}
			if !reflect.DeepEqual(placeholderErr.FieldPaths, tt.wantFields) {
				t.Errorf("FieldPaths = %v, want %v", placeholderErr.FieldPaths, tt.wantFields)
			}
			if tt.wantMention != "" && !strings.Contains(err.Error(), tt.wantMention) {
				t.Errorf("error %q does not name the config file %q", err, tt.wantMention)
			}
		})
	}
}
