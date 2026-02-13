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
)

// ClientImpl implements Client using go-git.
type ClientImpl struct {
	cfg      *Config
	auth     transport.AuthMethod
	repo     *git.Repository
	tempDir  string
	workDir  string
	repoPath string
}

// NewClient creates a new git client from the provided configuration.
// The client must be cleaned up with Cleanup() when done.
//
// Note: ClientImpl is NOT safe for concurrent use. Each goroutine should
// create its own client instance.
func NewClient(cfg *Config) (*ClientImpl, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Build auth after creating tempDir so we can clean up on failure
	auth, err := cfg.Auth.GetAuth()
	if err != nil {
		// Clean up tempDir on auth failure to prevent resource leak
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to build auth: %w", err)
	}

	repoPath := tempDir
	workDir := tempDir
	if cfg.Path != "" {
		workDir = filepath.Join(tempDir, cfg.Path)
	}

	return &ClientImpl{
		cfg:      cfg,
		auth:     auth,
		tempDir:  tempDir,
		workDir:  workDir,
		repoPath: repoPath,
	}, nil
}

// ValidateAuth checks that credentials can access the repository.
func (c *ClientImpl) ValidateAuth(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.ValidateAuth")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", c.cfg.URL),
		attribute.String("git.auth_type", c.cfg.Auth.AuthType()),
	)

	// Create a remote to test access without cloning
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: remoteName,
		URLs: []string{c.cfg.URL},
	})

	// List refs to validate access
	_, err := remote.ListContext(ctx, &git.ListOptions{
		Auth: c.auth,
	})
	if err != nil {
		// Empty repositories return an error but authentication still works
		// Check if it's an "empty repository" error which is OK
		if isEmptyRepoError(err) {
			span.SetAttributes(attribute.Bool("git.empty_repo", true))
			return nil
		}
		span.RecordError(err)
		return fmt.Errorf("failed to authenticate with repository %s: %w", c.cfg.URL, err)
	}

	return nil
}

// isEmptyRepoError checks if the error indicates an empty repository
func isEmptyRepoError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "remote repository is empty") ||
		strings.Contains(errStr, "couldn't find remote ref") ||
		strings.Contains(errStr, "reference not found")
}

// Init clones the repository or pulls latest if already cloned.
// For empty repositories, it initializes a new local repo with the remote configured.
func (c *ClientImpl) Init(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.Init")
	defer span.End()

	branchName := c.cfg.GetBranch()
	span.SetAttributes(
		attribute.String("git.url", c.cfg.URL),
		attribute.String("git.branch", branchName),
		attribute.String("git.repo_path", c.repoPath),
	)

	// Check if repo already exists
	if c.repo != nil {
		// Pull latest
		return c.pull(ctx)
	}

	// Clone the repository (shallow clone - we only need latest for push)
	repo, err := git.PlainCloneContext(ctx, c.repoPath, false, &git.CloneOptions{
		URL:           c.cfg.URL,
		Auth:          c.auth,
		ReferenceName: plumbing.NewBranchReferenceName(branchName),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		// Handle empty repository
		if isEmptyRepoError(err) {
			span.SetAttributes(attribute.Bool("git.empty_repo", true))
			return c.initEmptyRepo(ctx)
		}

		// If the configured branch doesn't exist, try cloning without branch specification
		// This handles the case where the remote has content on a different default branch
		span.SetAttributes(attribute.Bool("git.branch_not_found", true))
		repo, err = git.PlainCloneContext(ctx, c.repoPath, false, &git.CloneOptions{
			URL:   c.cfg.URL,
			Auth:  c.auth,
			Depth: 1,
		})
		if err != nil {
			if isEmptyRepoError(err) {
				span.SetAttributes(attribute.Bool("git.empty_repo", true))
				return c.initEmptyRepo(ctx)
			}
			span.RecordError(err)
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	c.repo = repo

	// Ensure we're on the correct branch
	worktree, err := repo.Worktree()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Check if we need to create or checkout the branch
	headRef, err := repo.Head()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	currentBranch := headRef.Name().Short()
	if currentBranch != branchName {
		// Try to checkout the configured branch
		err = worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branchName),
			Create: true,
		})
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
		}
	}

	// Create subdirectory if Path is specified
	if c.cfg.Path != "" {
		if err := os.MkdirAll(c.workDir, 0750); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create subdirectory %s: %w", c.cfg.Path, err)
		}
	}

	return nil
}

// initEmptyRepo initializes a new local repository for an empty remote.
func (c *ClientImpl) initEmptyRepo(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.initEmptyRepo")
	defer span.End()

	branchName := c.cfg.GetBranch()
	span.SetAttributes(attribute.String("git.branch", branchName))

	// Initialize a new repository
	repo, err := git.PlainInit(c.repoPath, false)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to initialize repository: %w", err)
	}

	// Set HEAD to point to the configured branch (instead of default "master")
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branchName))
	if err := repo.Storer.SetReference(headRef); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to set HEAD to branch %s: %w", branchName, err)
	}

	// Create the remote
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: remoteName,
		URLs: []string{c.cfg.URL},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to create remote: %w", err)
	}

	c.repo = repo

	// Create subdirectory if Path is specified
	if c.cfg.Path != "" {
		if err := os.MkdirAll(c.workDir, 0750); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create subdirectory %s: %w", c.cfg.Path, err)
		}
	}

	return nil
}

// pull fetches and merges latest changes from remote.
func (c *ClientImpl) pull(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.pull")
	defer span.End()

	worktree, err := c.repo.Worktree()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = worktree.PullContext(ctx, &git.PullOptions{
		Auth:          c.auth,
		ReferenceName: plumbing.NewBranchReferenceName(c.cfg.GetBranch()),
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		span.RecordError(err)
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// WorkDir returns the local working directory path.
func (c *ClientImpl) WorkDir() string {
	return c.workDir
}

// CommitAndPush stages all changes, commits, and pushes to remote.
// No-op if there are no changes.
func (c *ClientImpl) CommitAndPush(ctx context.Context, message string) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.CommitAndPush")
	defer span.End()

	span.SetAttributes(attribute.String("git.commit_message", message))

	if c.repo == nil {
		err := fmt.Errorf("repository not initialized, call Init first")
		span.RecordError(err)
		return err
	}

	worktree, err := c.repo.Worktree()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Check for changes
	status, err := worktree.Status()
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to get status: %w", err)
	}

	if status.IsClean() {
		span.SetAttributes(attribute.Bool("git.no_changes", true))
		return nil
	}

	// Stage all changes
	if err := worktree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Commit
	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  commitAuthorName,
			Email: commitAuthorEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Push to the configured branch
	branchName := c.cfg.GetBranch()
	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName))
	err = c.repo.PushContext(ctx, &git.PushOptions{
		Auth:     c.auth,
		RefSpecs: []config.RefSpec{refSpec},
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// IsBootstrapped checks if the .bootstrapped marker file exists.
func (c *ClientImpl) IsBootstrapped(ctx context.Context) (bool, error) {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.IsBootstrapped")
	defer span.End()

	markerPath := filepath.Join(c.workDir, bootstrapMarkerFile)
	_, err := os.Stat(markerPath)
	if os.IsNotExist(err) {
		span.SetAttributes(attribute.Bool("git.bootstrapped", false))
		return false, nil
	}
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("failed to check bootstrap marker: %w", err)
	}

	span.SetAttributes(attribute.Bool("git.bootstrapped", true))
	return true, nil
}

// WriteBootstrapMarker writes the .bootstrapped marker file.
func (c *ClientImpl) WriteBootstrapMarker(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	_, span := tracer.Start(ctx, "git.WriteBootstrapMarker")
	defer span.End()

	markerPath := filepath.Join(c.workDir, bootstrapMarkerFile)

	content := fmt.Sprintf("bootstrapped_at: %s\n", time.Now().UTC().Format(time.RFC3339))

	if err := os.WriteFile(markerPath, []byte(content), 0600); err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to write bootstrap marker: %w", err)
	}

	return nil
}

// Cleanup removes temporary resources.
func (c *ClientImpl) Cleanup() error {
	if c.tempDir != "" {
		return os.RemoveAll(c.tempDir)
	}
	return nil
}
