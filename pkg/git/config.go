package git

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/skeema/knownhosts"
	cryptossh "golang.org/x/crypto/ssh"
)

const (
	// DefaultBranch is the default git branch name when none is specified.
	DefaultBranch = "main"
)

// DefaultLocalPath returns the host directory NIC manages for a project's
// local gitops repository when no git_repository is configured. It is a pure
// function so the deploy orchestrator and providers that mount the directory
// (e.g. the local kind provider) can derive the same path independently
// without threading it between them.
func DefaultLocalPath(projectName string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("nebari-gitops-%s", projectName))
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

	// InsecureSkipHostKeyVerification disables SSH host key verification,
	// removing protection against man-in-the-middle attacks. Only intended
	// for ephemeral environments (e.g. CI) where maintaining a known_hosts
	// file is impractical. Has no effect on token (HTTPS) authentication.
	InsecureSkipHostKeyVerification bool `yaml:"insecure_skip_host_key_verification,omitempty" json:"insecure_skip_host_key_verification,omitempty"`
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

		if a.InsecureSkipHostKeyVerification {
			return &ssh.PublicKeys{
				User:   "git",
				Signer: signer,
				HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
					HostKeyCallback: cryptossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: explicit opt-in via insecure_skip_host_key_verification
				},
			}, nil
		}

		callback, err := newHostKeyCallback()
		if err != nil {
			return nil, err
		}

		return &ssh.PublicKeys{
			User:   "git",
			Signer: signer,
			HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
				HostKeyCallback: callback,
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

// newHostKeyCallback returns a host key callback backed by the standard
// known_hosts files (SSH_KNOWN_HOSTS, ~/.ssh/known_hosts, /etc/ssh/ssh_known_hosts),
// wrapping verification failures with actionable guidance.
func newHostKeyCallback() (cryptossh.HostKeyCallback, error) {
	callback, err := ssh.NewKnownHostsCallback()
	if err != nil {
		return nil, fmt.Errorf("ssh host key verification requires a known_hosts file: %w\n"+
			"connect to the git host once with your SSH client (e.g. `ssh git@github.com`) to record its key, "+
			"or set insecure_skip_host_key_verification: true under git_repository auth to disable verification (not recommended)", err)
	}

	return func(hostname string, remote net.Addr, key cryptossh.PublicKey) error {
		err := callback(hostname, remote, key)
		host := strings.TrimSuffix(hostname, ":22")
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostUnknown(err):
			return fmt.Errorf("ssh host key verification failed: %s is not in known_hosts\n"+
				"to trust this host, connect to it once with your SSH client (e.g. `ssh git@%s`) and accept its key, "+
				"or set insecure_skip_host_key_verification: true under git_repository auth to disable verification (not recommended)", host, host)
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("ssh host key verification failed: the key presented by %s does not match known_hosts\n"+
				"this could indicate a man-in-the-middle attack; if the host key legitimately changed, "+
				"remove the old entry (`ssh-keygen -R %s`) and connect once to record the new one: %w", host, host, err)
		default:
			return err
		}
	}, nil
}
