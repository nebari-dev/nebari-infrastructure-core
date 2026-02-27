package config

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider/cloudflare"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

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

// DNSConfig holds typed DNS provider configuration.
// Only one provider field should be set at a time.
type DNSConfig struct {
	Cloudflare *cloudflare.Config `yaml:"cloudflare,omitempty"`
}

// ProviderName returns the name of the configured DNS provider,
// or an empty string if none is configured.
func (d *DNSConfig) ProviderName() string {
	if d.Cloudflare != nil {
		return "cloudflare"
	}
	return ""
}

// ProviderConfig returns the DNS provider config as a map for use with the provider
// interface.
func (d *DNSConfig) ProviderConfig() map[string]any {
	var v any
	switch {
	case d.Cloudflare != nil:
		v = d.Cloudflare
	default:
		return nil
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
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

// Validate checks that exactly one DNS provider is configured.
func (d *DNSConfig) Validate() error {
	count := 0
	if d.Cloudflare != nil {
		count++
	}
	// Add future providers here: if d.Route53 != nil { count++ }

	if count == 0 {
		return fmt.Errorf("dns block is present but no provider is configured")
	}
	if count > 1 {
		return fmt.Errorf("only one DNS provider can be configured at a time")
	}
	return nil
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

	if c.DNS != nil {
		if err := c.DNS.Validate(); err != nil {
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
