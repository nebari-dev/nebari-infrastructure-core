package git

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// GitOpsDirMode is the directory mode for generated GitOps repository content.
	GitOpsDirMode os.FileMode = 0o755

	// GitOpsFileMode is the file mode for generated non-secret GitOps files.
	GitOpsFileMode os.FileMode = 0o644
)

// EnsureLocalGitOpsDir creates a local GitOps root with the desired initial
// mode. The process umask may restrict a newly created directory; the Client
// repairs the mounted repository root and Git-serving data after
// initialization and local commits.
func EnsureLocalGitOpsDir(ctx context.Context, path string) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.EnsureLocalGitOpsDir")
	defer span.End()
	span.SetAttributes(attribute.String("git.repo_path", path))

	if err := os.MkdirAll(path, GitOpsDirMode); err != nil {
		span.RecordError(err)
		return fmt.Errorf("create local gitops directory %s: %w", path, err)
	}
	return nil
}

// privateGitDirNames and privateGitFileNames are the entries, directly inside
// a Git directory (.git or a submodule's .git/modules/<name>), that hold data
// private to the source checkout and never read to serve commits to ArgoCD, so
// the permission upgrade skips them. They are matched only at that exact path
// position so a branch, tag, or ref whose name happens to collide with one of
// these is still repaired.
var privateGitDirNames = map[string]bool{
	"hooks":     true,
	"logs":      true,
	"worktrees": true,
}

var privateGitFileNames = map[string]bool{
	"index":          true,
	"COMMIT_EDITMSG": true,
	"MERGE_MSG":      true,
	"ORIG_HEAD":      true,
	"FETCH_HEAD":     true,
}

// upgradeLocalPermissions makes ONLY the repo root directory and Git-serving
// data under .git group/other-readable, so ArgoCD's non-root repo-server can read
// the repo through a read-only kind hostPath mount. It deliberately does NOT
// touch the working tree: argo's repo-server reads a local file:// repo by
// running git against .git (objects + refs) and rebuilding its own checkout
// in its own cache — it never reads the source working tree. So the only
// things that need group/other read+traverse are (a) the repo root dir itself,
// so argo can traverse into it to reach .git, and (b) data used to serve Git
// objects and refs. Working-tree files and private local Git metadata such as
// hooks, reflogs, and the index are irrelevant to argo and left exactly as-is.
//
// This specifically fixes .git/objects: go-git writes every object through a
// temp-file-then-rename (storage/filesystem/dotgit/writers.go), and the temp
// file is always created at 0600 regardless of the process umask, so the
// non-root repo-server otherwise can't read a fresh commit.
//
// Callers must invoke this method only on a repository acquired with Init (a
// local on-disk repository). It runs for ANY such repo NIC commits to,
// including a user-supplied one, not just the directory NIC auto-generates.
// It's safe to touch a user's repo here precisely because of the .git-only + OR-only
// design: (1) .git is not tracked by git, so upgrading it can never produce a
// tracked mode-only diff on the next commit; (2) the OR only adds bits, so an
// existing +x on a git hook (or any other bit) survives untouched; and (3) the
// working tree is never modified, so a user's hand-edited files are left
// exactly as-is. The .git/objects NIC writes are NIC's own commit artifacts,
// so making them readable is just finishing the job of the commit.
//
// Permissions are only ever OR'd in, never replaced: a path that already has
// the needed bits is left untouched, and special bits such as setgid and sticky
// are preserved. This cannot strip an existing executable bit or produce a
// tracked mode-only diff. It runs when a local repository is initialized and
// after every local commit so stale or partially repaired repositories recover
// before the already-bootstrapped skip path. Symlinks are skipped.
func (c *Client) upgradeLocalPermissions(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.upgradeLocalPermissions")
	defer span.End()
	span.SetAttributes(attribute.String("git.repo_path", c.localDir))

	rootDir, err := os.OpenRoot(c.localDir)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("open local gitops root %s: %w", c.localDir, err)
	}
	defer func() {
		_ = rootDir.Close()
	}()

	// isGitDir reports whether p (a slash-separated path relative to the repo
	// root) is a Git directory whose direct children may carry private
	// metadata: the top-level .git or a submodule's .git/modules/<name>.
	// Keep this as a closure inside the traced operation rather than a separate
	// uninstrumented package function.
	isGitDir := func(p string) bool {
		if p == ".git" {
			return true
		}
		if rest, ok := strings.CutPrefix(p, ".git/modules/"); ok {
			return rest != "" && !strings.Contains(rest, "/")
		}
		return false
	}

	// upgrade OR's the group/other read (and, for dirs, traverse) bits into a
	// single path, never replacing existing bits. Symlinks are skipped.
	//
	// Bits to OR in, expressed as owner/group/other octal digits:
	//   0o044 = ---r--r-- : files only need to be readable by group/other.
	//   0o055 = ---r-xr-x : directories also need the execute bit for
	//   group/other, since traversing a directory (opening a path inside it)
	//   requires execute, not just read.
	// Either way the owner digit is 0, so owner's existing bits (whatever they
	// are) are never touched by the OR.
	//
	// `perm | add` can only set bits, never clear them, so a mode that's
	// already sufficient (or deliberately stricter for some other reason) is
	// left exactly as-is — hence the equality check to skip the chmod syscall
	// entirely when there's nothing to add.
	upgrade := func(path string, d fs.DirEntry) error {
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		add := os.FileMode(0o044)
		if d.IsDir() {
			add = 0o055
		}
		const specialBits = os.ModeSetuid | os.ModeSetgid | os.ModeSticky
		currentMode := info.Mode().Perm() | info.Mode()&specialBits
		if mode := currentMode | add; mode != currentMode {
			if err := rootDir.Chmod(path, mode); err != nil {
				return fmt.Errorf("set permissions on %s: %w", path, err)
			}
		}
		return nil
	}

	// (a) Make the repo root itself group/other-traversable so argo can
	// descend into it to reach .git. We only touch the root dir, not the
	// working-tree files it contains.
	rootInfo, err := rootDir.Stat(".")
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("stat local gitops root %s: %w", c.localDir, err)
	}
	if err := upgrade(".", fs.FileInfoToDirEntry(rootInfo)); err != nil {
		span.RecordError(err)
		return fmt.Errorf("upgrade local gitops root %s: %w", c.localDir, err)
	}

	// (b) Walk the .git subtree, excluding metadata that is private to the
	// source checkout and is not used to serve commits to ArgoCD. These names
	// are only private when they sit directly inside a Git directory — .git
	// itself or a submodule's .git/modules/<name> — so we match by path
	// position, not by basename. Matching the basename anywhere in the tree
	// over-matches into the refs namespace: a branch or tag named e.g.
	// "hooks/..." creates .git/refs/heads/hooks, and a ref named exactly
	// "index"/"ORIG_HEAD"/etc. collides too, leaving those refs unreadable to
	// the non-root repo-server.
	err = fs.WalkDir(rootDir.FS(), ".git", func(entryPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if isGitDir(path.Dir(entryPath)) {
			if d.IsDir() {
				if privateGitDirNames[d.Name()] {
					return fs.SkipDir
				}
			} else if privateGitFileNames[d.Name()] {
				return nil
			}
		}
		return upgrade(entryPath, d)
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("upgrade local gitops permissions under %s: %w", c.localDir, err)
	}
	return nil
}
