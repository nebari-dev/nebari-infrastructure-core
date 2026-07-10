package nic

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestBootstrapGitOpsNormalizesExistingLocalRepository(t *testing.T) {
	repoPath := t.TempDir()
	gitConfig := &git.Config{
		URL:    "file://" + repoPath,
		Branch: git.DefaultBranch,
	}

	gitClient, err := git.NewClient(gitConfig)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if err := gitClient.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	markerPath := filepath.Join(repoPath, ".bootstrapped")
	if err := os.WriteFile(markerPath, []byte("bootstrapped_at: test\n"), 0o600); err != nil {
		t.Fatalf("write bootstrap marker: %v", err)
	}
	manifestPath := filepath.Join(repoPath, "application.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: Application\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.Chmod(repoPath, 0o700); err != nil { //nolint:gosec // Deliberately restrictive setup for permission normalization.
		t.Fatalf("chmod repository: %v", err)
	}

	cfg := &config.NebariConfig{
		ProjectName:   "test",
		GitRepository: gitConfig,
	}
	client := &Client{}
	if err := client.bootstrapGitOps(context.Background(), cfg, gitConfig, false, cluster.InfraSettings{}, ""); err != nil {
		t.Fatalf("bootstrapGitOps() error: %v", err)
	}

	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %v, want %v", path, got, want)
		}
	}
	assertMode(repoPath, git.GitOpsDirMode)
	assertMode(markerPath, git.GitOpsFileMode)
	assertMode(manifestPath, git.GitOpsFileMode)
}

func TestWriteConfigToRepoUsesGitOpsPermissions(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "gitops")
	cfg := &config.NebariConfig{ProjectName: "test"}
	gitConfig := &git.Config{
		URL:    "file:///tmp/test-gitops",
		Branch: git.DefaultBranch,
	}

	client := &Client{}
	if err := client.writeConfigToRepo(context.Background(), cfg, gitConfig, workDir, ""); err != nil {
		t.Fatalf("writeConfigToRepo() error: %v", err)
	}

	dirInfo, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("stat workDir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != git.GitOpsDirMode {
		t.Errorf("workDir mode = %v, want %v", got, git.GitOpsDirMode)
	}

	configInfo, err := os.Stat(filepath.Join(workDir, "nic-config.yaml"))
	if err != nil {
		t.Fatalf("stat nic-config.yaml: %v", err)
	}
	if got := configInfo.Mode().Perm(); got != git.GitOpsFileMode {
		t.Errorf("nic-config.yaml mode = %v, want %v", got, git.GitOpsFileMode)
	}
}

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
			result, err := generateSecurePassword(reader)
			if err != nil {
				t.Fatalf("generateSecurePassword() unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("generateSecurePassword() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGenerateSecurePasswordLength(t *testing.T) {
	input := bytes.Repeat([]byte{0x42}, 32)
	reader := bytes.NewReader(input)
	result, err := generateSecurePassword(reader)
	if err != nil {
		t.Fatalf("generateSecurePassword() unexpected error: %v", err)
	}

	if len(result) != 43 {
		t.Errorf("generateSecurePassword() length = %d, want 43", len(result))
	}
}

// TestGenerateSecurePasswordError verifies that a failing reader yields an
// error rather than a weak, predictable fallback. These strings end up as
// admin credentials on the deployed cluster, so silently substituting a
// timestamp-based string would be a serious security regression.
func TestGenerateSecurePasswordError(t *testing.T) {
	t.Run("empty reader returns error", func(t *testing.T) {
		result, err := generateSecurePassword(bytes.NewReader([]byte{}))
		if err == nil {
			t.Fatalf("generateSecurePassword() expected error, got result = %q", result)
		}
		if result != "" {
			t.Errorf("generateSecurePassword() on error should return empty string, got %q", result)
		}
	})

	t.Run("short reader returns error", func(t *testing.T) {
		// 16 bytes is half of what we need - io.ReadFull must fail.
		result, err := generateSecurePassword(bytes.NewReader(bytes.Repeat([]byte{0x00}, 16)))
		if err == nil {
			t.Fatalf("generateSecurePassword() expected error for short read, got result = %q", result)
		}
		if result != "" {
			t.Errorf("generateSecurePassword() on error should return empty string, got %q", result)
		}
	})
}

func TestScrubbedConfig(t *testing.T) {
	t.Run("zeros Auth and nils ArgoCDAuth in GitRepository", func(t *testing.T) {
		cfg := &config.NebariConfig{ProjectName: "test"}
		gitConfig := &git.Config{
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
		}

		scrubbed := scrubbedConfig(cfg, gitConfig, "")

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

	t.Run("does not mutate the input cfg or gitConfig", func(t *testing.T) {
		cfg := &config.NebariConfig{}
		gitConfig := &git.Config{
			URL: "git@github.com:org/repo.git",
			Auth: git.AuthConfig{
				SSHKeyEnv: "MY_SSH_KEY",
				TokenEnv:  "MY_TOKEN",
			},
			ArgoCDAuth: &git.AuthConfig{TokenEnv: "ARGOCD_TOKEN"},
		}

		_ = scrubbedConfig(cfg, gitConfig, "")

		if cfg.GitRepository != nil {
			t.Errorf("input cfg.GitRepository should remain nil, got %+v", cfg.GitRepository)
		}
		if gitConfig.Auth.SSHKeyEnv != "MY_SSH_KEY" {
			t.Errorf("input gitConfig.Auth.SSHKeyEnv mutated: %q", gitConfig.Auth.SSHKeyEnv)
		}
		if gitConfig.Auth.TokenEnv != "MY_TOKEN" {
			t.Errorf("input gitConfig.Auth.TokenEnv mutated: %q", gitConfig.Auth.TokenEnv)
		}
		if gitConfig.ArgoCDAuth == nil || gitConfig.ArgoCDAuth.TokenEnv != "ARGOCD_TOKEN" {
			t.Errorf("input gitConfig.ArgoCDAuth mutated")
		}
	})

	t.Run("handles nil gitConfig", func(t *testing.T) {
		cfg := &config.NebariConfig{ProjectName: "test"}

		scrubbed := scrubbedConfig(cfg, nil, "")

		if scrubbed.GitRepository != nil {
			t.Errorf("GitRepository should be nil, got %+v", scrubbed.GitRepository)
		}
		if scrubbed.ProjectName != "test" {
			t.Errorf("ProjectName altered: %q", scrubbed.ProjectName)
		}
	})

	t.Run("ignores cfg.GitRepository in favor of gitConfig argument", func(t *testing.T) {
		cfg := &config.NebariConfig{
			GitRepository: &git.Config{URL: "should-be-overridden"},
		}
		gitConfig := &git.Config{URL: "effective"}

		scrubbed := scrubbedConfig(cfg, gitConfig, "")

		if scrubbed.GitRepository == nil || scrubbed.GitRepository.URL != "effective" {
			t.Errorf("scrubbed.GitRepository.URL = %+v, want %q", scrubbed.GitRepository, "effective")
		}
	})

	t.Run("rewrites path-based trust_bundle to resolved inline", func(t *testing.T) {
		const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		cfg := &config.NebariConfig{
			TrustBundle: &config.TrustBundleConfig{Path: "/home/operator/org-ca.pem"},
		}

		scrubbed := scrubbedConfig(cfg, nil, pem)

		if scrubbed.TrustBundle == nil {
			t.Fatal("TrustBundle should be preserved as inline, got nil")
		}
		if scrubbed.TrustBundle.Path != "" {
			t.Errorf("machine-local path should be dropped, got %q", scrubbed.TrustBundle.Path)
		}
		if scrubbed.TrustBundle.Inline != pem {
			t.Errorf("Inline = %q, want resolved PEM", scrubbed.TrustBundle.Inline)
		}
		// Input must not be mutated.
		if cfg.TrustBundle.Path != "/home/operator/org-ca.pem" || cfg.TrustBundle.Inline != "" {
			t.Errorf("input cfg.TrustBundle mutated: %+v", cfg.TrustBundle)
		}
	})

	t.Run("strips trust_bundle that resolved to empty", func(t *testing.T) {
		cfg := &config.NebariConfig{
			TrustBundle: &config.TrustBundleConfig{Path: "   "},
		}

		scrubbed := scrubbedConfig(cfg, nil, "")

		if scrubbed.TrustBundle != nil {
			t.Errorf("TrustBundle should be stripped when resolved PEM is empty, got %+v", scrubbed.TrustBundle)
		}
	})

	t.Run("leaves unset trust_bundle nil", func(t *testing.T) {
		scrubbed := scrubbedConfig(&config.NebariConfig{}, nil, "")

		if scrubbed.TrustBundle != nil {
			t.Errorf("TrustBundle should stay nil, got %+v", scrubbed.TrustBundle)
		}
	})

	t.Run("marshalled output excludes machine-local trust_bundle path", func(t *testing.T) {
		const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		cfg := &config.NebariConfig{
			ProjectName: "test",
			TrustBundle: &config.TrustBundleConfig{Path: "/home/operator/org-ca.pem"},
		}

		out, err := yaml.Marshal(scrubbedConfig(cfg, nil, pem))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(out)

		if strings.Contains(s, "/home/operator/org-ca.pem") {
			t.Errorf("scrubbed output should not contain the local path:\n%s", s)
		}
		if !strings.Contains(s, "-----BEGIN CERTIFICATE-----") {
			t.Errorf("scrubbed output should contain the resolved inline PEM:\n%s", s)
		}
	})

	t.Run("marshalled output excludes sensitive strings", func(t *testing.T) {
		cfg := &config.NebariConfig{ProjectName: "test"}
		gitConfig := &git.Config{
			URL:    "git@github.com:org/repo.git",
			Branch: "main",
			Auth: git.AuthConfig{
				SSHKeyEnv: "MY_SSH_KEY",
				TokenEnv:  "MY_TOKEN",
			},
			ArgoCDAuth: &git.AuthConfig{
				TokenEnv: "ARGOCD_TOKEN",
			},
		}

		out, err := yaml.Marshal(scrubbedConfig(cfg, gitConfig, ""))
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
