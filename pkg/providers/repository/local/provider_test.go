package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// repoConfig wraps a raw local-provider config map the way ParseConfig
// produces it from a `repository: local:` block.
func repoConfig(raw map[string]any) *config.RepositoryConfig {
	return &config.RepositoryConfig{Providers: map[string]any{ProviderName: raw}}
}

func TestProviderName(t *testing.T) {
	if got := NewProvider().Name(); got != ProviderName {
		t.Errorf("Name() = %q, want %q", got, ProviderName)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantErr     bool
		errContains string
	}{
		{
			name:    "empty config is valid",
			cfg:     Config{},
			wantErr: false,
		},
		{
			name:    "absolute path is valid",
			cfg:     Config{Path: "/tmp/nebari-gitops"},
			wantErr: false,
		},
		{
			name:        "relative path is rejected",
			cfg:         Config{Path: "relative/gitops"},
			wantErr:     true,
			errContains: "must be an absolute directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()

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

func TestProviderProvision(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	t.Run("creates configured directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "gitops")
		cfg := repoConfig(map[string]any{"path": dir})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		local, ok := src.(repository.LocalSource)
		if !ok {
			t.Fatalf("Provision() = %T, want repository.LocalSource", src)
		}
		if local.Dir != dir {
			t.Errorf("Dir = %q, want %q", local.Dir, dir)
		}
		if local.Branch != "main" {
			t.Errorf("Branch = %q, want default %q", local.Branch, "main")
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("configured directory was not created: %v", err)
		}
		if !info.IsDir() {
			t.Errorf("%s exists but is not a directory", dir)
		}
	})

	t.Run("defaults to per-project directory", func(t *testing.T) {
		const projectName = "nic-local-provider-test"
		want := config.DefaultLocalRepositoryPath(projectName)
		t.Cleanup(func() { _ = os.RemoveAll(want) })

		src, err := p.Provision(ctx, projectName, repoConfig(map[string]any{}))
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		local := src.(repository.LocalSource)
		if local.Dir != want {
			t.Errorf("Dir = %q, want default %q", local.Dir, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("default directory was not created: %v", err)
		}
	})

	t.Run("nil provider config uses defaults", func(t *testing.T) {
		const projectName = "nic-local-provider-nil-test"
		want := config.DefaultLocalRepositoryPath(projectName)
		t.Cleanup(func() { _ = os.RemoveAll(want) })

		cfg := &config.RepositoryConfig{Providers: map[string]any{ProviderName: nil}}
		src, err := p.Provision(ctx, projectName, cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		local := src.(repository.LocalSource)
		if local.Dir != want {
			t.Errorf("Dir = %q, want default %q", local.Dir, want)
		}
	})

	t.Run("respects configured branch", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "gitops")
		cfg := repoConfig(map[string]any{"path": dir, "branch": "develop"})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		if got := src.GetBranch(); got != "develop" {
			t.Errorf("GetBranch() = %q, want %q", got, "develop")
		}
	})

	t.Run("relative path fails validation", func(t *testing.T) {
		cfg := repoConfig(map[string]any{"path": "relative/gitops"})

		if _, err := p.Provision(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "must be an absolute directory") {
			t.Errorf("Provision() error = %v, want error containing %q", err, "must be an absolute directory")
		}
	})
}

func TestProviderValidate(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	t.Run("empty config is valid", func(t *testing.T) {
		if err := p.Validate(ctx, "test", repoConfig(map[string]any{})); err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
	})

	t.Run("relative path is rejected", func(t *testing.T) {
		cfg := repoConfig(map[string]any{"path": "relative/gitops"})
		if err := p.Validate(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "must be an absolute directory") {
			t.Errorf("Validate() error = %v, want error containing %q", err, "must be an absolute directory")
		}
	})
}
