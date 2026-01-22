package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSSHKey is a valid Ed25519 SSH key for testing (not used in production).
const testSSHKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVwAAAJDxAYQo8QGE
KAAAAAtzc2gtZWQyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVw
AAAEBB6qz6RjmJ3M8pLqLyS7X8EXC+xf9lxhJwJzPlJ5OiCUcrY7DkIOANDwvgraVHz9Fm
fQntf1jlnmzFCeXmUxpXAAAADHRlc3RAZXhhbXBsZQE=
-----END OPENSSH PRIVATE KEY-----`

func TestNewClient(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		envSetup    map[string]string
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
			envSetup: map[string]string{
				"TEST_SSH_KEY": testSSHKey,
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
			envSetup: map[string]string{
				"TEST_TOKEN": "ghp_testtoken123",
			},
			wantErr: false,
		},
		{
			name: "invalid config - missing URL",
			config: Config{
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
			},
			wantErr:     true,
			errContains: "invalid config",
		},
		{
			name: "missing SSH key env var",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "NONEXISTENT_KEY",
				},
			},
			envSetup:    map[string]string{},
			wantErr:     true,
			errContains: "not set or empty",
		},
		{
			name: "invalid SSH key format",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth: AuthConfig{
					SSHKeyEnv: "TEST_SSH_KEY",
				},
			},
			envSetup: map[string]string{
				"TEST_SSH_KEY": "not-a-valid-ssh-key",
			},
			wantErr:     true,
			errContains: "failed to parse SSH private key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup env vars
			for k, v := range tt.envSetup {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("failed to set env var %s: %v", k, err)
				}
				defer func(key string) {
					_ = os.Unsetenv(key)
				}(k)
			}

			client, err := NewClient(&tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewClient() expected error containing %q, got nil", tt.errContains)
					if client != nil {
						_ = client.Cleanup()
					}
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("NewClient() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("NewClient() unexpected error: %v", err)
					return
				}
				if client == nil {
					t.Errorf("NewClient() returned nil client")
					return
				}

				// Verify temp dir was created
				if client.tempDir == "" {
					t.Errorf("NewClient() tempDir is empty")
				}
				if _, err := os.Stat(client.tempDir); os.IsNotExist(err) {
					t.Errorf("NewClient() tempDir does not exist: %s", client.tempDir)
				}

				// Cleanup
				if err := client.Cleanup(); err != nil {
					t.Errorf("Cleanup() error: %v", err)
				}

				// Verify cleanup worked
				if _, err := os.Stat(client.tempDir); !os.IsNotExist(err) {
					t.Errorf("Cleanup() did not remove tempDir: %s", client.tempDir)
				}
			}
		})
	}
}

func TestClientWorkDir(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		wantContains string
	}{
		{
			name: "without path",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
			},
			wantContains: "nic-gitops",
		},
		{
			name: "with path",
			config: Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Path:   "clusters/my-cluster",
				Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
			},
			wantContains: "clusters/my-cluster",
		},
	}

	if err := os.Setenv("TEST_SSH_KEY", testSSHKey); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SSH_KEY") }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(&tt.config)
			if err != nil {
				t.Fatalf("NewClient() error: %v", err)
			}
			defer func() { _ = client.Cleanup() }()

			workDir := client.WorkDir()
			if !strings.Contains(workDir, tt.wantContains) {
				t.Errorf("WorkDir() = %v, want to contain %v", workDir, tt.wantContains)
			}
		})
	}
}

func TestClientIsBootstrapped(t *testing.T) {
	if err := os.Setenv("TEST_SSH_KEY", testSSHKey); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SSH_KEY") }()

	tests := []struct {
		name        string
		setupMarker bool
		want        bool
		wantErr     bool
	}{
		{
			name:        "not bootstrapped",
			setupMarker: false,
			want:        false,
			wantErr:     false,
		},
		{
			name:        "bootstrapped",
			setupMarker: true,
			want:        true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(&Config{
				URL:    "git@github.com:org/repo.git",
				Branch: "main",
				Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
			})
			if err != nil {
				t.Fatalf("NewClient() error: %v", err)
			}
			defer func() { _ = client.Cleanup() }()

			if tt.setupMarker {
				markerPath := filepath.Join(client.WorkDir(), bootstrapMarkerFile)
				if err := os.WriteFile(markerPath, []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create marker file: %v", err)
				}
			}

			got, err := client.IsBootstrapped(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Errorf("IsBootstrapped() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("IsBootstrapped() unexpected error: %v", err)
					return
				}
				if got != tt.want {
					t.Errorf("IsBootstrapped() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestClientWriteBootstrapMarker(t *testing.T) {
	if err := os.Setenv("TEST_SSH_KEY", testSSHKey); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SSH_KEY") }()

	client, err := NewClient(&Config{
		URL:    "git@github.com:org/repo.git",
		Branch: "main",
		Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = client.Cleanup() }()

	// Write marker
	err = client.WriteBootstrapMarker(context.Background())
	if err != nil {
		t.Fatalf("WriteBootstrapMarker() error: %v", err)
	}

	// Verify marker exists
	markerPath := filepath.Join(client.WorkDir(), bootstrapMarkerFile)
	content, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}

	if !strings.Contains(string(content), "bootstrapped_at") {
		t.Errorf("marker file content = %q, want to contain 'bootstrapped_at'", content)
	}

	// Verify IsBootstrapped returns true
	bootstrapped, err := client.IsBootstrapped(context.Background())
	if err != nil {
		t.Fatalf("IsBootstrapped() error: %v", err)
	}
	if !bootstrapped {
		t.Errorf("IsBootstrapped() = false, want true after WriteBootstrapMarker")
	}
}

func TestNewClientCleansUpOnAuthError(t *testing.T) {
	// Count temp directories before
	tmpDir := os.TempDir()
	entriesBefore, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	countBefore := countNicGitopsDirs(entriesBefore)

	// Set an invalid SSH key that will fail parsing
	if err := os.Setenv("TEST_SSH_KEY", "invalid-key"); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SSH_KEY") }()

	// This should fail in buildAuth after tempDir is created
	_, err = NewClient(&Config{
		URL:    "git@github.com:org/repo.git",
		Branch: "main",
		Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
	})
	if err == nil {
		t.Fatal("NewClient() expected error for invalid SSH key, got nil")
	}

	// Count temp directories after - should be the same (no leak)
	entriesAfter, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	countAfter := countNicGitopsDirs(entriesAfter)

	if countAfter > countBefore {
		t.Errorf("temp directory leak detected: had %d nic-gitops dirs before, %d after",
			countBefore, countAfter)
	}
}

func countNicGitopsDirs(entries []os.DirEntry) int {
	count := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "nic-gitops-") {
			count++
		}
	}
	return count
}

func TestClientCleanup(t *testing.T) {
	if err := os.Setenv("TEST_SSH_KEY", testSSHKey); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() { _ = os.Unsetenv("TEST_SSH_KEY") }()

	client, err := NewClient(&Config{
		URL:    "git@github.com:org/repo.git",
		Branch: "main",
		Auth:   AuthConfig{SSHKeyEnv: "TEST_SSH_KEY"},
	})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	tempDir := client.tempDir

	// Verify temp dir exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Fatalf("tempDir does not exist before cleanup: %s", tempDir)
	}

	// Cleanup
	if err := client.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}

	// Verify temp dir is removed
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("tempDir still exists after cleanup: %s", tempDir)
	}

	// Cleanup should be idempotent
	if err := client.Cleanup(); err != nil {
		t.Errorf("second Cleanup() error: %v", err)
	}
}
