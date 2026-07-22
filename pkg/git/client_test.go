package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// skipIfNoGit skips tests that exercise go-git's file transport, which shells
// out to the git binary (clone/push against a local path).
func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available; skipping remote (file-transport) test")
	}
}

// writeFile writes content into the client's working directory.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// headCommitMessage opens the repo at dir and returns its HEAD commit message.
func headCommitMessage(t *testing.T, dir string) string {
	t.Helper()
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}
	return commit.Message
}

func TestNewClientWorkDirEmptyBeforeAcquire(t *testing.T) {
	c := NewClient("main", "")
	if got := c.WorkDir(); got != "" {
		t.Errorf("WorkDir() before Init/Clone = %q, want empty", got)
	}
}

func TestNewClientDefaultsBranch(t *testing.T) {
	c := NewClient("", "")
	if c.branch != defaultBranch {
		t.Errorf("branch = %q, want %q", c.branch, defaultBranch)
	}
}

func TestLocalInitAndCommit(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("main", "")

	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if c.WorkDir() != dir {
		t.Errorf("WorkDir() = %q, want %q", c.WorkDir(), dir)
	}

	writeFile(t, c.WorkDir(), "app.yaml", "kind: Test\n")
	if err := c.Commit(context.Background(), "initial commit"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	if msg := headCommitMessage(t, dir); msg != "initial commit" {
		t.Errorf("HEAD commit message = %q, want %q", msg, "initial commit")
	}

	// HEAD should be on the configured branch.
	repo, _ := gogit.PlainOpen(dir)
	head, _ := repo.Head()
	if head.Name() != plumbing.NewBranchReferenceName("main") {
		t.Errorf("HEAD ref = %q, want refs/heads/main", head.Name())
	}
}

func TestLocalInitWithSubPath(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("main", "clusters/test")

	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	wantWorkDir := filepath.Join(dir, "clusters/test")
	if c.WorkDir() != wantWorkDir {
		t.Errorf("WorkDir() = %q, want %q", c.WorkDir(), wantWorkDir)
	}
	if _, err := os.Stat(wantWorkDir); err != nil {
		t.Errorf("subdirectory not created: %v", err)
	}

	writeFile(t, c.WorkDir(), "app.yaml", "kind: Test\n")
	if err := c.Commit(context.Background(), "commit in subpath"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
	if msg := headCommitMessage(t, dir); msg != "commit in subpath" {
		t.Errorf("HEAD commit message = %q, want %q", msg, "commit in subpath")
	}
}

func TestLocalInitOpensExistingRepo(t *testing.T) {
	dir := t.TempDir()

	// First Init creates the repo and commits.
	c1 := NewClient("main", "")
	if err := c1.Init(context.Background(), dir); err != nil {
		t.Fatalf("first Init() error: %v", err)
	}
	writeFile(t, c1.WorkDir(), "a.yaml", "x\n")
	if err := c1.Commit(context.Background(), "first"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	// Second Init on the same dir opens the existing repo.
	c2 := NewClient("main", "")
	if err := c2.Init(context.Background(), dir); err != nil {
		t.Fatalf("second Init() error: %v", err)
	}
	writeFile(t, c2.WorkDir(), "b.yaml", "y\n")
	if err := c2.Commit(context.Background(), "second"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
	if msg := headCommitMessage(t, dir); msg != "second" {
		t.Errorf("HEAD commit message = %q, want %q", msg, "second")
	}
}

func TestCommitNoChanges(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("main", "")
	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	// No files written — committing a clean worktree is a no-op, not an error.
	if err := c.Commit(context.Background(), "noop"); err != nil {
		t.Errorf("Commit() on clean worktree error: %v", err)
	}
}

func TestCommitBeforeAcquireErrors(t *testing.T) {
	c := NewClient("main", "")
	if err := c.Commit(context.Background(), "msg"); err == nil {
		t.Error("Commit() before Init/Clone expected error, got nil")
	}
}

func TestIsBootstrappedAndMarker(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("main", "")
	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	got, err := c.IsBootstrapped(context.Background())
	if err != nil {
		t.Fatalf("IsBootstrapped() error: %v", err)
	}
	if got {
		t.Error("IsBootstrapped() = true before marker written, want false")
	}

	if err := c.WriteBootstrapMarker(context.Background()); err != nil {
		t.Fatalf("WriteBootstrapMarker() error: %v", err)
	}

	got, err = c.IsBootstrapped(context.Background())
	if err != nil {
		t.Fatalf("IsBootstrapped() error: %v", err)
	}
	if !got {
		t.Error("IsBootstrapped() = false after marker written, want true")
	}
}

func TestCleanupLocalIsNoOp(t *testing.T) {
	dir := t.TempDir()
	c := NewClient("main", "")
	if err := c.Init(context.Background(), dir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if err := c.Cleanup(); err != nil {
		t.Errorf("Cleanup() error: %v", err)
	}
	// A local repository is never deleted by Cleanup.
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("local directory removed by Cleanup: %v", err)
	}
}

func TestRemoteCloneCommitPush(t *testing.T) {
	skipIfNoGit(t)

	// An empty bare repository acts as the remote.
	bareDir := t.TempDir()
	if _, err := gogit.PlainInit(bareDir, true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}

	c := NewClient("main", "")
	defer func() {
		if err := c.Cleanup(); err != nil {
			t.Errorf("Cleanup() error: %v", err)
		}
	}()

	if err := c.ValidateAuth(context.Background(), bareDir, Auth{}); err != nil {
		t.Fatalf("ValidateAuth() on empty repo error: %v", err)
	}
	if err := c.Clone(context.Background(), bareDir, Auth{}); err != nil {
		t.Fatalf("Clone() error: %v", err)
	}

	writeFile(t, c.WorkDir(), "app.yaml", "kind: Test\n")
	if err := c.Commit(context.Background(), "bootstrap"); err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
	if err := c.Push(context.Background()); err != nil {
		t.Fatalf("Push() error: %v", err)
	}

	// The bare repo should now have the branch we pushed.
	bare, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("open bare repo: %v", err)
	}
	if _, err := bare.Reference(plumbing.NewBranchReferenceName("main"), false); err != nil {
		t.Errorf("bare repo missing pushed branch main: %v", err)
	}

	// Cleanup removes the clone's temp working directory.
	workDir := c.WorkDir()
	if err := c.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error: %v", err)
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Errorf("clone temp dir not removed by Cleanup: stat err = %v", err)
	}
}
