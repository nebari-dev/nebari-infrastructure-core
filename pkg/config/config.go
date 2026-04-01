package config

import (
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

// ValidateOptions configures validation behavior.
// Provider lists are injected by the caller (typically from a registry)
// to keep the config package decoupled from provider implementations.
type ValidateOptions struct {
	ClusterProviders []string
	DNSProviders     []string
}

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Domain      string `yaml:"domain,omitempty"`

	// KubeContext specifies an existing Kubernetes context to deploy to.
	// When set, this enables "bring your own cluster" mode - the provider's
	// infrastructure provisioning (Terraform) is skipped, but provider-specific
	// settings (storage classes, resource types) are still applied based on
	// the cluster provider. This allows deploying to pre-existing clusters.
	KubeContext string `yaml:"kube_context,omitempty"`

	// Cluster Provider configuration.
	// Only one provider can be configured at a time.
	Cluster *ClusterConfig `yaml:"cluster,omitempty"`

	// DNS provider configuration (optional).
	// Only one provider can be configured at a time.
	DNS *DNSConfig `yaml:"dns,omitempty"`

	// GitRepository configures the GitOps repository for ArgoCD bootstrap (optional)
	GitRepository *git.Config `yaml:"git_repository,omitempty"`

	// Certificate configuration (optional)
	Certificate *CertificateConfig `yaml:"certificate,omitempty"`

	// Runtime options (set by CLI, not from YAML file)
	DryRun  bool          `yaml:"-"` // Preview changes without applying them
	Force   bool          `yaml:"-"` // Continue destruction even if some resources fail to delete
	Timeout time.Duration `yaml:"-"` // Override default operation timeout
}

// DNSConfig holds typed DNS provider configuration.
// The provider name is the map key, the provider config is the map value.
// Example YAML:
//
//	dns:
//	  cloudflare:
//	    zone_name: example.com
type DNSConfig struct {
	// Providers captures the provider name as key and its config as value.
	Providers map[string]any `yaml:",inline"`
}

// Validate checks that exactly one valid DNS provider is configured.
// When validProviders is non-empty, the provider name is checked against the list.
func (d *DNSConfig) Validate(validProviders []string) error {
	if len(d.Providers) == 0 {
		return fmt.Errorf("dns block is present but no provider is configured")
	}
	if len(d.Providers) > 1 {
		return fmt.Errorf("only one DNS provider can be configured at a time")
	}
	name := d.ProviderName()
	if len(validProviders) > 0 && !slices.Contains(validProviders, name) {
		return fmt.Errorf("invalid DNS provider %q, must be one of: %v", name, validProviders)
	}
	if d.ProviderConfig() == nil {
		return fmt.Errorf("DNS provider %q config must be a mapping, not a scalar value", name)
	}
	return nil
}

// ProviderName returns the name of the configured DNS provider,
// or an empty string if none is configured.
// Precondition: Validate() ensures exactly one entry in the map.
func (d *DNSConfig) ProviderName() string {
	if d == nil {
		return ""
	}
	for name := range d.Providers {
		return name
	}
	return ""
}

// ProviderConfig returns the DNS provider config as a map.
// Returns nil if no provider is configured or the value is not a map.
// Precondition: Validate() ensures exactly one entry in the map.
func (d *DNSConfig) ProviderConfig() map[string]any {
	if d == nil {
		return nil
	}
	for _, v := range d.Providers {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return nil
	}
	return nil
}

// ClusterConfig holds typed cloud provider configuration.
// The provider name is the map key, the provider config is the map value.
// Example YAML:
//
//	cluster:
//	  aws:
//	    region: us-west-2
type ClusterConfig struct {
	// Providers captures the provider name as key and its config as value.
	Providers map[string]any `yaml:",inline"`
}

// Validate checks that exactly one valid cluster provider is configured.
// When validProviders is non-empty, the provider name is checked against the list.
func (c *ClusterConfig) Validate(validProviders []string) error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("cluster block is present but no provider is configured")
	}
	if len(c.Providers) > 1 {
		return fmt.Errorf("only one cluster provider can be configured at a time")
	}
	name := c.ProviderName()
	if len(validProviders) > 0 && !slices.Contains(validProviders, name) {
		return fmt.Errorf("invalid cluster provider %q, must be one of: %v", name, validProviders)
	}
	if c.ProviderConfig() == nil {
		return fmt.Errorf("cluster provider %q config must be a mapping, not a scalar value", name)
	}
	return nil
}

// ProviderName returns the name of the configured cluster provider,
// or an empty string if none is configured.
// Precondition: Validate() ensures exactly one entry in the map.
func (c *ClusterConfig) ProviderName() string {
	if c == nil {
		return ""
	}
	for name := range c.Providers {
		return name
	}
	return ""
}

// ProviderConfig returns the cluster provider config as a map.
// Returns nil if no provider is configured or the value is not a map.
// Precondition: Validate() ensures exactly one entry in the map.
func (c *ClusterConfig) ProviderConfig() map[string]any {
	if c == nil {
		return nil
	}
	for _, v := range c.Providers {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return nil
	}
	return nil
}

// CertificateConfig holds TLS certificate configuration
type CertificateConfig struct {
	// Type is the certificate type: "selfsigned" or "letsencrypt"
	Type string `yaml:"type,omitempty"`

	// ACME configuration for Let's Encrypt
	ACME *ACMEConfig `yaml:"acme,omitempty"`
}

// ACMEConfig holds ACME (Let's Encrypt) configuration
type ACMEConfig struct {
	// Email is the email address for Let's Encrypt registration
	Email string `yaml:"email"`

	// Server is the ACME server URL (defaults to Let's Encrypt production)
	// Use "https://acme-staging-v02.api.letsencrypt.org/directory" for testing
	Server string `yaml:"server,omitempty"`
}

// safeProjectName matches alphanumeric strings with hyphens and underscores.
// Used to validate ProjectName before it is used as a filesystem path component.
var safeProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// Validate checks that the configuration is valid.
// The opts parameter provides the set of valid provider names, injected by the caller.
// Returns an error describing the first validation failure encountered.
func (c *NebariConfig) Validate(opts ValidateOptions) error {
	if c.ProjectName == "" {
		return fmt.Errorf("project_name field is required")
	}
	if !safeProjectName.MatchString(c.ProjectName) {
		return fmt.Errorf("project_name %q contains invalid characters (must start with alphanumeric and contain only alphanumeric, hyphens, or underscores)", c.ProjectName)
	}

	if c.Cluster == nil {
		return fmt.Errorf("cluster field is required")
	}
	if err := c.Cluster.Validate(opts.ClusterProviders); err != nil {
		return fmt.Errorf("invalid cluster: %w", err)
	}

	if c.DNS != nil {
		if err := c.DNS.Validate(opts.DNSProviders); err != nil {
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
