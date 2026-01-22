package git

import (
	"fmt"
	"os"
)

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

// AuthConfig specifies authentication credentials for git operations.
// Only one of SSHKeyEnv or TokenEnv should be set.
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

// GetBranch returns the configured branch or "main" as default.
func (c *Config) GetBranch() string {
	if c.Branch == "" {
		return "main"
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
