package config

import (
	"fmt"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Provider    string `yaml:"provider"`
	Domain      string `yaml:"domain,omitempty"`

	// DNS provider configuration (optional)
	DNSProvider string         `yaml:"dns_provider,omitempty"`
	DNS         map[string]any `yaml:"dns,omitempty"` // Dynamic DNS config parsed by specific provider

	// GitRepository configures the GitOps repository for ArgoCD bootstrap (optional)
	GitRepository *git.Config `yaml:"git_repository,omitempty"`

	// ProviderConfig captures provider-specific configuration via inline YAML.
	// Each provider extracts its config using its config key, e.g.:
	//   cfg.ProviderConfig["amazon_web_services"]
	// Reading from a nil map is safe in Go (returns nil), so no getter needed.
	// Extra YAML fields are captured here and safely ignored (forward compatibility).
	ProviderConfig map[string]any `yaml:",inline"`

	// Runtime options (set by CLI, not from YAML file)
	DryRun  bool          `yaml:"-"` // Preview changes without applying them
	Force   bool          `yaml:"-"` // Continue destruction even if some resources fail to delete
	Timeout time.Duration `yaml:"-"` // Override default operation timeout
}

// ValidProviders lists the supported providers
var ValidProviders = []string{"aws", "gcp", "azure", "local"}

// IsValidProvider checks if the provider string is valid
func IsValidProvider(provider string) bool {
	for _, p := range ValidProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// Validate checks that the configuration is valid.
// Returns an error describing the first validation failure encountered.
func (c *NebariConfig) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider field is required")
	}

	if !IsValidProvider(c.Provider) {
		return fmt.Errorf("invalid provider %q, must be one of: %v", c.Provider, ValidProviders)
	}

	if c.GitRepository != nil {
		if err := c.GitRepository.Validate(); err != nil {
			return fmt.Errorf("invalid git_repository: %w", err)
		}
	}

	return nil
}
