package existing

import (
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid token auth",
			cfg: Config{
				URL:  "https://github.com/org/repo.git",
				Auth: AuthConfig{Token: &EnvRef{Env: "GIT_TOKEN"}},
			},
			wantErr: false,
		},
		{
			name: "valid ssh auth",
			cfg: Config{
				URL:  "git@github.com:org/repo.git",
				Auth: AuthConfig{SSH: &EnvRef{Env: "GIT_SSH_KEY"}},
			},
			wantErr: false,
		},
		{
			name: "valid with separate argocd auth",
			cfg: Config{
				URL:        "git@github.com:org/repo.git",
				Auth:       AuthConfig{SSH: &EnvRef{Env: "GIT_SSH_KEY"}},
				ArgoCDAuth: &AuthConfig{Token: &EnvRef{Env: "ARGOCD_TOKEN"}},
			},
			wantErr: false,
		},
		{
			name: "missing url",
			cfg: Config{
				Auth: AuthConfig{Token: &EnvRef{Env: "GIT_TOKEN"}},
			},
			wantErr:     true,
			errContains: "url is required",
		},
		{
			name: "file url rejected",
			cfg: Config{
				URL:  "file:///tmp/gitops",
				Auth: AuthConfig{Token: &EnvRef{Env: "GIT_TOKEN"}},
			},
			wantErr:     true,
			errContains: "use the local provider",
		},
		{
			name: "no auth method",
			cfg: Config{
				URL: "git@github.com:org/repo.git",
			},
			wantErr:     true,
			errContains: "one of token or ssh is required",
		},
		{
			name: "both token and ssh",
			cfg: Config{
				URL: "git@github.com:org/repo.git",
				Auth: AuthConfig{
					Token: &EnvRef{Env: "GIT_TOKEN"},
					SSH:   &EnvRef{Env: "GIT_SSH_KEY"},
				},
			},
			wantErr:     true,
			errContains: "only one of token or ssh",
		},
		{
			name: "token env empty",
			cfg: Config{
				URL:  "git@github.com:org/repo.git",
				Auth: AuthConfig{Token: &EnvRef{}},
			},
			wantErr:     true,
			errContains: "token.env is required",
		},
		{
			name: "ssh env empty",
			cfg: Config{
				URL:  "git@github.com:org/repo.git",
				Auth: AuthConfig{SSH: &EnvRef{}},
			},
			wantErr:     true,
			errContains: "ssh.env is required",
		},
		{
			name: "invalid argocd auth",
			cfg: Config{
				URL:        "git@github.com:org/repo.git",
				Auth:       AuthConfig{Token: &EnvRef{Env: "GIT_TOKEN"}},
				ArgoCDAuth: &AuthConfig{},
			},
			wantErr:     true,
			errContains: "argocd_auth",
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

func TestAuthConfigResolve(t *testing.T) {
	t.Run("token resolves from environment", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_TOKEN", "sekret-token")
		auth := AuthConfig{Token: &EnvRef{Env: "NIC_TEST_GIT_TOKEN"}}

		got, err := auth.resolve()
		if err != nil {
			t.Fatalf("resolve() unexpected error: %v", err)
		}
		token, ok := got.(repository.TokenAuth)
		if !ok {
			t.Fatalf("resolve() = %T, want repository.TokenAuth", got)
		}
		if token.Token != "sekret-token" {
			t.Errorf("resolve().Token = %q, want %q", token.Token, "sekret-token")
		}
	})

	t.Run("ssh key resolves from environment", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_SSH_KEY", "-----BEGIN OPENSSH PRIVATE KEY-----")
		auth := AuthConfig{SSH: &EnvRef{Env: "NIC_TEST_GIT_SSH_KEY"}}

		got, err := auth.resolve()
		if err != nil {
			t.Fatalf("resolve() unexpected error: %v", err)
		}
		key, ok := got.(repository.SSHKeyAuth)
		if !ok {
			t.Fatalf("resolve() = %T, want repository.SSHKeyAuth", got)
		}
		if key.Key != "-----BEGIN OPENSSH PRIVATE KEY-----" {
			t.Errorf("resolve().Key = %q, want the key material", key.Key)
		}
	})

	t.Run("empty environment variable is rejected", func(t *testing.T) {
		t.Setenv("NIC_TEST_GIT_TOKEN", "")
		auth := AuthConfig{Token: &EnvRef{Env: "NIC_TEST_GIT_TOKEN"}}

		if _, err := auth.resolve(); err == nil || !strings.Contains(err.Error(), "not set or empty") {
			t.Errorf("resolve() error = %v, want error containing %q", err, "not set or empty")
		}
	})

	t.Run("unset environment variable is rejected", func(t *testing.T) {
		auth := AuthConfig{Token: &EnvRef{Env: "NIC_TEST_ENV_VAR_THAT_IS_NEVER_SET"}}

		if _, err := auth.resolve(); err == nil || !strings.Contains(err.Error(), "not set or empty") {
			t.Errorf("resolve() error = %v, want error containing %q", err, "not set or empty")
		}
	})

	t.Run("no method configured is rejected", func(t *testing.T) {
		auth := AuthConfig{}

		if _, err := auth.resolve(); err == nil || !strings.Contains(err.Error(), "no auth method configured") {
			t.Errorf("resolve() error = %v, want error containing %q", err, "no auth method configured")
		}
	})
}
