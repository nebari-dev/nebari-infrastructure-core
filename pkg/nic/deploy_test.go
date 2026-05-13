package nic

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

func TestGenerateSecurePassword(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "deterministic output with known bytes",
			input:    bytes.Repeat([]byte{0x00}, 32),
			expected: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
		{
			name:     "deterministic output with different bytes",
			input:    bytes.Repeat([]byte{0xFF}, 32),
			expected: "__________________________________________8",
		},
		{
			name:     "mixed bytes produce consistent output",
			input:    []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20},
			expected: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.input)
			result := generateSecurePassword(reader)

			if result != tt.expected {
				t.Errorf("generateSecurePassword() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGenerateSecurePasswordLength(t *testing.T) {
	input := bytes.Repeat([]byte{0x42}, 32)
	reader := bytes.NewReader(input)
	result := generateSecurePassword(reader)

	if len(result) != 43 {
		t.Errorf("generateSecurePassword() length = %d, want 43", len(result))
	}
}

func TestGenerateSecurePasswordFallback(t *testing.T) {
	// Empty reader will cause Read to fail
	reader := bytes.NewReader([]byte{})
	result := generateSecurePassword(reader)

	// Should return fallback format "nebari-<timestamp>"
	if len(result) < 7 || result[:7] != "nebari-" {
		t.Errorf("generateSecurePassword() fallback = %q, want prefix 'nebari-'", result)
	}
}

func TestScrubbedConfig(t *testing.T) {
	t.Run("zeros Auth and nils ArgoCDAuth in GitRepository", func(t *testing.T) {
		cfg := &config.NebariConfig{
			ProjectName: "test",
			GitRepository: &git.Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Path:   "clusters/prod",
				Auth: git.AuthConfig{
					SSHKeyEnv: "MY_SSH_KEY",
					TokenEnv:  "MY_TOKEN",
				},
				ArgoCDAuth: &git.AuthConfig{
					TokenEnv: "ARGOCD_TOKEN",
				},
			},
		}

		scrubbed := scrubbedConfig(cfg)

		if scrubbed.GitRepository.Auth != (git.AuthConfig{}) {
			t.Errorf("Auth should be zeroed, got %+v", scrubbed.GitRepository.Auth)
		}
		if scrubbed.GitRepository.ArgoCDAuth != nil {
			t.Errorf("ArgoCDAuth should be nil, got %+v", scrubbed.GitRepository.ArgoCDAuth)
		}
		if scrubbed.GitRepository.URL != "git@github.com:org/repo.git" {
			t.Errorf("URL altered: %q", scrubbed.GitRepository.URL)
		}
		if scrubbed.GitRepository.Branch != "main" {
			t.Errorf("Branch altered: %q", scrubbed.GitRepository.Branch)
		}
		if scrubbed.GitRepository.Path != "clusters/prod" {
			t.Errorf("Path altered: %q", scrubbed.GitRepository.Path)
		}
		if scrubbed.ProjectName != "test" {
			t.Errorf("ProjectName altered: %q", scrubbed.ProjectName)
		}
	})

	t.Run("does not mutate the input cfg", func(t *testing.T) {
		cfg := &config.NebariConfig{
			GitRepository: &git.Config{
				URL: "git@github.com:org/repo.git",
				Auth: git.AuthConfig{
					SSHKeyEnv: "MY_SSH_KEY",
					TokenEnv:  "MY_TOKEN",
				},
				ArgoCDAuth: &git.AuthConfig{TokenEnv: "ARGOCD_TOKEN"},
			},
		}

		_ = scrubbedConfig(cfg)

		if cfg.GitRepository.Auth.SSHKeyEnv != "MY_SSH_KEY" {
			t.Errorf("input cfg.Auth.SSHKeyEnv mutated: %q", cfg.GitRepository.Auth.SSHKeyEnv)
		}
		if cfg.GitRepository.Auth.TokenEnv != "MY_TOKEN" {
			t.Errorf("input cfg.Auth.TokenEnv mutated: %q", cfg.GitRepository.Auth.TokenEnv)
		}
		if cfg.GitRepository.ArgoCDAuth == nil || cfg.GitRepository.ArgoCDAuth.TokenEnv != "ARGOCD_TOKEN" {
			t.Errorf("input cfg.ArgoCDAuth mutated")
		}
	})

	t.Run("handles nil GitRepository", func(t *testing.T) {
		cfg := &config.NebariConfig{
			ProjectName:   "test",
			GitRepository: nil,
		}

		scrubbed := scrubbedConfig(cfg)

		if scrubbed.GitRepository != nil {
			t.Errorf("GitRepository should remain nil, got %+v", scrubbed.GitRepository)
		}
		if scrubbed.ProjectName != "test" {
			t.Errorf("ProjectName altered: %q", scrubbed.ProjectName)
		}
	})

	t.Run("marshalled output excludes sensitive strings", func(t *testing.T) {
		cfg := &config.NebariConfig{
			ProjectName: "test",
			GitRepository: &git.Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: git.AuthConfig{
					SSHKeyEnv: "MY_SSH_KEY",
					TokenEnv:  "MY_TOKEN",
				},
				ArgoCDAuth: &git.AuthConfig{
					TokenEnv: "ARGOCD_TOKEN",
				},
			},
		}

		out, err := yaml.Marshal(scrubbedConfig(cfg))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)

		// Sensitive env-var names and the ArgoCDAuth block must not appear.
		for _, forbidden := range []string{"MY_SSH_KEY", "MY_TOKEN", "ARGOCD_TOKEN", "argocd_auth"} {
			if strings.Contains(s, forbidden) {
				t.Errorf("scrubbed output should not contain %q:\n%s", forbidden, s)
			}
		}
		// Non-sensitive fields preserved.
		for _, kept := range []string{"git@github.com:org/repo.git", "branch: main"} {
			if !strings.Contains(s, kept) {
				t.Errorf("scrubbed output should contain %q:\n%s", kept, s)
			}
		}
	})
}
