package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		span.RecordError(err)
		return fmt.Errorf("failed to authenticate with repository %s: %w", c.cfg.URL, err)
	}

	return nil
}

// Init clones the repository or pulls latest if already cloned.
func (c *ClientImpl) Init(ctx context.Context) error {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "git.Init")
	defer span.End()

	span.SetAttributes(
		attribute.String("git.url", c.cfg.URL),
		attribute.String("git.branch", c.cfg.GetBranch()),
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
		ReferenceName: plumbing.NewBranchReferenceName(c.cfg.GetBranch()),
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("failed to clone repository: %w", err)
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

	// Push
	err = c.repo.PushContext(ctx, &git.PushOptions{
		Auth: c.auth,
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
