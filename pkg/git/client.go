package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	bootstrapMarkerFile = ".bootstrapped"
	tracerName          = "nebari-infrastructure-core"
	tempDirPrefix       = "nic-gitops-*"
	remoteName          = "origin"
	commitAuthorName    = "Nebari Infrastructure Core"
	commitAuthorEmail   = "nic[bot]@users.noreply.github.com"
	defaultBranch       = "main"
	dirPerm             = 0o750
)

// Client operates on a single git working copy to bootstrap GitOps repositories
// for ArgoCD. Construct it with NewClient, then acquire a working copy with
// Init (a local directory) or Clone (a remote URL). The other operations act on
// that working copy.
//
// A Client holds the state produced by Init/Clone — the repository handle, the
// working directory, and, for remote repositories, the push credentials. State can
// be partial. For example, a local repository has no remote.
//
// Client is NOT safe for concurrent use. Create one client per goroutine.
type Client struct {
	branch string
	path   string // optional subdirectory within the repository

	repo    *git.Repository
	workDir string
	tempDir string // temp dir used to clone a remote repo. It is removed by Cleanup.

	auth transport.AuthMethod // set by Clone, used by Push
}

// NewClient creates a client for the given branch and optional subdirectory
// within the repository. Branch defaults to "main" when empty.
func NewClient(branch, path string) *Client {
	if branch == "" {
		branch = defaultBranch
	}
	return &Client{branch: branch, path: path}
}

// WorkDir returns the working directory where files are written, scoped to the
// subdirectory passed to NewClient when set. Empty until Init or Clone runs.
func (c *Client) WorkDir() string {
	return c.workDir
}

// Init opens the git repository at dir, initializing a new one if dir is not yet
// a repository. Used for a local on-disk repository that NIC commits to in place.
func (c *Client) Init(ctx context.Context, dir string) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.Init")
	defer span.End()

	span.SetAttributes(attribute.String("git.dir", dir), attribute.String("git.branch", c.branch))

	c.workDir = dir
	if c.path != "" {
		c.workDir = filepath.Join(dir, c.path)
		if err := os.MkdirAll(c.workDir, dirPerm); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create subdirectory %s: %w", c.path, err)
		}
	}

	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	switch {
	case os.IsNotExist(err):
		span.SetAttributes(attribute.Bool("git.initialized_new_repo", true))
		repo, err := git.PlainInit(dir, false)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to initialize git repository: %w", err)
		}
		headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(c.branch))
		if err := repo.Storer.SetReference(headRef); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to set HEAD to branch %s: %w", c.branch, err)
		}
		c.repo = repo
		return nil
	case err != nil:
		span.RecordError(err)
		return fmt.Errorf("failed to check git directory: %w", err)
	case !info.IsDir():
		err := fmt.Errorf(".git exists but is not a directory at %s", dir)
		span.RecordError(err)
		return err
	default:
		span.SetAttributes(attribute.Bool("git.opened_existing_repo", true))
		repo, err := git.PlainOpen(dir)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to open git repository: %w", err)
		}
		c.repo = repo
		return nil
	}
}

// Clone clones the repository at url into a temporary directory managed by the
// client, authenticating with auth. Used for a remote repository. An empty
// remote is initialized locally with origin configured so the first push seeds
// it. The credentials are retained for Push.
func (c *Client) Clone(ctx context.Context, url string, auth Auth) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.Clone")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", url),
		attribute.String("git.branch", c.branch),
		attribute.String("git.auth_type", auth.authType()),
	)

	authMethod, err := auth.method()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to build auth: %w", err)
	}
	c.auth = authMethod

	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	c.tempDir = tempDir
	c.workDir = tempDir
	if c.path != "" {
		c.workDir = filepath.Join(tempDir, c.path)
	}

	repo, err := git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
		URL:           url,
		Auth:          c.auth,
		ReferenceName: plumbing.NewBranchReferenceName(c.branch),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		if isEmptyRepoError(err) {
			span.SetAttributes(attribute.Bool("git.empty_repo", true))
			return c.initEmptyRepo(ctx, url)
		}
		// The configured branch may not exist on the remote yet. Retry without
		// pinning a branch (handles content on a different default branch).
		span.SetAttributes(attribute.Bool("git.branch_not_found", true))
		repo, err = git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
			URL:   url,
			Auth:  c.auth,
			Depth: 1,
		})
		if err != nil {
			if isEmptyRepoError(err) {
				span.SetAttributes(attribute.Bool("git.empty_repo", true))
				return c.initEmptyRepo(ctx, url)
			}
			span.RecordError(err)
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	}
	c.repo = repo

	// Ensure we're on the configured branch.
	worktree, err := repo.Worktree()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	headRef, err := repo.Head()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	if headRef.Name().Short() != c.branch {
		if err := worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(c.branch),
			Create: true,
		}); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to checkout branch %s: %w", c.branch, err)
		}
	}

	if c.path != "" {
		if err := os.MkdirAll(c.workDir, dirPerm); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create subdirectory %s: %w", c.path, err)
		}
	}
	return nil
}

// ValidateAuth checks that auth can access the remote repository at url, without
// cloning it. An empty repository counts as accessible.
func (c *Client) ValidateAuth(ctx context.Context, url string, auth Auth) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.ValidateAuth")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", url),
		attribute.String("git.auth_type", auth.authType()),
	)

	authMethod, err := auth.method()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to build auth: %w", err)
	}

	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{url},
	})
	if _, err := remote.ListContext(ctx, &git.ListOptions{Auth: authMethod}); err != nil {
		// Empty repositories return an error, but authentication still worked.
		if isEmptyRepoError(err) {
			span.SetAttributes(attribute.Bool("git.empty_repo", true))
			return nil
		}
		span.RecordError(err)
		return fmt.Errorf("failed to authenticate with repository %s: %w", url, err)
	}
	return nil
}

// Commit stages all changes and commits them. Returns nil without error when
// there is nothing to commit.
func (c *Client) Commit(ctx context.Context, message string) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.Commit")
	defer span.End()

	span.SetAttributes(attribute.String("git.commit_message", message))

	if c.repo == nil {
		err := fmt.Errorf("repository not initialized, call Init or Clone first")
		span.RecordError(err)
		return err
	}
	_, err := c.stageAndCommit(ctx, message)
	return err
}

// Push pushes committed changes to the origin remote configured by Clone, using
// the credentials captured then. Returns nil when the remote is already up to
// date, and an error when no remote is configured (e.g. a local repository).
func (c *Client) Push(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.Push")
	defer span.End()

	if c.repo == nil {
		err := fmt.Errorf("repository not initialized, call Clone first")
		span.RecordError(err)
		return err
	}

	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", c.branch, c.branch))
	err := c.repo.PushContext(ctx, &git.PushOptions{
		Auth:     c.auth,
		RefSpecs: []config.RefSpec{refSpec},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		span.RecordError(err)
		return fmt.Errorf("failed to push: %w", err)
	}
	return nil
}

// IsBootstrapped reports whether the .bootstrapped marker file exists in WorkDir.
func (c *Client) IsBootstrapped(ctx context.Context) (bool, error) {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.IsBootstrapped")
	defer span.End()

	markerPath := filepath.Join(c.workDir, bootstrapMarkerFile)
	if _, err := os.Stat(markerPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		span.RecordError(err)
		return false, fmt.Errorf("failed to check bootstrap marker: %w", err)
	}
	return true, nil
}

// WriteBootstrapMarker writes the .bootstrapped marker file to WorkDir.
func (c *Client) WriteBootstrapMarker(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.WriteBootstrapMarker")
	defer span.End()

	markerPath := filepath.Join(c.workDir, bootstrapMarkerFile)
	content := fmt.Sprintf("bootstrapped_at: %s\n", time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(markerPath, []byte(content), 0o600); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}
	return nil
}

// Cleanup removes the clone temp directory, if one was created. No-op for a
// local repository (which has no temp directory).
func (c *Client) Cleanup() error {
	if c.tempDir != "" {
		return os.RemoveAll(c.tempDir)
	}
	return nil
}

// stageAndCommit stages all changes and commits them. Returns true if a commit
// was made, false if the worktree was clean.
func (c *Client) stageAndCommit(ctx context.Context, message string) (bool, error) {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.stageAndCommit")
	defer span.End()

	worktree, err := c.repo.Worktree()
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to get status: %w", err)
	}
	if status.IsClean() {
		span.SetAttributes(attribute.Bool("git.no_changes", true))
		return false, nil
	}

	if err := worktree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to stage changes: %w", err)
	}

	if _, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  commitAuthorName,
			Email: commitAuthorEmail,
			When:  time.Now(),
		},
	}); err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to commit: %w", err)
	}

	span.SetAttributes(attribute.Bool("git.committed", true))
	return true, nil
}

// initEmptyRepo initializes a new local repository (for an empty remote) with
// origin configured, so the first Push seeds the remote.
func (c *Client) initEmptyRepo(ctx context.Context, url string) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.initEmptyRepo")
	defer span.End()

	span.SetAttributes(attribute.String("git.branch", c.branch))

	repo, err := git.PlainInit(c.tempDir, false)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(c.branch))
	if err := repo.Storer.SetReference(headRef); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to set HEAD to branch %s: %w", c.branch, err)
	}

	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: remoteName,
		URLs: []string{url},
	}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create remote: %w", err)
	}
	c.repo = repo

	if c.path != "" {
		if err := os.MkdirAll(c.workDir, dirPerm); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create subdirectory %s: %w", c.path, err)
		}
	}
	return nil
}

// isEmptyRepoError reports whether err indicates an empty remote repository.
func isEmptyRepoError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "remote repository is empty") ||
		strings.Contains(errStr, "couldn't find remote ref") ||
		strings.Contains(errStr, "reference not found")
}
