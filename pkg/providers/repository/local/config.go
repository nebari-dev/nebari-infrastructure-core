package local

import (
	"fmt"
	"path/filepath"
)

// Config holds the configuration for the local repository provider.
type Config struct {
	// Path is the directory of the repository. When empty, the provider
	// defaults to ~/.nic/gitops/<project_name>, falling back to a per-project
	// directory under the OS temp dir only when the home directory cannot be
	// resolved.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Branch is the git branch to use (default: "main").
	Branch string `yaml:"branch,omitempty" json:"branch,omitempty"`
}

// Validate checks that the configuration is well-formed.
func (c *Config) Validate() error {
	if c.Path != "" && !filepath.IsAbs(c.Path) {
		return fmt.Errorf("path must be an absolute directory, got: %s", c.Path)
	}
	return nil
}
