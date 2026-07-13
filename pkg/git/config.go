package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	// DefaultBranch is the default git branch name when none is specified.
	DefaultBranch = "main"

	// GitOpsDirMode is the directory mode for generated GitOps repository content.
	GitOpsDirMode os.FileMode = 0o755

	// GitOpsFileMode is the file mode for generated non-secret GitOps files.
	GitOpsFileMode os.FileMode = 0o644
)

var userHomeDir = os.UserHomeDir

// DefaultLocalPath returns the host directory NIC manages for a project's
// local gitops repository when no git_repository is configured. It lives under
// the user's home directory so the repo is durable and stays on host paths that
// kind/Docker Desktop can mount reliably.
func DefaultLocalPath(projectName string) string {
	homeDir, err := userHomeDir()
	if err != nil || homeDir == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("nebari-gitops-%s", projectName))
	}
	return filepath.Join(homeDir, ".nic", "gitops", projectName)
}

// EnsureLocalGitOpsDir creates a NIC-managed local GitOps root and sets its
// permissions so non-root ArgoCD pods can read it through a kind hostPath mount.
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

// Config represents git repository configuration for GitOps bootstrap.
// Secrets (SSH keys, tokens) are read from environment variables, never stored in config.
type Config struct {
	// URL is the repository URL (SSH or HTTPS format)
	// Examples: "git@github.com:org/repo.git" or "https://github.com/org/repo.git"
	URL string `yaml:"url" json:"url"`

	// Branch is the git branch to use (default: "main")
	Branch string `yaml:"branch" json:"branch"`

	// Path is an optional subdirectory within the repository
	// If specified, all operations are scoped to this path
	Path string `yaml:"path" json:"path"`

	// Auth specifies credentials for NIC to push to the repository (requires write access)
	Auth AuthConfig `yaml:"auth" json:"auth"`

	// ArgoCDAuth specifies optional separate credentials for ArgoCD (read-only access)
	// If not specified, falls back to Auth
	ArgoCDAuth *AuthConfig `yaml:"argocd_auth,omitempty" json:"argocd_auth,omitempty"`

	// Managed marks a local file:// repository that NIC fully owns: the
	// auto-generated directory created via DefaultLocalPath when no
	// git_repository is configured. It is never set for a user-supplied
	// git_repository, including a user-supplied file:// path. Only Managed
	// repos get post-commit permission upgrades (see ClientImpl.CommitAndPush);
	// NIC never mutates permissions in a repository it doesn't own.
	Managed bool `yaml:"-" json:"-"`
}

// CredentialProvider abstracts credential retrieval for git authentication.
// This interface enables dependency injection for testing.
type CredentialProvider interface {
	// GetAuth returns the configured transport.AuthMethod for git operations.
	// Returns an error if credentials are missing or invalid.
	GetAuth() (transport.AuthMethod, error)
}

// AuthConfig specifies authentication credentials for git operations.
// Only one of SSHKeyEnv or TokenEnv should be set.
// AuthConfig implements CredentialProvider.
type AuthConfig struct {
	// SSHKeyEnv is the name of the environment variable containing the SSH private key
	// The key should be in PEM format (e.g., contents of ~/.ssh/id_ed25519)
	SSHKeyEnv string `yaml:"ssh_key_env" json:"ssh_key_env"`

	// TokenEnv is the name of the environment variable containing the personal access token
	// Used for HTTPS authentication
	TokenEnv string `yaml:"token_env" json:"token_env"`
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("git repository url is required")
	}

	// For local file paths, skip auth validation. A missing directory is valid because
	// Validate runs before Deploy, and for the local (kind) provider the
	// directory is created during cluster creation.
	if c.IsLocalPath() {
		path, err := c.GetLocalPath()
		if err != nil {
			return err
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return fmt.Errorf("local path must be a directory: %s", path)
		}
		return nil
	}

	// For remote repositories, validate authentication
	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	if c.ArgoCDAuth != nil {
		if err := c.ArgoCDAuth.Validate(); err != nil {
			return fmt.Errorf("argocd_auth: %w", err)
		}
	}

	return nil
}

// IsLocalPath returns true if the URL uses the file:// protocol (local filesystem).
func (c *Config) IsLocalPath() bool {
	return strings.HasPrefix(c.URL, "file://")
}

// GetLocalPath returns the filesystem path from a file:// URL.
// Returns an error if not a local path or if the path is invalid.
// The path is cleaned to prevent directory traversal attacks.
func (c *Config) GetLocalPath() (string, error) {
	if !c.IsLocalPath() {
		return "", fmt.Errorf("not a local file:// URL: %s", c.URL)
	}
	path := strings.TrimPrefix(c.URL, "file://")
	// Clean the path to resolve ".." and other traversal attempts
	path = filepath.Clean(path)
	// Ensure the path is absolute to prevent relative path attacks
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file:// URL must use absolute path, got: %s", path)
	}
	return path, nil
}

// GetBranch returns the configured branch or DefaultBranch as default.
func (c *Config) GetBranch() string {
	if c.Branch == "" {
		return DefaultBranch
	}
	return c.Branch
}

// GetArgoCDAuth returns the ArgoCD authentication config.
// Falls back to Auth if ArgoCDAuth is not specified.
func (c *Config) GetArgoCDAuth() *AuthConfig {
	if c.ArgoCDAuth != nil {
		return c.ArgoCDAuth
	}
	return &c.Auth
}

// Validate checks that the auth configuration is valid.
func (a *AuthConfig) Validate() error {
	if a.SSHKeyEnv == "" && a.TokenEnv == "" {
		return fmt.Errorf("ssh_key_env or token_env is required")
	}

	if a.SSHKeyEnv != "" && a.TokenEnv != "" {
		return fmt.Errorf("only one of ssh_key_env or token_env should be set, not both")
	}

	return nil
}

// AuthType returns the type of authentication configured.
func (a *AuthConfig) AuthType() string {
	if a.SSHKeyEnv != "" {
		return "ssh"
	}
	if a.TokenEnv != "" {
		return "token"
	}
	return "none"
}

// GetSSHKey reads the SSH private key from the configured environment variable.
func (a *AuthConfig) GetSSHKey() (string, error) {
	if a.SSHKeyEnv == "" {
		return "", fmt.Errorf("ssh_key_env not configured")
	}

	key := os.Getenv(a.SSHKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set or empty", a.SSHKeyEnv)
	}

	return key, nil
}

// GetToken reads the token from the configured environment variable.
func (a *AuthConfig) GetToken() (string, error) {
	if a.TokenEnv == "" {
		return "", fmt.Errorf("token_env not configured")
	}

	token := os.Getenv(a.TokenEnv)
	if token == "" {
		return "", fmt.Errorf("environment variable %s is not set or empty", a.TokenEnv)
	}

	return token, nil
}

// RedactedCopy returns a copy of the Config with sensitive fields removed.
// The auth block is replaced with a placeholder, suitable for writing to
// the gitops repo without leaking credentials.
func (c *Config) RedactedCopy() *Config {
	return &Config{
		URL:    c.URL,
		Branch: c.Branch,
		Path:   c.Path,
		// Auth and ArgoCDAuth are intentionally omitted (zero values)
		// to prevent accidentally committing credentials
	}
}

// GetAuth returns the configured transport.AuthMethod for git operations.
// Implements CredentialProvider interface.
func (a *AuthConfig) GetAuth() (transport.AuthMethod, error) {
	switch a.AuthType() {
	case "ssh":
		sshKey, err := a.GetSSHKey()
		if err != nil {
			return nil, err
		}

		signer, err := cryptossh.ParsePrivateKey([]byte(sshKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
		}

		return &ssh.PublicKeys{
			User:   "git",
			Signer: signer,
			// Accept any host key - appropriate for automated systems
			// where we trust the configured repository URL.
			// This is intentional for CI/CD environments where known_hosts
			// may not be available or maintained.
			HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
				HostKeyCallback: cryptossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: Intentional for automated CI/CD systems
			},
		}, nil

	case "token":
		token, err := a.GetToken()
		if err != nil {
			return nil, err
		}

		return &http.BasicAuth{
			Username: "git",
			Password: token,
		}, nil

	default:
		return nil, fmt.Errorf("no authentication configured")
	}
}
