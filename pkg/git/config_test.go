package git

import (
	"os"
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid SSH config",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "valid HTTPS config",
			config: Config{
				URL:    "https://github.com/org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					TokenEnv: "TEST_TOKEN",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with path",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Path:   "clusters/my-cluster",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with argocd_auth",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
				ArgoCDAuth: &AuthConfig{
					SSHKeyEnv: "ARGOCD_SSH_KEY",
				},
			},
			wantErr: false,
		},
		{
			name: "missing URL",
			config: Config{
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
			},
			wantErr:     true,
			errContains: "url is required",
		},
		{
			name: "missing auth",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth:   AuthConfig{},
			},
			wantErr:     true,
			errContains: "ssh_key_env or token_env is required",
		},
		{
			name: "both SSH and token configured",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
					TokenEnv:  "TEST_TOKEN",
				},
			},
			wantErr:     true,
			errContains: "only one of",
		},
		{
			name: "invalid argocd_auth",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
				ArgoCDAuth: &AuthConfig{},
			},
			wantErr:     true,
			errContains: "argocd_auth",
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
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfigGetBranch(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{
			name:   "empty branch returns main",
			config: Config{Branch: ""},
			want:   "main",
		},
		{
			name:   "configured branch returned",
			config: Config{Branch: "develop"},
			want:   "develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetBranch()
			if got != tt.want {
				t.Errorf("GetBranch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetArgoCDAuth(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{
			name: "returns argocd_auth when set",
			config: Config{
				Auth: AuthConfig{
					SSHKeyEnv: "NIC_KEY",
				},
				ArgoCDAuth: &AuthConfig{
					SSHKeyEnv: "ARGOCD_KEY",
				},
			},
			want: "ARGOCD_KEY",
		},
		{
			name: "falls back to auth when argocd_auth not set",
			config: Config{
				Auth: AuthConfig{
					SSHKeyEnv: "NIC_KEY",
				},
			},
			want: "NIC_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.GetArgoCDAuth()
			if got.SSHKeyEnv != tt.want {
				t.Errorf("GetArgoCDAuth().SSHKeyEnv = %v, want %v", got.SSHKeyEnv, tt.want)
			}
		})
	}
}

func TestAuthConfigAuthType(t *testing.T) {
	tests := []struct {
		name string
		auth AuthConfig
		want string
	}{
		{
			name: "SSH auth type",
			auth: AuthConfig{SSHKeyEnv: "KEY"},
			want: "ssh",
		},
		{
			name: "token auth type",
			auth: AuthConfig{TokenEnv: "TOKEN"},
			want: "token",
		},
		{
			name: "no auth type",
			auth: AuthConfig{},
			want: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.auth.AuthType()
			if got != tt.want {
				t.Errorf("AuthType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthConfigGetSSHKey(t *testing.T) {
	tests := []struct {
		name        string
		auth        AuthConfig
		envValue    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "returns SSH key from env",
			auth:     AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
			envValue: "fake-ssh-key-content",
			wantErr:  false,
		},
		{
			name:        "error when env not configured",
			auth:        AuthConfig{},
			wantErr:     true,
			errContains: "not configured",
		},
		{
			name:        "error when env is empty",
			auth:        AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
			envValue:    "",
			wantErr:     true,
			errContains: "not set or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.auth.SSHKeyEnv != "" {
				if tt.envValue != "" {
					if err := os.Setenv(tt.auth.SSHKeyEnv, tt.envValue); err != nil {
						t.Fatalf("failed to set env var: %v", err)
					}
					defer func() { _ = os.Unsetenv(tt.auth.SSHKeyEnv) }()
				} else {
					_ = os.Unsetenv(tt.auth.SSHKeyEnv)
				}
			}

			got, err := tt.auth.GetSSHKey()

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetSSHKey() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("GetSSHKey() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("GetSSHKey() unexpected error: %v", err)
					return
				}
				if got != tt.envValue {
					t.Errorf("GetSSHKey() = %v, want %v", got, tt.envValue)
				}
			}
		})
	}
}

func TestAuthConfigGetToken(t *testing.T) {
	tests := []struct {
		name        string
		auth        AuthConfig
		envValue    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "returns token from env",
			auth:     AuthConfig{TokenEnv: "TEST_TOKEN"},
			envValue: "fake-token",
			wantErr:  false,
		},
		{
			name:        "error when env not configured",
			auth:        AuthConfig{},
			wantErr:     true,
			errContains: "not configured",
		},
		{
			name:        "error when env is empty",
			auth:        AuthConfig{TokenEnv: "TEST_TOKEN"},
			envValue:    "",
			wantErr:     true,
			errContains: "not set or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.auth.TokenEnv != "" {
				if tt.envValue != "" {
					if err := os.Setenv(tt.auth.TokenEnv, tt.envValue); err != nil {
						t.Fatalf("failed to set env var: %v", err)
					}
					defer func() { _ = os.Unsetenv(tt.auth.TokenEnv) }()
				} else {
					_ = os.Unsetenv(tt.auth.TokenEnv)
				}
			}

			got, err := tt.auth.GetToken()

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetToken() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("GetToken() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("GetToken() unexpected error: %v", err)
					return
				}
				if got != tt.envValue {
					t.Errorf("GetToken() = %v, want %v", got, tt.envValue)
				}
			}
		})
	}
}
