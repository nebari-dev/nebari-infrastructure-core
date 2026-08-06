package existing

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// repoConfig wraps a raw existing-provider config map the way ParseConfig
// produces it from a `repository: existing:` block.
func repoConfig(raw map[string]any) *config.RepositoryConfig {
	return &config.RepositoryConfig{Providers: map[string]any{ProviderName: raw}}
}

func TestProviderName(t *testing.T) {
	if got := NewProvider().Name(); got != ProviderName {
		t.Errorf("Name() = %q, want %q", got, ProviderName)
	}
}

func TestProviderValidate(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	t.Run("valid config", func(t *testing.T) {
		cfg := repoConfig(map[string]any{
			"url":  "git@github.com:org/repo.git",
			"auth": map[string]any{"ssh": map[string]any{"env": "GIT_SSH_KEY"}},
		})
		if err := p.Validate(ctx, "test", cfg); err != nil {
			t.Errorf("Validate() unexpected error: %v", err)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		cfg := repoConfig(map[string]any{
			"auth": map[string]any{"ssh": map[string]any{"env": "GIT_SSH_KEY"}},
		})
		if err := p.Validate(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "url is required") {
			t.Errorf("Validate() error = %v, want error containing %q", err, "url is required")
		}
	})

	t.Run("missing provider config", func(t *testing.T) {
		cfg := &config.RepositoryConfig{Providers: map[string]any{ProviderName: nil}}
		if err := p.Validate(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "configuration is required") {
			t.Errorf("Validate() error = %v, want error containing %q", err, "configuration is required")
		}
	})
}

func TestProviderProvision(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	t.Run("resolves token auth and applies defaults", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_TOKEN", "sekret-token")
		cfg := repoConfig(map[string]any{
			"url":  "https://github.com/org/repo.git",
			"auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_GIT_TOKEN"}},
		})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		remote, ok := src.(repository.RemoteSource)
		if !ok {
			t.Fatalf("Provision() = %T, want repository.RemoteSource", src)
		}
		if remote.URL != "https://github.com/org/repo.git" {
			t.Errorf("URL = %q, want the configured url", remote.URL)
		}
		if remote.Branch != "main" {
			t.Errorf("Branch = %q, want default %q", remote.Branch, "main")
		}
		push, ok := remote.PushAuth.(repository.TokenAuth)
		if !ok {
			t.Fatalf("PushAuth = %T, want repository.TokenAuth", remote.PushAuth)
		}
		if push.Token != "sekret-token" {
			t.Errorf("PushAuth.Token = %q, want the resolved env value", push.Token)
		}
	})

	t.Run("respects configured branch and path", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_SSH_KEY", "key-material")
		cfg := repoConfig(map[string]any{
			"url":    "git@github.com:org/repo.git",
			"branch": "develop",
			"path":   "clusters/prod",
			"auth":   map[string]any{"ssh": map[string]any{"env": "NIC_TEST_GIT_SSH_KEY"}},
		})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		remote := src.(repository.RemoteSource)
		if remote.Branch != "develop" {
			t.Errorf("Branch = %q, want %q", remote.Branch, "develop")
		}
		if remote.Path != "clusters/prod" {
			t.Errorf("Path = %q, want %q", remote.Path, "clusters/prod")
		}
		if _, ok := remote.PushAuth.(repository.SSHKeyAuth); !ok {
			t.Errorf("PushAuth = %T, want repository.SSHKeyAuth", remote.PushAuth)
		}
	})

	t.Run("ReadAuth is nil without argocd_auth and falls back to PushAuth", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_TOKEN", "push-token")
		cfg := repoConfig(map[string]any{
			"url":  "https://github.com/org/repo.git",
			"auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_GIT_TOKEN"}},
		})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		remote := src.(repository.RemoteSource)
		if remote.ReadAuth != nil {
			t.Errorf("ReadAuth = %v, want nil when argocd_auth is not configured", remote.ReadAuth)
		}
		argo, ok := remote.ArgoCDAuth().(repository.TokenAuth)
		if !ok {
			t.Fatalf("ArgoCDAuth() = %T, want repository.TokenAuth", remote.ArgoCDAuth())
		}
		if argo.Token != "push-token" {
			t.Errorf("ArgoCDAuth().Token = %q, want the push credential", argo.Token)
		}
	})

	t.Run("resolves separate argocd_auth into ReadAuth", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_SSH_KEY", "push-key")
		t.Setenv("NIC_TEST_ARGOCD_TOKEN", "read-token")
		cfg := repoConfig(map[string]any{
			"url":         "git@github.com:org/repo.git",
			"auth":        map[string]any{"ssh": map[string]any{"env": "NIC_TEST_GIT_SSH_KEY"}},
			"argocd_auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_ARGOCD_TOKEN"}},
		})

		src, err := p.Provision(ctx, "test", cfg)
		if err != nil {
			t.Fatalf("Provision() unexpected error: %v", err)
		}
		remote := src.(repository.RemoteSource)
		read, ok := remote.ReadAuth.(repository.TokenAuth)
		if !ok {
			t.Fatalf("ReadAuth = %T, want repository.TokenAuth", remote.ReadAuth)
		}
		if read.Token != "read-token" {
			t.Errorf("ReadAuth.Token = %q, want the argocd credential", read.Token)
		}
		if remote.ArgoCDAuth() != remote.ReadAuth {
			t.Errorf("ArgoCDAuth() = %v, want ReadAuth", remote.ArgoCDAuth())
		}
	})

	t.Run("unresolvable push credential fails", func(t *testing.T) {
		cfg := repoConfig(map[string]any{
			"url":  "https://github.com/org/repo.git",
			"auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_ENV_VAR_THAT_IS_NEVER_SET"}},
		})

		if _, err := p.Provision(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "resolve push credentials") {
			t.Errorf("Provision() error = %v, want error containing %q", err, "resolve push credentials")
		}
	})

	t.Run("unresolvable argocd credential fails", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_TOKEN", "push-token")
		cfg := repoConfig(map[string]any{
			"url":         "https://github.com/org/repo.git",
			"auth":        map[string]any{"token": map[string]any{"env": "NIC_TEST_GIT_TOKEN"}},
			"argocd_auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_ENV_VAR_THAT_IS_NEVER_SET"}},
		})

		if _, err := p.Provision(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "resolve argocd credentials") {
			t.Errorf("Provision() error = %v, want error containing %q", err, "resolve argocd credentials")
		}
	})

	t.Run("invalid config fails before resolution", func(t *testing.T) {
		cfg := repoConfig(map[string]any{
			"url":  "file:///tmp/gitops",
			"auth": map[string]any{"token": map[string]any{"env": "NIC_TEST_GIT_TOKEN"}},
		})

		if _, err := p.Provision(ctx, "test", cfg); err == nil || !strings.Contains(err.Error(), "use the local provider") {
			t.Errorf("Provision() error = %v, want error containing %q", err, "use the local provider")
		}
	})
}
