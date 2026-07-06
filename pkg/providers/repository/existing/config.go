package existing

import (
	"fmt"
	"os"
	"strings"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// Config holds the configuration for the existing repository provider.
type Config struct {
	// URL is the remote repository URL (ssh or https).
	// Examples: "git@github.com:org/repo.git", "https://github.com/org/repo.git".
	URL string `yaml:"url" json:"url"`

	// Branch is the git branch to use (default: "main").
	Branch string `yaml:"branch" json:"branch"`

	// Path is an optional subdirectory within the repository. When set, all
	// operations are scoped to this path.
	Path string `yaml:"path" json:"path"`

	// Auth specifies the credentials NIC uses to push to the repository
	// (requires write access).
	Auth AuthConfig `yaml:"auth" json:"auth"`

	// ArgoCDAuth specifies optional separate credentials for ArgoCD's in-cluster
	// read access. When unset, Auth is used.
	ArgoCDAuth *AuthConfig `yaml:"argocd_auth,omitempty" json:"argocd_auth,omitempty"`
}

// AuthConfig selects exactly one authentication method. Each method names the
// environment variable its secret is read from. Example:
//
//	auth:
//	  token:
//	    env: GIT_TOKEN
//
//	auth:
//	  ssh:
//	    env: GIT_SSH_KEY
type AuthConfig struct {
	// Token authenticates over HTTPS with a token read from Token.Env.
	Token *EnvRef `yaml:"token,omitempty" json:"token,omitempty"`

	// SSH authenticates over SSH with a private key read from SSH.Env.
	SSH *EnvRef `yaml:"ssh,omitempty" json:"ssh,omitempty"`
}

// EnvRef names the environment variable a secret is read from.
type EnvRef struct {
	Env string `yaml:"env" json:"env"`
}

// Validate checks that the configuration is well-formed.
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("repository url is required")
	}
	if strings.HasPrefix(c.URL, "file://") {
		return fmt.Errorf("the existing provider is for remote repositories; use the local provider for a file:// directory")
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

// GetArgoCDAuth returns the ArgoCD auth config, falling back to Auth.
func (c *Config) GetArgoCDAuth() *AuthConfig {
	if c.ArgoCDAuth != nil {
		return c.ArgoCDAuth
	}
	return &c.Auth
}

// Validate checks that exactly one auth method is configured and complete.
func (a *AuthConfig) Validate() error {
	switch {
	case a.Token == nil && a.SSH == nil:
		return fmt.Errorf("one of token or ssh is required")
	case a.Token != nil && a.SSH != nil:
		return fmt.Errorf("only one of token or ssh may be set")
	case a.Token != nil && a.Token.Env == "":
		return fmt.Errorf("token.env is required")
	case a.SSH != nil && a.SSH.Env == "":
		return fmt.Errorf("ssh.env is required")
	}
	return nil
}

// resolve reads the configured environment variable and returns the resolved
// auth.
func (a *AuthConfig) resolve() (repository.Auth, error) {
	switch {
	case a.Token != nil:
		v := os.Getenv(a.Token.Env)
		if v == "" {
			return nil, fmt.Errorf("environment variable %s is not set or empty", a.Token.Env)
		}
		return repository.TokenAuth{Token: v}, nil
	case a.SSH != nil:
		v := os.Getenv(a.SSH.Env)
		if v == "" {
			return nil, fmt.Errorf("environment variable %s is not set or empty", a.SSH.Env)
		}
		return repository.SSHKeyAuth{Key: v}, nil
	default:
		return nil, fmt.Errorf("no auth method configured")
	}
}
