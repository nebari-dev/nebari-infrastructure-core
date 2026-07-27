package git

import "context"

// Client defines operations for interacting with a Git repository.
// This interface is used to bootstrap GitOps repositories for ArgoCD.
//
// Implementations of this interface are NOT safe for concurrent use.
// Each goroutine should create its own Client instance.
type Client interface {
	// ValidateAuth checks that credentials are configured and can access the repository.
	// - SSH key configured: validates key format and tests access via remote.List()
	// - Token configured: validates token and tests access via remote.List()
	// Returns an error if credentials are missing, malformed, or cannot access the repo.
	ValidateAuth(ctx context.Context) error

	// Init clones the repository if not present locally, or pulls latest if it exists.
	// The repository is cloned to a temporary directory managed by the client. For
	// local file:// repositories, Init also repairs the additive permissions ArgoCD
	// needs on the repository root and Git-serving metadata before returning.
	Init(ctx context.Context) error

	// WorkDir returns the local working directory path where files can be written.
	// If Config.Path is set, returns the subdirectory path within the repo.
	WorkDir() string

	// CommitAndPush stages all changes, commits with the given message, and pushes to remote.
	// Internally checks for changes first - returns nil without error if nothing changed.
	// For any local file:// repo (including a user-supplied one), also upgrades the repo
	// root and Git-serving data under .git to be group/other-readable so ArgoCD's
	// non-root repo-server can read the objects the commit wrote. Working-tree and
	// private Git metadata are left untouched; permission bits are only ever added.
	CommitAndPush(ctx context.Context, message string) error

	// IsBootstrapped checks if the .bootstrapped marker file exists in the working directory.
	IsBootstrapped(ctx context.Context) (bool, error)

	// WriteBootstrapMarker writes the .bootstrapped marker file to the working directory.
	// The marker contains metadata about when bootstrapping occurred.
	WriteBootstrapMarker(ctx context.Context) error

	// Cleanup removes any temporary resources created by the client (e.g., temp directories).
	// Should be called when done with the client, typically via defer.
	Cleanup() error
}
