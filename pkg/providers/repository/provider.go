package repository

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider provisions or resolves the GitOps repository for a deployment.
//
// Implementations must not depend on pkg/git or go-git: their job is to produce
// a Source, not to operate on the repository.
type Provider interface {
	// Name returns the provider name (e.g. "local", "existing").
	Name() string

	// Validate checks that the provider configuration is well-formed and that
	// any referenced credentials are available. It does not mutate state.
	Validate(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) error

	// Provision resolves (or creates) the backing repository and returns a
	// Source describing how to reach it. Credentials in the returned Source are
	// already resolved from their sources (e.g. environment variables).
	Provision(ctx context.Context, projectName string, repoConfig *config.RepositoryConfig) (Source, error)
}

// Auth is resolved git credentials. It is a sealed interface with two kinds:
// TokenAuth (a token / app-password used as the HTTPS password) and SSHKeyAuth
// (an SSH private key). The values are already resolved from their source (e.g.
// an environment variable) by the provider.
type Auth interface {
	// isAuth seals the interface to the kinds defined in this package.
	isAuth()
}

// TokenAuth authenticates over HTTPS with a token used as the password.
type TokenAuth struct {
	Token string
}

// SSHKeyAuth authenticates over SSH with a private key (PEM).
type SSHKeyAuth struct {
	Key string

	// InsecureSkipHostKeyVerification disables SSH host key verification,
	// removing protection against man-in-the-middle attacks. Only intended
	// for ephemeral environments (e.g. CI) where maintaining a known_hosts
	// file is impractical.
	InsecureSkipHostKeyVerification bool
}

func (TokenAuth) isAuth()  {}
func (SSHKeyAuth) isAuth() {}

// Source describes the GitOps repository a provider creates (or resolves). It is
// a sealed interface with two types: LocalSource (a directory on disk) and
// RemoteSource (a remote URL with credentials).
type Source interface {
	// RepoURL is the URL ArgoCD uses as the Application's repoURL and that
	// manifest templates render.
	RepoURL() string

	// GetBranch returns the git branch to use. Providers resolve the default
	// when constructing the Source, so this is never empty.
	GetBranch() string

	// RepoPath returns the optional subdirectory within the repository that
	// operations are scoped to.
	RepoPath() string

	// isSource seals the interface to the two types defined in this package.
	isSource()
}

// RemoteSource is a remote repository reached over the network
type RemoteSource struct {
	// URL is the repository URL (ssh or https).
	URL string

	// Branch is the git branch to use, already resolved by the provider.
	Branch string

	// Path is an optional subdirectory within the repository.
	Path string

	// PushAuth are the credentials NIC uses to push to the repository.
	PushAuth Auth

	// ReadAuth are the (potentially read-only) credentials ArgoCD uses
	// in-cluster to pull the repository. Nil means fall back to PushAuth.
	ReadAuth Auth
}

func (RemoteSource) isSource()           {}
func (s RemoteSource) RepoURL() string   { return s.URL }
func (s RemoteSource) GetBranch() string { return s.Branch }
func (s RemoteSource) RepoPath() string  { return s.Path }

// ArgoCDAuth returns the credential ArgoCD should use to read the repository,
// falling back to PushAuth when ReadAuth is not set.
func (s RemoteSource) ArgoCDAuth() Auth {
	if s.ReadAuth != nil {
		return s.ReadAuth
	}
	return s.PushAuth
}

// LocalSource is a repository that lives as a directory on disk
type LocalSource struct {
	// Dir is the filesystem path of the repository.
	Dir string

	// Branch is the git branch to use, already resolved by the provider.
	Branch string

	// Path is an optional subdirectory within the repository.
	Path string
}

func (LocalSource) isSource()           {}
func (s LocalSource) RepoURL() string   { return "file://" + s.Dir }
func (s LocalSource) GetBranch() string { return s.Branch }
func (s LocalSource) RepoPath() string  { return s.Path }
