package config

import (
	"fmt"
	"regexp"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

type ValidProviders struct {
	ClusterProviders []string
	DNSProviders     []string
	GitProviders     []string
	CertProviders    []string
}

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Provider    string `yaml:"provider"`
	Domain      string `yaml:"domain,omitempty"`

	// KubeContext specifies an existing Kubernetes context to deploy to.
	// When set, this enables "bring your own cluster" mode - the provider's
	// infrastructure provisioning (Terraform) is skipped, but provider-specific
	// settings (storage classes, resource types) are still applied based on
	// the provider field. This allows deploying to pre-existing clusters.
	KubeContext string `yaml:"kube_context,omitempty"`

	// DNS provider configuration (optional).
	// Only one provider can be configured at a time.
	DNS *DNSConfig `yaml:"dns,omitempty"`

	// GitRepository configures the GitOps repository for ArgoCD bootstrap (optional)
	GitRepository *git.Config `yaml:"git_repository,omitempty"`

	// ProviderConfig captures provider-specific configuration via inline YAML.
	// Each provider extracts its config using its config key, e.g.:
	//   cfg.ProviderConfig["amazon_web_services"]
	// Reading from a nil map is safe in Go (returns nil), so no getter needed.
	// Extra YAML fields are captured here and safely ignored (forward compatibility).
	ProviderConfig map[string]any `yaml:",inline"`
	// Certificate configuration (optional)
	Certificate *CertificateConfig `yaml:"certificate,omitempty"`

	// Runtime options (set by CLI, not from YAML file)
	DryRun  bool          `yaml:"-"` // Preview changes without applying them
	Force   bool          `yaml:"-"` // Continue destruction even if some resources fail to delete
	Timeout time.Duration `yaml:"-"` // Override default operation timeout
}

// safeProjectName matches alphanumeric strings with hyphens and underscores.
// Used to validate ProjectName before it is used as a filesystem path component.
var safeProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Validate checks that the configuration is valid.
// Returns an error describing the first validation failure encountered.
func (c *NebariConfig) Validate(validProviders ValidProviders) error {
	if c.ProjectName == "" {
		return fmt.Errorf("project_name field is required")
	}
	if !safeProjectName.MatchString(c.ProjectName) {
		return fmt.Errorf("project_name %q contains invalid characters (must start with alphanumeric and contain only alphanumeric, hyphens, or underscores)", c.ProjectName)
	}

	if c.Provider == "" {
		return fmt.Errorf("provider field is required")
	}

	if len(validProviders.ClusterProviders) > 0 && !isValidProvider(c.Provider, validProviders.ClusterProviders) {
		return fmt.Errorf("invalid cluster provider %q, must be one of: %v", c.Provider, validProviders.ClusterProviders)
	}

	// Check for old-format dns_provider field
	if _, ok := c.ProviderConfig["dns_provider"]; ok {
		return fmt.Errorf("'dns_provider' is no longer supported; use nested dns block format instead:\n  dns:\n    cloudflare:\n      zone_name: example.com")
	}

	if c.DNS != nil {
		if err := c.DNS.Validate(validProviders.DNSProviders); err != nil {
			return fmt.Errorf("invalid dns: %w", err)
		}
	}

	if c.GitRepository != nil {
		if err := c.GitRepository.Validate(); err != nil {
			return fmt.Errorf("invalid git_repository: %w", err)
		}
	}

	return nil
}

// GetKubeContext returns the effective Kubernetes context to use.
// It checks the top-level kube_context first, which enables "bring your own cluster" mode.
// Returns an empty string if no context is specified (provider will create the cluster).
func (c *NebariConfig) GetKubeContext() string {
	return c.KubeContext
}

// IsExistingCluster returns true if deploying to an existing cluster
// (i.e., kube_context is specified at the top level).
// When true, infrastructure provisioning should be skipped.
func (c *NebariConfig) IsExistingCluster() bool {
	return c.KubeContext != ""
}

// isValidProvider checks if the given provider name is in the list of valid providers.
func isValidProvider(name string, validProviders []string) bool {
	for _, p := range validProviders {
		if p == name {
			return true
		}
	}
	return false
}
