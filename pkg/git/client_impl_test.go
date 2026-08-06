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

	// The marker is a working-tree file, so its on-disk mode is masked by the
	// ambient umask and is not an invariant the code guarantees (ArgoCD reads
	// via .git, not the working tree); asserting an exact mode here would only
	// pass under the default umask. Existence and content are what matter.

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

	// Return the bare dir path directly (not file:// URL).
	// file:// URLs are now treated as local development paths by IsLocalPath(),
	// but these tests simulate remote repos that need clone/push behavior.
	cleanup := func() {
		_ = os.RemoveAll(bareDir)
	}

	return bareDir, cleanup
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
	client := createClientWithLocalRepo(t, "/nonexistent/path/to/repo", "main", "")
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

func TestClientCommitLocalOnFreshRepo(t *testing.T) {
	// Exercise CommitAndPush on a freshly PlainInit'd local repo with zero commits.
	// This is the path taken when the auto-generated local GitOps
	// directory is brand new.
	tmpDir, err := os.MkdirTemp("", "test-fresh-local-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cfg := &Config{
		URL:    "file://" + tmpDir,
		Branch: "main",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	// Init — this calls initLocalPath which does PlainInit but no initial commit
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Write a file into the working directory
	filePath := filepath.Join(client.WorkDir(), "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// CommitAndPush should succeed even with zero prior commits
	if err := client.CommitAndPush(context.Background(), "First commit on fresh repo"); err != nil {
		t.Errorf("CommitAndPush() on fresh local repo error: %v", err)
	}

	// Verify the commit was created
	head, err := client.repo.Head()
	if err != nil {
		t.Fatalf("Head() error after commit: %v", err)
	}
	commit, err := client.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject() error: %v", err)
	}
	if commit.Message != "First commit on fresh repo" {
		t.Errorf("commit message = %q, want %q", commit.Message, "First commit on fresh repo")
	}
}

// TestClientCommitAndPushUpgradesLocalPermissions verifies that CommitAndPush
// on a local repo makes the repo root and .git (including
// .git/objects) group/other readable. go-git writes loose objects via a
// temp-file-then-rename that is always created at 0600 regardless of the
// process umask, so without this fix a non-root reader (e.g. ArgoCD's
// repo-server) can't read a fresh commit. It also verifies the upgrade leaves
// the working tree untouched — argo reads .git and rebuilds its own checkout,
// so a 0600 working-tree file keeps its mode — and that the upgrade is
// additive: an executable file under .git (which the walk does touch) gains
// group/other read while keeping its +x, so a hand-picked executable bit can't
// be stripped, and no mode-only diff is staged.
func TestClientCommitAndPushUpgradesLocalPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	client, err := NewClient(&Config{URL: "file://" + tmpDir, Branch: "main"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	filePath := filepath.Join(client.WorkDir(), "test.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	scriptPath := filepath.Join(client.WorkDir(), "script.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // Deliberately executable to prove the working tree is left untouched.
		t.Fatalf("failed to write script: %v", err)
	}
	// Chmod explicitly so the 0o755 assertion below is umask-independent.
	if err := os.Chmod(scriptPath, 0o755); err != nil { //nolint:gosec // Deliberately executable to prove the working tree is left untouched.
		t.Fatalf("failed to chmod script: %v", err)
	}
	// An executable file directly under .git IS walked (unlike the working-tree
	// script above), so it proves the OR adds the missing group/other read bits
	// without stripping the executable bit: 0o700 | 0o044 = 0o744.
	gitExecPath := filepath.Join(client.WorkDir(), ".git", "executable-serving-file")
	if err := os.WriteFile(gitExecPath, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // Deliberately executable to prove +x survives the additive OR.
		t.Fatalf("failed to write .git executable: %v", err)
	}

	if err := client.CommitAndPush(context.Background(), "commit"); err != nil {
		t.Fatalf("CommitAndPush() error: %v", err)
	}

	// The tracked working-tree script is never walked, so this only shows the
	// working tree is left untouched — not that +x survives the OR.
	assertPathMode(t, scriptPath, 0o755)
	// This is the real proof that the additive OR preserves +x: a walked .git
	// file gains group/other read while keeping its executable bit.
	assertPathMode(t, gitExecPath, 0o744)

	// Working-tree files are never touched: the upgrade only repairs the repo
	// root dir and .git (argo reads .git and rebuilds its own checkout). The
	// 0600 file must be left exactly as-is.
	assertPathMode(t, filePath, 0o600)

	// The repo root itself must be group/other-traversable so argo can descend
	// into it to reach .git.
	rootInfo, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatalf("stat repo root: %v", err)
	}
	if mode := rootInfo.Mode().Perm(); mode&0o055 != 0o055 {
		t.Errorf("repo root mode = %v, want group+other read+execute set", mode)
	}

	objectsDir := filepath.Join(tmpDir, ".git", "objects")
	sawObject := false
	if err := filepath.WalkDir(objectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sawObject = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		if mode := info.Mode().Perm(); mode&0o044 != 0o044 {
			t.Errorf("object %s mode = %v, want group+other read set", path, mode)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk objects dir: %v", err)
	}
	if !sawObject {
		t.Fatal("expected at least one loose object under .git/objects")
	}

	worktree, err := client.repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree() error: %v", err)
	}
	status, err := worktree.Status()
	if err != nil {
		t.Fatalf("Status() error: %v", err)
	}
	if !status.IsClean() {
		t.Errorf("worktree status after permission upgrade = %v, want clean (no mode-only diff)", status)
	}
}

// TestClientCommitAndPushUpgradesUserSuppliedLocalRepo verifies that NIC can
// open a pre-existing user repository and make the new Git objects from its
// own commit group/other-readable without changing existing tracked files or
// introducing a mode-only worktree diff.
func TestClientCommitAndPushUpgradesUserSuppliedLocalRepo(t *testing.T) {
	tmpDir := t.TempDir()

	existingRepo, err := gogit.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("PlainInit() existing repository error: %v", err)
	}
	if err := existingRepo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD,
		plumbing.NewBranchReferenceName(DefaultBranch),
	)); err != nil {
		t.Fatalf("set existing repository HEAD: %v", err)
	}
	existingWorktree, err := existingRepo.Worktree()
	if err != nil {
		t.Fatalf("existing Worktree() error: %v", err)
	}
	existingScriptPath := filepath.Join(tmpDir, "existing-script.sh")
	if err := os.WriteFile(existingScriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // Executable mode is the behavior under test.
		t.Fatalf("write existing script: %v", err)
	}
	// Chmod explicitly so the 0o755 assertion below is umask-independent.
	if err := os.Chmod(existingScriptPath, 0o755); err != nil { //nolint:gosec // Executable mode is the behavior under test.
		t.Fatalf("chmod existing script: %v", err)
	}
	if _, err := existingWorktree.Add("existing-script.sh"); err != nil {
		t.Fatalf("stage existing script: %v", err)
	}
	if _, err := existingWorktree.Commit("existing user commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Existing User",
			Email: "existing@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		t.Fatalf("commit existing repository: %v", err)
	}

	client, err := NewClient(&Config{URL: "file://" + tmpDir, Branch: "main"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	filePath := filepath.Join(client.WorkDir(), "nic-generated.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := client.CommitAndPush(context.Background(), "commit"); err != nil {
		t.Fatalf("CommitAndPush() error: %v", err)
	}

	// Existing and newly written working-tree files are never chmodded.
	assertPathMode(t, existingScriptPath, 0o755)
	assertPathMode(t, filePath, 0o600)

	objectsDir := filepath.Join(tmpDir, ".git", "objects")
	sawObject := false
	if err := filepath.WalkDir(objectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		sawObject = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		if mode := info.Mode().Perm(); mode&0o044 != 0o044 {
			t.Errorf("object %s mode = %v, want group+other read set", path, mode)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk objects dir: %v", err)
	}
	if !sawObject {
		t.Fatal("expected at least one loose object under .git/objects")
	}

	status, err := existingWorktree.Status()
	if err != nil {
		t.Fatalf("existing worktree Status() error: %v", err)
	}
	if !status.IsClean() {
		t.Errorf("existing worktree status after NIC commit = %v, want clean", status)
	}
}

// TestClientInitRepairsExistingLocalRepository verifies that Init repairs
// stale Git-serving permissions before bootstrapGitOps can take its
// already-bootstrapped skip path. Working-tree and private Git metadata remain
// untouched.
func TestClientInitRepairsExistingLocalRepository(t *testing.T) {
	repoPath := t.TempDir()
	cfg := &Config{URL: "file://" + repoPath, Branch: DefaultBranch}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("initial Init() error: %v", err)
	}

	manifestPath := filepath.Join(repoPath, "application.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: Application\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := client.WriteBootstrapMarker(context.Background()); err != nil {
		t.Fatalf("WriteBootstrapMarker() error: %v", err)
	}
	if err := client.CommitAndPush(context.Background(), "bootstrap"); err != nil {
		t.Fatalf("CommitAndPush() error: %v", err)
	}

	hookPath := filepath.Join(repoPath, ".git", "hooks", "private-hook")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o700); err != nil {
		t.Fatalf("create hooks directory: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // Deliberately private metadata.
		t.Fatalf("write private hook: %v", err)
	}

	objectsDir := filepath.Join(repoPath, ".git", "objects")
	if err := filepath.WalkDir(filepath.Join(repoPath, ".git"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(repoPath, ".git", "hooks") {
			return filepath.SkipDir
		}
		mode := os.FileMode(0o600)
		if d.IsDir() {
			mode = 0o700
		}
		return os.Chmod(path, mode) //nolint:gosec // Test setup walks a private TempDir with no concurrent writers.
	}); err != nil {
		t.Fatalf("make .git restrictive: %v", err)
	}
	if err := os.Chmod(repoPath, 0o700); err != nil { //nolint:gosec // Deliberately restrictive stale repository.
		t.Fatalf("chmod repository root: %v", err)
	}

	reopened, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() for existing repo error: %v", err)
	}
	if err := reopened.Init(context.Background()); err != nil {
		t.Fatalf("Init() for existing repo error: %v", err)
	}
	bootstrapped, err := reopened.IsBootstrapped(context.Background())
	if err != nil {
		t.Fatalf("IsBootstrapped() error: %v", err)
	}
	if !bootstrapped {
		t.Fatal("IsBootstrapped() = false, want true")
	}

	assertPathMode(t, repoPath, 0o755)
	assertPathMode(t, manifestPath, 0o600)
	assertPathMode(t, hookPath, 0o700)

	sawObject := false
	if err := filepath.WalkDir(objectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if info.Mode().Perm()&0o055 != 0o055 {
				t.Errorf("object directory %s mode = %v, want read/traverse bits", path, info.Mode().Perm())
			}
			return nil
		}
		sawObject = true
		if info.Mode().Perm()&0o044 != 0o044 {
			t.Errorf("object %s mode = %v, want read bits", path, info.Mode().Perm())
		}
		return nil
	}); err != nil {
		t.Fatalf("walk repaired objects: %v", err)
	}
	if !sawObject {
		t.Fatal("expected at least one Git object")
	}
}

// TestClientUpgradeLocalPermissions verifies the scoped, additive upgrade:
// only the repo root directory and Git-serving data under .git are touched
// (argo reads Git data and rebuilds its own checkout), modes are only ever OR'd
// with the needed read/traverse bits, and special bits are preserved.
func TestClientUpgradeLocalPermissions(t *testing.T) {
	// mkGit creates a minimal .git/objects tree under root so the walk always
	// has something to descend into, mirroring a real post-commit repo.
	mkGit := func(t *testing.T, root string) {
		t.Helper()
		objects := filepath.Join(root, ".git", "objects")
		if err := os.MkdirAll(objects, 0o700); err != nil {
			t.Fatalf("mkdir .git objects: %v", err)
		}
	}

	tests := []struct {
		name        string
		setup       func(t *testing.T, root string) string
		check       func(t *testing.T, root string)
		wantErr     bool
		errContains string
	}{
		{
			name: "root directory preserves special bits",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				if err := os.Chmod(root, 0o700|os.ModeSticky); err != nil {
					t.Fatalf("chmod root: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				assertPathMode(t, root, 0o755)
				info, err := os.Stat(root)
				if err != nil {
					t.Fatalf("stat root: %v", err)
				}
				if info.Mode()&os.ModeSticky == 0 {
					t.Errorf("root mode = %v, want sticky bit preserved", info.Mode())
				}
			},
		},
		{
			name: "working-tree file left untouched",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				path := filepath.Join(root, "nic-config.yaml")
				if err := os.WriteFile(path, []byte("project_name: test\n"), 0o600); err != nil {
					t.Fatalf("write file: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				// Working-tree files are never walked: argo reads .git only, so
				// this 0600 file must be left exactly as-is.
				assertPathMode(t, filepath.Join(root, "nic-config.yaml"), 0o600)
			},
		},
		{
			name: "working-tree directory left untouched",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				nested := filepath.Join(root, "apps", "root")
				if err := os.MkdirAll(nested, 0o700); err != nil {
					t.Fatalf("mkdir nested: %v", err)
				}
				if err := os.Chmod(nested, 0o700); err != nil { //nolint:gosec // Deliberately restrictive setup for permission upgrade.
					t.Fatalf("chmod nested: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				// A working-tree subdirectory is outside .git, so it is never
				// touched — its restrictive 0o700 mode is preserved.
				assertPathMode(t, filepath.Join(root, "apps", "root"), 0o700)
			},
		},
		{
			name: ".git objects",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				objects := filepath.Join(root, ".git", "objects")
				if err := os.MkdirAll(objects, 0o700); err != nil {
					t.Fatalf("mkdir .git objects: %v", err)
				}
				if err := os.Chmod(filepath.Join(root, ".git"), 0o700); err != nil { //nolint:gosec // Deliberately restrictive setup for permission upgrade.
					t.Fatalf("chmod .git: %v", err)
				}
				if err := os.Chmod(objects, 0o700); err != nil { //nolint:gosec // Deliberately restrictive setup for permission upgrade.
					t.Fatalf("chmod objects: %v", err)
				}
				obj := filepath.Join(objects, "deadbeef")
				if err := os.WriteFile(obj, []byte("blob"), 0o600); err != nil {
					t.Fatalf("write object: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				assertPathMode(t, filepath.Join(root, ".git"), 0o755)
				assertPathMode(t, filepath.Join(root, ".git", "objects"), 0o755)
				assertPathMode(t, filepath.Join(root, ".git", "objects", "deadbeef"), 0o644)
			},
		},
		{
			name: "already-executable .git file is unchanged",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				path := filepath.Join(root, ".git", "hooks-sample.sh")
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // Deliberately executable to prove it survives untouched.
					t.Fatalf("write file: %v", err)
				}
				// Chmod explicitly so the "already 0o755" precondition holds
				// regardless of the ambient umask (WriteFile's mode is masked).
				if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // Deliberately executable to prove it survives untouched.
					t.Fatalf("chmod file: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				// 0o755 | 0o044 = 0o755 — group/other read was already set,
				// so the OR is a no-op and +x is never at risk of removal.
				assertPathMode(t, filepath.Join(root, ".git", "hooks-sample.sh"), 0o755)
			},
		},
		{
			name: "private git metadata left untouched",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				for _, dir := range []string{
					"hooks",
					"logs",
					"worktrees",
					filepath.Join("modules", "example", "hooks"),
					filepath.Join("modules", "example", "logs"),
				} {
					path := filepath.Join(root, ".git", dir)
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatalf("mkdir %s: %v", path, err)
					}
					if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // Deliberately private metadata.
						t.Fatalf("chmod %s: %v", path, err)
					}
				}
				for _, name := range []string{"index", "COMMIT_EDITMSG", "MERGE_MSG", "ORIG_HEAD", "FETCH_HEAD"} {
					if err := os.WriteFile(filepath.Join(root, ".git", name), []byte("private"), 0o600); err != nil {
						t.Fatalf("write %s: %v", name, err)
					}
				}
				nestedIndex := filepath.Join(root, ".git", "modules", "example", "index")
				if err := os.WriteFile(nestedIndex, []byte("private"), 0o600); err != nil {
					t.Fatalf("write nested index: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				assertPathMode(t, filepath.Join(root, ".git", "hooks"), 0o700)
				assertPathMode(t, filepath.Join(root, ".git", "logs"), 0o700)
				assertPathMode(t, filepath.Join(root, ".git", "worktrees"), 0o700)
				for _, name := range []string{"index", "COMMIT_EDITMSG", "MERGE_MSG", "ORIG_HEAD", "FETCH_HEAD"} {
					assertPathMode(t, filepath.Join(root, ".git", name), 0o600)
				}
				assertPathMode(t, filepath.Join(root, ".git", "modules", "example", "hooks"), 0o700)
				assertPathMode(t, filepath.Join(root, ".git", "modules", "example", "logs"), 0o700)
				assertPathMode(t, filepath.Join(root, ".git", "modules", "example", "index"), 0o600)
			},
		},
		{
			name: "ref names colliding with private metadata are still upgraded",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				// A branch named "hooks/pre-commit-fix" and a tag named "index"
				// live in the refs namespace, not directly under .git, so the
				// walk must not mistake them for private metadata by basename
				// and leave them unreadable to the non-root repo-server.
				heads := filepath.Join(root, ".git", "refs", "heads", "hooks")
				if err := os.MkdirAll(heads, 0o700); err != nil {
					t.Fatalf("mkdir refs/heads/hooks: %v", err)
				}
				if err := os.WriteFile(filepath.Join(heads, "pre-commit-fix"), []byte("ref\n"), 0o600); err != nil {
					t.Fatalf("write branch ref: %v", err)
				}
				tags := filepath.Join(root, ".git", "refs", "tags")
				if err := os.MkdirAll(tags, 0o700); err != nil {
					t.Fatalf("mkdir refs/tags: %v", err)
				}
				if err := os.WriteFile(filepath.Join(tags, "index"), []byte("ref\n"), 0o600); err != nil {
					t.Fatalf("write tag ref: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				assertPathMode(t, filepath.Join(root, ".git", "refs", "heads", "hooks"), 0o755)
				assertPathMode(t, filepath.Join(root, ".git", "refs", "heads", "hooks", "pre-commit-fix"), 0o644)
				assertPathMode(t, filepath.Join(root, ".git", "refs", "tags", "index"), 0o644)
			},
		},
		{
			name: "symlink skip",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				mkGit(t, root)
				target := filepath.Join(t.TempDir(), "target")
				if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
					t.Fatalf("write symlink target: %v", err)
				}
				link := filepath.Join(root, ".git", "linked-target")
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				return root
			},
			check: func(t *testing.T, root string) {
				t.Helper()
				link := filepath.Join(root, ".git", "linked-target")
				target, err := os.Readlink(link)
				if err != nil {
					t.Fatalf("readlink: %v", err)
				}
				assertPathMode(t, target, 0o600)
			},
		},
		{
			name: "missing root error",
			setup: func(t *testing.T, root string) string {
				t.Helper()
				return filepath.Join(root, "missing")
			},
			wantErr:     true,
			errContains: "open local gitops root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			repoPath := tt.setup(t, root)
			client := &ClientImpl{
				cfg:      &Config{URL: "file://" + repoPath},
				repoPath: repoPath,
			}

			err := client.upgradeLocalPermissions(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("upgradeLocalPermissions() expected error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("upgradeLocalPermissions() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("upgradeLocalPermissions() error: %v", err)
			}
			tt.check(t, root)
		})
	}
}

func assertPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
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
	client := createClientWithLocalRepo(t, "/nonexistent/path/to/repo", "main", "")
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
