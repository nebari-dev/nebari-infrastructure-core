package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

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
			name: "any provider string is accepted by config parser",
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
			name:        "invalid YAML syntax",
			yaml:        "invalid: yaml: content: [",
			wantErr:     true,
			errContains: "failed to parse YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfigBytes([]byte(tt.yaml))

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

		cfg, err := ParseConfig(context.Background(), configFile)
		if err != nil {
			t.Fatalf("ParseConfig() error: %v", err)
		}
		if cfg.Provider != "aws" {
			t.Errorf("Provider = %q, want %q", cfg.Provider, "aws")
		}
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := ParseConfig(context.Background(), "/nonexistent/path/config.yaml")
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

		_, err := ParseConfig(context.Background(), configFile)
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
		name        string
		config      NebariConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid minimal config",
			config: NebariConfig{
				Provider: "aws",
			},
			wantErr: false,
		},
		{
			name: "valid config with git_repository",
			config: NebariConfig{
				Provider: "aws",
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
			name: "missing provider",
			config: NebariConfig{
				ProjectName: "test",
			},
			wantErr:     true,
			errContains: "provider field is required",
		},
		{
			name: "provider name validation deferred to registry",
			config: NebariConfig{
				Provider: "any-provider-name",
			},
			wantErr: false,
		},
		{
			name: "invalid git_repository",
			config: NebariConfig{
				Provider: "aws",
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
			err := tt.config.Validate()

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
	if err := cfg.Validate(); err != nil {
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
