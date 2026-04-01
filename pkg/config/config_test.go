package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

func TestIsValidDNSProvider(t *testing.T) {
	tests := []struct {
		name           string
		provider       string
		validProviders []string
		want           bool
	}{
		{
			name:           "cloudflare is valid",
			provider:       "cloudflare",
			validProviders: []string{"cloudflare"},
			want:           true,
		},
		{
			name:           "empty string is invalid",
			provider:       "",
			validProviders: []string{"cloudflare"},
			want:           false,
		},
		{
			name:           "unknown provider is invalid",
			provider:       "notreal",
			validProviders: []string{"cloudflare"},
			want:           false,
		},
		{
			name:           "Cloudflare uppercase is invalid",
			provider:       "Cloudflare",
			validProviders: []string{"cloudflare"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidProvider(tt.provider, tt.validProviders)
			if got != tt.want {
				t.Errorf("isValidProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestProviderConfig_NilMapAccess(t *testing.T) {
	// Verify that accessing a nil ProviderConfig map returns nil (Go behavior)
	cfg := &NebariConfig{
		// ProviderConfig is nil
	}

	// Reading from nil map should return nil, not panic
	got := cfg.ProviderConfig["amazon_web_services"]
	if got != nil {
		t.Errorf("Expected nil from nil map access, got %v", got)
	}
}

func TestProviderConfig_DirectAccess(t *testing.T) {
	type mockConfig struct {
		Region string
		Zone   string
	}

	cfg := &NebariConfig{
		ProviderConfig: map[string]any{
			"amazon_web_services": &mockConfig{Region: "us-west-2", Zone: "a"},
		},
	}

	// Access existing key
	rawCfg := cfg.ProviderConfig["amazon_web_services"]
	if rawCfg == nil {
		t.Fatal("Expected non-nil config for existing key")
	}

	awsCfg, ok := rawCfg.(*mockConfig)
	if !ok {
		t.Fatalf("Expected *mockConfig, got %T", rawCfg)
	}
	if awsCfg.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", awsCfg.Region, "us-west-2")
	}

	// Access non-existing key
	missing := cfg.ProviderConfig["nonexistent"]
	if missing != nil {
		t.Errorf("Expected nil for missing key, got %v", missing)
	}
}

func TestProviderConfig_MultipleProviders(t *testing.T) {
	// Verify multiple provider configs can coexist
	cfg := &NebariConfig{
		ProviderConfig: map[string]any{
			"amazon_web_services":   map[string]any{"region": "us-west-2"},
			"google_cloud_platform": map[string]any{"project": "my-project"},
			"azure":                 map[string]any{"region": "eastus"},
		},
	}

	// All should be accessible
	if cfg.ProviderConfig["amazon_web_services"] == nil {
		t.Error("AWS config should not be nil")
	}
	if cfg.ProviderConfig["google_cloud_platform"] == nil {
		t.Error("GCP config should not be nil")
	}
	if cfg.ProviderConfig["azure"] == nil {
		t.Error("Azure config should not be nil")
	}
}

func TestNebariConfig_RuntimeOptions(t *testing.T) {
	// Verify runtime options are independent of YAML parsing
	cfg := &NebariConfig{
		DryRun: true,
		Force:  true,
	}

	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
	if !cfg.Force {
		t.Error("Force should be true")
	}
}

func TestParseConfigBytes(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
		validate    func(t *testing.T, cfg *NebariConfig)
	}{
		{
			name: "minimal valid config",
			yaml: `
project_name: test-project
provider: aws
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.ProjectName != "test-project" {
					t.Errorf("ProjectName = %q, want %q", cfg.ProjectName, "test-project")
				}
				if cfg.Provider != "aws" {
					t.Errorf("Provider = %q, want %q", cfg.Provider, "aws")
				}
			},
		},
		{
			name: "config with git_repository",
			yaml: `
project_name: test-project
provider: aws
git_repository:
  url: "git@github.com:org/repo.git"
  branch: main
  path: "clusters/test"
  auth:
    ssh_key_env: GIT_SSH_KEY
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.GitRepository == nil {
					t.Fatal("GitRepository is nil")
				}
				if cfg.GitRepository.URL != "git@github.com:org/repo.git" {
					t.Errorf("GitRepository.URL = %q, want %q", cfg.GitRepository.URL, "git@github.com:org/repo.git")
				}
				if cfg.GitRepository.Branch != "main" {
					t.Errorf("GitRepository.Branch = %q, want %q", cfg.GitRepository.Branch, "main")
				}
				if cfg.GitRepository.Path != "clusters/test" {
					t.Errorf("GitRepository.Path = %q, want %q", cfg.GitRepository.Path, "clusters/test")
				}
				if cfg.GitRepository.Auth.SSHKeyEnv != "GIT_SSH_KEY" {
					t.Errorf("GitRepository.Auth.SSHKeyEnv = %q, want %q", cfg.GitRepository.Auth.SSHKeyEnv, "GIT_SSH_KEY")
				}
			},
		},
		{
			name: "config with git_repository and argocd_auth",
			yaml: `
project_name: test-project
provider: aws
git_repository:
  url: "https://github.com/org/repo.git"
  auth:
    token_env: GIT_TOKEN
  argocd_auth:
    token_env: ARGOCD_TOKEN
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.GitRepository == nil {
					t.Fatal("GitRepository is nil")
				}
				if cfg.GitRepository.Auth.TokenEnv != "GIT_TOKEN" {
					t.Errorf("GitRepository.Auth.TokenEnv = %q, want %q", cfg.GitRepository.Auth.TokenEnv, "GIT_TOKEN")
				}
				if cfg.GitRepository.ArgoCDAuth == nil {
					t.Fatal("GitRepository.ArgoCDAuth is nil")
				}
				if cfg.GitRepository.ArgoCDAuth.TokenEnv != "ARGOCD_TOKEN" {
					t.Errorf("GitRepository.ArgoCDAuth.TokenEnv = %q, want %q", cfg.GitRepository.ArgoCDAuth.TokenEnv, "ARGOCD_TOKEN")
				}
			},
		},
		{
			name: "missing provider",
			yaml: `
project_name: test-project
`,
			wantErr:     true,
			errContains: "provider field is required",
		},
		{
			name: "hetzner provider is accepted",
			yaml: `
project_name: test-project
provider: hetzner
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.Provider != "hetzner" {
					t.Errorf("Provider = %q, want %q", cfg.Provider, "hetzner")
				}
			},
		},
		{
			name: "unknown provider passes config validation",
			yaml: `
project_name: test-project
provider: unknown-provider
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.Provider != "unknown-provider" {
					t.Errorf("Provider = %q, want %q", cfg.Provider, "unknown-provider")
				}
			},
		},
		{
			name: "invalid git_repository - missing url",
			yaml: `
project_name: test-project
provider: aws
git_repository:
  branch: main
  auth:
    ssh_key_env: GIT_SSH_KEY
`,
			wantErr:     true,
			errContains: "url is required",
		},
		{
			name: "invalid git_repository - missing auth",
			yaml: `
project_name: test-project
provider: aws
git_repository:
  url: "git@github.com:org/repo.git"
`,
			wantErr:     true,
			errContains: "ssh_key_env or token_env is required",
		},
		{
			name: "invalid git_repository - both ssh and token",
			yaml: `
project_name: test-project
provider: aws
git_repository:
  url: "git@github.com:org/repo.git"
  auth:
    ssh_key_env: GIT_SSH_KEY
    token_env: GIT_TOKEN
`,
			wantErr:     true,
			errContains: "only one of",
		},
		{
			name: "new DNS format with nested provider",
			yaml: `
project_name: test-project
provider: aws
dns:
  cloudflare:
    zone_name: example.com
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.DNS == nil {
					t.Fatal("DNS is nil")
				}
				if cfg.DNS.ProviderName() != "cloudflare" {
					t.Errorf("DNS.ProviderName() = %q, want %q", cfg.DNS.ProviderName(), "cloudflare")
				}
				pc := cfg.DNS.ProviderConfig()
				if pc == nil {
					t.Fatal("DNS.ProviderConfig() is nil")
				}
				if pc["zone_name"] != "example.com" {
					t.Errorf("DNS.ProviderConfig()[zone_name] = %v, want %q", pc["zone_name"], "example.com")
				}
			},
		},
		{
			name: "old DNS format rejected",
			yaml: `
project_name: test-project
provider: aws
dns_provider: cloudflare
dns:
  zone_name: example.com
`,
			wantErr:     true,
			errContains: "dns_provider",
		},
		{
			name: "bare dns block treated as no DNS configured",
			yaml: `
project_name: test-project
provider: aws
dns:
`,
			wantErr: false,
			validate: func(t *testing.T, cfg *NebariConfig) {
				if cfg.DNS != nil {
					t.Errorf("DNS should be nil for bare dns: block, got %+v", cfg.DNS)
				}
			},
		},
		{
			name: "invalid DNS provider name rejected",
			yaml: `
project_name: test-project
provider: aws
dns:
  notreal:
    zone_name: example.com
`,
			wantErr:     true,
			errContains: "invalid DNS provider",
		},
		{
			name:        "invalid YAML syntax",
			yaml:        "invalid: yaml: content: [",
			wantErr:     true,
			errContains: "failed to parse YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfigBytes([]byte(tt.yaml), mockValidProviders)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseConfigBytes() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ParseConfigBytes() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseConfigBytes() unexpected error: %v", err)
				return
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

var mockValidProviders = ValidProviders{
	ClusterProviders: []string{"aws", "hetzner"},
	DNSProviders:     []string{"cloudflare"},
	GitProviders:     []string{"existing", "local"},
	CertProviders:    []string{"letsencrypt"},
}

func TestParseConfig(t *testing.T) {
	// Test file I/O wrapper - parsing logic is tested in TestParseConfigBytes
	t.Run("reads and parses valid file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		yaml := `
project_name: test-project
provider: aws
`
		if err := os.WriteFile(configFile, []byte(yaml), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		cfg, err := ParseConfig(context.Background(), configFile, mockValidProviders)
		if err != nil {
			t.Fatalf("ParseConfig() error: %v", err)
		}
		if cfg.Provider != "aws" {
			t.Errorf("Provider = %q, want %q", cfg.Provider, "aws")
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := ParseConfig(context.Background(), "/nonexistent/path/config.yaml", mockValidProviders)
		if err == nil {
			t.Error("ParseConfig() expected error for nonexistent file, got nil")
		}
		if !strings.Contains(err.Error(), "failed to read config file") {
			t.Errorf("ParseConfig() error = %v, want error containing 'failed to read config file'", err)
		}
	})

	t.Run("wraps parsing errors with filename", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.yaml")
		// Missing provider field triggers validation error
		if err := os.WriteFile(configFile, []byte("project_name: test"), 0600); err != nil {
			t.Fatalf("failed to write config file: %v", err)
		}

		_, err := ParseConfig(context.Background(), configFile, mockValidProviders)
		if err == nil {
			t.Error("ParseConfig() expected error, got nil")
		}
		if !strings.Contains(err.Error(), configFile) {
			t.Errorf("ParseConfig() error should contain filename, got: %v", err)
		}
	})
}

func TestNebariConfigValidate(t *testing.T) {
	tests := []struct {
		name           string
		config         NebariConfig
		validProviders ValidProviders
		wantErr        bool
		errContains    string
	}{
		{
			name: "valid minimal config",
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
			},
			validProviders: mockValidProviders,
			wantErr:        false,
		},
		{
			name:           "valid config with git_repository",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				GitRepository: &git.Config{
					URL: "git@github.com:org/repo.git",
					Auth: git.AuthConfig{
						SSHKeyEnv: "GIT_SSH_KEY",
					},
				},
			},
			wantErr: false,
		},
		{
			name:           "missing project_name",
			validProviders: mockValidProviders,
			config:         NebariConfig{},
			wantErr:        true,
			errContains:    "project_name field is required",
		},
		{
			name:           "invalid project_name with path traversal",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "../../etc",
			},
			wantErr:     true,
			errContains: "project_name",
		},
		{
			name:           "invalid project_name with dots",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "..sneaky",
			},
			wantErr:     true,
			errContains: "project_name",
		},
		{
			name:           "missing provider",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test",
			},
			wantErr:     true,
			errContains: "provider field is required",
		},
		{
			name:           "any provider name passes config validation",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test",
				Provider:    "any-provider-name",
			},
			wantErr: false,
		},
		{
			name:           "valid config with DNS",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				DNS: &DNSConfig{
					Providers: map[string]any{
						"cloudflare": map[string]any{"zone_name": "example.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name:           "invalid DNS - no provider",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				DNS: &DNSConfig{
					Providers: map[string]any{},
				},
			},
			wantErr:     true,
			errContains: "invalid dns",
		},
		{
			name:           "invalid DNS provider name",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				DNS: &DNSConfig{
					Providers: map[string]any{
						"notreal": map[string]any{"zone_name": "example.com"},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid DNS provider",
		},
		{
			name:           "old format dns_provider rejected",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				ProviderConfig: map[string]any{
					"dns_provider": "cloudflare",
				},
			},
			wantErr:     true,
			errContains: "dns_provider",
		},
		{
			name:           "invalid git_repository",
			validProviders: mockValidProviders,
			config: NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				GitRepository: &git.Config{
					URL: "git@github.com:org/repo.git",
					// missing auth
				},
			},
			wantErr:     true,
			errContains: "invalid git_repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(tt.validProviders)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestDNSConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		dns         DNSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid single provider",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": map[string]any{"zone_name": "example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "no provider configured",
			dns: DNSConfig{
				Providers: map[string]any{},
			},
			wantErr:     true,
			errContains: "no provider is configured",
		},
		{
			name: "nil providers map",
			dns: DNSConfig{
				Providers: nil,
			},
			wantErr:     true,
			errContains: "no provider is configured",
		},
		{
			name: "multiple providers",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": map[string]any{"zone_name": "example.com"},
					"route53":    map[string]any{"hosted_zone_id": "Z123"},
				},
			},
			wantErr:     true,
			errContains: "only one DNS provider",
		},
		{
			name: "invalid provider name",
			dns: DNSConfig{
				Providers: map[string]any{
					"notreal": map[string]any{"zone_name": "example.com"},
				},
			},
			wantErr:     true,
			errContains: "invalid DNS provider",
		},
		{
			name: "scalar provider value rejected",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": "not-a-map",
				},
			},
			wantErr:     true,
			errContains: "must be a mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dns.Validate(mockValidProviders.DNSProviders)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestDNSConfigProviderName(t *testing.T) {
	tests := []struct {
		name string
		dns  DNSConfig
		want string
	}{
		{
			name: "cloudflare provider",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": map[string]any{"zone_name": "example.com"},
				},
			},
			want: "cloudflare",
		},
		{
			name: "empty config",
			dns:  DNSConfig{},
			want: "",
		},
		{
			name: "nil providers",
			dns:  DNSConfig{Providers: nil},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dns.ProviderName()
			if got != tt.want {
				t.Errorf("ProviderName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDNSConfigProviderConfig(t *testing.T) {
	tests := []struct {
		name    string
		dns     DNSConfig
		wantNil bool
		wantKey string
		wantVal string
	}{
		{
			name: "returns provider config map",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": map[string]any{"zone_name": "example.com"},
				},
			},
			wantNil: false,
			wantKey: "zone_name",
			wantVal: "example.com",
		},
		{
			name:    "nil when empty",
			dns:     DNSConfig{},
			wantNil: true,
		},
		{
			name: "nil when value is not a map",
			dns: DNSConfig{
				Providers: map[string]any{
					"cloudflare": "not-a-map",
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dns.ProviderConfig()
			if tt.wantNil {
				if got != nil {
					t.Errorf("ProviderConfig() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ProviderConfig() = nil, want non-nil")
			}
			if got[tt.wantKey] != tt.wantVal {
				t.Errorf("ProviderConfig()[%q] = %v, want %q", tt.wantKey, got[tt.wantKey], tt.wantVal)
			}
		})
	}
}

func TestDNSConfigNilReceiver(t *testing.T) {
	var dns *DNSConfig

	if got := dns.ProviderName(); got != "" {
		t.Errorf("nil.ProviderName() = %q, want empty string", got)
	}
	if got := dns.ProviderConfig(); got != nil {
		t.Errorf("nil.ProviderConfig() = %v, want nil", got)
	}
}

func TestNebariConfigGitRepositoryIntegration(t *testing.T) {
	// Test that the git.Config type works correctly when embedded in NebariConfig
	cfg := &NebariConfig{
		ProjectName: "test",
		Provider:    "aws",
		GitRepository: &git.Config{
			URL:    "git@github.com:org/repo.git",
			Branch: "develop",
			Path:   "clusters/prod",
			Auth: git.AuthConfig{
				SSHKeyEnv: "MY_SSH_KEY",
			},
		},
	}

	// Verify GetBranch works
	if cfg.GitRepository.GetBranch() != "develop" {
		t.Errorf("GetBranch() = %q, want %q", cfg.GitRepository.GetBranch(), "develop")
	}

	// Verify GetArgoCDAuth falls back to Auth
	argoAuth := cfg.GitRepository.GetArgoCDAuth()
	if argoAuth.SSHKeyEnv != "MY_SSH_KEY" {
		t.Errorf("GetArgoCDAuth().SSHKeyEnv = %q, want %q", argoAuth.SSHKeyEnv, "MY_SSH_KEY")
	}

	// Verify NebariConfig.Validate works
	if err := cfg.Validate(mockValidProviders); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestUnmarshalProviderConfig(t *testing.T) {
	tests := []struct {
		name        string
		input       any
		wantErr     bool
		errContains string
	}{
		{
			name:  "valid map config",
			input: map[string]any{"region": "us-west-2"},
		},
		{
			name:        "nil config",
			input:       nil,
			wantErr:     true,
			errContains: "provider config is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target map[string]any
			err := UnmarshalProviderConfig(context.Background(), tt.input, &target)

			if tt.wantErr {
				if err == nil {
					t.Errorf("UnmarshalProviderConfig() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("UnmarshalProviderConfig() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("UnmarshalProviderConfig() unexpected error: %v", err)
			}
		})
	}
}
