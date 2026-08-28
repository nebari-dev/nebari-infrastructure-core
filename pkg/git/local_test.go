package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

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

func TestEnsureLocalGitOpsDir(t *testing.T) {
	t.Run("creates missing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gitops", "project")
		if err := EnsureLocalGitOpsDir(context.Background(), path); err != nil {
			t.Fatalf("EnsureLocalGitOpsDir() error: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat created directory: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("EnsureLocalGitOpsDir() created %s, want directory", path)
		}
	})

	t.Run("no-op on existing directory", func(t *testing.T) {
		path := t.TempDir()
		if err := EnsureLocalGitOpsDir(context.Background(), path); err != nil {
			t.Fatalf("EnsureLocalGitOpsDir() error on existing dir: %v", err)
		}
	})

	t.Run("errors when path is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if err := EnsureLocalGitOpsDir(context.Background(), path); err == nil {
			t.Fatal("EnsureLocalGitOpsDir() expected error for file path, got nil")
		}
	})
}

// TestCommitUpgradesLocalPermissions verifies that Commit on a local repo
// makes the repo root and .git (including .git/objects) group/other readable.
// go-git writes loose objects via a temp-file-then-rename that is always
// created at 0600 regardless of the process umask, so without this fix a
// non-root reader (e.g. ArgoCD's repo-server) can't read a fresh commit. It
// also verifies the upgrade leaves the working tree untouched — argo reads
// .git and rebuilds its own checkout, so a 0600 working-tree file keeps its
// mode — and that the upgrade is additive: an executable file under .git
// (which the walk does touch) gains group/other read while keeping its +x, so
// a hand-picked executable bit can't be stripped, and no mode-only diff is
// staged.
func TestCommitUpgradesLocalPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	client := NewClient("main", "")
	if err := client.Init(context.Background(), tmpDir); err != nil {
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

	if err := client.Commit(context.Background(), "commit"); err != nil {
		t.Fatalf("Commit() error: %v", err)
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

// TestCommitUpgradesUserSuppliedLocalRepo verifies that NIC can open a
// pre-existing user repository and make the new Git objects from its own
// commit group/other-readable without changing existing tracked files or
// introducing a mode-only worktree diff.
func TestCommitUpgradesUserSuppliedLocalRepo(t *testing.T) {
	tmpDir := t.TempDir()

	existingRepo, err := gogit.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("PlainInit() existing repository error: %v", err)
	}
	if err := existingRepo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD,
		plumbing.NewBranchReferenceName(defaultBranch),
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

	client := NewClient("main", "")
	if err := client.Init(context.Background(), tmpDir); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	filePath := filepath.Join(client.WorkDir(), "nic-generated.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if err := client.Commit(context.Background(), "commit"); err != nil {
		t.Fatalf("Commit() error: %v", err)
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

// TestInitRepairsExistingLocalRepository verifies that Init repairs stale
// Git-serving permissions before bootstrapGitOps can take its
// already-bootstrapped skip path. Working-tree and private Git metadata remain
// untouched.
func TestInitRepairsExistingLocalRepository(t *testing.T) {
	repoPath := t.TempDir()

	client := NewClient(defaultBranch, "")
	if err := client.Init(context.Background(), repoPath); err != nil {
		t.Fatalf("initial Init() error: %v", err)
	}

	manifestPath := filepath.Join(repoPath, "application.yaml")
	if err := os.WriteFile(manifestPath, []byte("kind: Application\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := client.WriteBootstrapMarker(context.Background()); err != nil {
		t.Fatalf("WriteBootstrapMarker() error: %v", err)
	}
	if err := client.Commit(context.Background(), "bootstrap"); err != nil {
		t.Fatalf("Commit() error: %v", err)
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

	reopened := NewClient(defaultBranch, "")
	if err := reopened.Init(context.Background(), repoPath); err != nil {
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

// TestUpgradeLocalPermissions verifies the scoped, additive upgrade: only the
// repo root directory and Git-serving data under .git are touched (argo reads
// Git data and rebuilds its own checkout), modes are only ever OR'd with the
// needed read/traverse bits, and special bits are preserved.
func TestUpgradeLocalPermissions(t *testing.T) {
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
			client := &Client{localDir: repoPath}

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
