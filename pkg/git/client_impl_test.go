package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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
				if err := os.WriteFile(markerPath, []byte("test"), 0600); err != nil {
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
	content, err := os.ReadFile(markerPath) //nolint:gosec // G304: path is constructed from known constant within test tempDir
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

func TestClientCleanupEmptyTempDir(t *testing.T) {
	client := &ClientImpl{
		tempDir: "",
	}

	// Cleanup with empty tempDir should not error
	err := client.Cleanup()
	if err != nil {
		t.Errorf("Cleanup() with empty tempDir should not error, got: %v", err)
	}
}

// setupLocalGitRepo creates a local bare git repository for testing.
// Returns the repo URL (file:// format) and a cleanup function.
func setupLocalGitRepo(t *testing.T, branch string) (string, func()) {
	t.Helper()

	// Create a bare repository
	bareDir, err := os.MkdirTemp("", "test-bare-repo-*")
	if err != nil {
		t.Fatalf("failed to create bare repo dir: %v", err)
	}

	// Initialize bare repo
	_, err = gogit.PlainInit(bareDir, true)
	if err != nil {
		_ = os.RemoveAll(bareDir)
		t.Fatalf("failed to init bare repo: %v", err)
	}

	// Create a working clone to add initial commit
	workDir, err := os.MkdirTemp("", "test-work-repo-*")
	if err != nil {
		_ = os.RemoveAll(bareDir)
		t.Fatalf("failed to create work dir: %v", err)
	}

	workRepo, err := gogit.PlainClone(workDir, false, &gogit.CloneOptions{
		URL: bareDir,
	})
	if err != nil {
		// Clone of empty repo fails, so init instead
		workRepo, err = gogit.PlainInit(workDir, false)
		if err != nil {
			_ = os.RemoveAll(bareDir)
			_ = os.RemoveAll(workDir)
			t.Fatalf("failed to init work repo: %v", err)
		}

		// Add remote
		_, err = workRepo.CreateRemote(&gogitconfig.RemoteConfig{
			Name: "origin",
			URLs: []string{bareDir},
		})
		if err != nil {
			_ = os.RemoveAll(bareDir)
			_ = os.RemoveAll(workDir)
			t.Fatalf("failed to create remote: %v", err)
		}
	}

	// Create initial commit
	worktree, err := workRepo.Worktree()
	if err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to get worktree: %v", err)
	}

	// Create a file
	readmeFile := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readmeFile, []byte("# Test Repo\n"), 0600); err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to write README: %v", err)
	}

	// Stage and commit
	if _, err := worktree.Add("README.md"); err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to stage README: %v", err)
	}

	_, err = worktree.Commit("Initial commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to commit: %v", err)
	}

	// Get HEAD reference after commit
	headRef, err := workRepo.Head()
	if err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to get HEAD: %v", err)
	}

	// Determine target branch name
	targetBranch := branch
	if targetBranch == "" {
		targetBranch = "main"
	}

	// Create the target branch pointing to the commit
	branchRef := plumbing.NewBranchReferenceName(targetBranch)
	ref := plumbing.NewHashReference(branchRef, headRef.Hash())
	if err := workRepo.Storer.SetReference(ref); err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to create branch %s: %v", targetBranch, err)
	}

	// Checkout the target branch
	if err := worktree.Checkout(&gogit.CheckoutOptions{
		Branch: branchRef,
	}); err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to checkout branch %s: %v", targetBranch, err)
	}

	// Push the target branch to bare repo
	refSpec := gogitconfig.RefSpec("+refs/heads/" + targetBranch + ":refs/heads/" + targetBranch)
	err = workRepo.Push(&gogit.PushOptions{
		RefSpecs: []gogitconfig.RefSpec{refSpec},
	})
	if err != nil {
		_ = os.RemoveAll(bareDir)
		_ = os.RemoveAll(workDir)
		t.Fatalf("failed to push to bare repo: %v", err)
	}

	// Cleanup work dir, keep bare
	_ = os.RemoveAll(workDir)

	repoURL := "file://" + bareDir

	cleanup := func() {
		_ = os.RemoveAll(bareDir)
	}

	return repoURL, cleanup
}

// createClientWithLocalRepo creates a ClientImpl configured to use a local file:// repo.
// This bypasses SSH/token auth for local testing.
func createClientWithLocalRepo(t *testing.T, repoURL, branch, path string) *ClientImpl {
	t.Helper()

	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}

	repoPath := tempDir
	workDir := tempDir
	if path != "" {
		workDir = filepath.Join(tempDir, path)
	}

	if branch == "" {
		branch = "main"
	}

	return &ClientImpl{
		cfg: &Config{
			URL:    repoURL,
			Branch: branch,
			Path:   path,
		},
		auth:     nil, // No auth needed for file:// URLs
		tempDir:  tempDir,
		workDir:  workDir,
		repoPath: repoPath,
	}
}

func TestClientInit(t *testing.T) {
	tests := []struct {
		name        string
		branch      string
		path        string
		wantErr     bool
		errContains string
	}{
		{
			name:    "clone repository on main branch",
			branch:  "main",
			path:    "",
			wantErr: false,
		},
		{
			name:    "clone repository with subdirectory path",
			branch:  "main",
			path:    "clusters/test",
			wantErr: false,
		},
		{
			name:    "clone repository on custom branch",
			branch:  "develop",
			path:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoURL, cleanup := setupLocalGitRepo(t, tt.branch)
			defer cleanup()

			client := createClientWithLocalRepo(t, repoURL, tt.branch, tt.path)
			defer func() { _ = client.Cleanup() }()

			err := client.Init(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Errorf("Init() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Init() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Init() unexpected error: %v", err)
				return
			}

			// Verify repo was cloned
			if client.repo == nil {
				t.Errorf("Init() did not set repo")
				return
			}

			// Verify README exists (from initial commit)
			readmePath := filepath.Join(client.repoPath, "README.md")
			if _, err := os.Stat(readmePath); os.IsNotExist(err) {
				t.Errorf("Init() README.md not found in cloned repo")
			}

			// Verify subdirectory created if path specified
			if tt.path != "" {
				if _, err := os.Stat(client.workDir); os.IsNotExist(err) {
					t.Errorf("Init() subdirectory not created: %s", client.workDir)
				}
			}
		})
	}
}

func TestClientInitPullsWhenRepoExists(t *testing.T) {
	repoURL, cleanup := setupLocalGitRepo(t, "main")
	defer cleanup()

	client := createClientWithLocalRepo(t, repoURL, "main", "")
	defer func() { _ = client.Cleanup() }()

	// First init - clones
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("first Init() error: %v", err)
	}

	// Second init - should pull (no error)
	if err := client.Init(context.Background()); err != nil {
		t.Errorf("second Init() (pull) error: %v", err)
	}
}

func TestClientInitInvalidURL(t *testing.T) {
	client := createClientWithLocalRepo(t, "file:///nonexistent/path", "main", "")
	defer func() { _ = client.Cleanup() }()

	err := client.Init(context.Background())
	if err == nil {
		t.Errorf("Init() with invalid URL expected error, got nil")
	}
}

func TestClientCommitAndPush(t *testing.T) {
	tests := []struct {
		name         string
		setupChanges func(t *testing.T, workDir string)
		wantErr      bool
		errContains  string
	}{
		{
			name: "commit and push new file",
			setupChanges: func(t *testing.T, workDir string) {
				filePath := filepath.Join(workDir, "test.txt")
				if err := os.WriteFile(filePath, []byte("test content"), 0600); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
			},
			wantErr: false,
		},
		{
			name: "no changes - should succeed without error",
			setupChanges: func(t *testing.T, workDir string) {
				// No changes
			},
			wantErr: false,
		},
		{
			name: "commit and push modified file",
			setupChanges: func(t *testing.T, workDir string) {
				// Modify README.md that exists from initial commit
				readmePath := filepath.Join(workDir, "README.md")
				if err := os.WriteFile(readmePath, []byte("# Modified\n"), 0600); err != nil {
					t.Fatalf("failed to modify README: %v", err)
				}
			},
			wantErr: false,
		},
		{
			name: "commit and push deleted file",
			setupChanges: func(t *testing.T, workDir string) {
				readmePath := filepath.Join(workDir, "README.md")
				if err := os.Remove(readmePath); err != nil {
					t.Fatalf("failed to delete README: %v", err)
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoURL, cleanup := setupLocalGitRepo(t, "main")
			defer cleanup()

			client := createClientWithLocalRepo(t, repoURL, "main", "")
			defer func() { _ = client.Cleanup() }()

			// Init first
			if err := client.Init(context.Background()); err != nil {
				t.Fatalf("Init() error: %v", err)
			}

			// Setup changes
			tt.setupChanges(t, client.workDir)

			// Commit and push
			err := client.CommitAndPush(context.Background(), "Test commit")

			if tt.wantErr {
				if err == nil {
					t.Errorf("CommitAndPush() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CommitAndPush() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("CommitAndPush() unexpected error: %v", err)
			}
		})
	}
}

func TestClientCommitAndPushNotInitialized(t *testing.T) {
	client := &ClientImpl{
		cfg:     &Config{URL: "file:///test"},
		repo:    nil, // Not initialized
		tempDir: "",
	}

	err := client.CommitAndPush(context.Background(), "Test commit")
	if err == nil {
		t.Errorf("CommitAndPush() on uninitialized repo expected error, got nil")
		return
	}

	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("CommitAndPush() error = %v, want error containing 'not initialized'", err)
	}
}

func TestClientCommitAndPushWithPath(t *testing.T) {
	repoURL, cleanup := setupLocalGitRepo(t, "main")
	defer cleanup()

	client := createClientWithLocalRepo(t, repoURL, "main", "subdir/nested")
	defer func() { _ = client.Cleanup() }()

	// Init
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Create file in subdirectory
	filePath := filepath.Join(client.workDir, "config.yaml")
	if err := os.WriteFile(filePath, []byte("key: value\n"), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Commit and push
	if err := client.CommitAndPush(context.Background(), "Add config"); err != nil {
		t.Errorf("CommitAndPush() error: %v", err)
	}
}

func TestClientValidateAuthWithLocalRepo(t *testing.T) {
	repoURL, cleanup := setupLocalGitRepo(t, "main")
	defer cleanup()

	client := createClientWithLocalRepo(t, repoURL, "main", "")
	defer func() { _ = client.Cleanup() }()

	// ValidateAuth should succeed for local file:// repos (no auth needed)
	err := client.ValidateAuth(context.Background())
	if err != nil {
		t.Errorf("ValidateAuth() for local repo unexpected error: %v", err)
	}
}

func TestClientValidateAuthInvalidURL(t *testing.T) {
	client := createClientWithLocalRepo(t, "file:///nonexistent/path/to/repo", "main", "")
	defer func() { _ = client.Cleanup() }()

	err := client.ValidateAuth(context.Background())
	if err == nil {
		t.Errorf("ValidateAuth() with invalid URL expected error, got nil")
	}
}

func TestClientPull(t *testing.T) {
	repoURL, cleanup := setupLocalGitRepo(t, "main")
	defer cleanup()

	client := createClientWithLocalRepo(t, repoURL, "main", "")
	defer func() { _ = client.Cleanup() }()

	// Init (clones)
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Pull when already up to date should not error
	err := client.pull(context.Background())
	if err != nil {
		t.Errorf("pull() when up to date unexpected error: %v", err)
	}
}
