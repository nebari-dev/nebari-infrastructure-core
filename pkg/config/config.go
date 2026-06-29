package config

import (
	"fmt"
	"regexp"
	"slices"
)

// ValidateOptions configures validation behavior.
// Provider lists are injected by the caller (typically from a registry)
// to keep the config package decoupled from provider implementations.
type ValidateOptions struct {
	ClusterProviders []string
	DNSProviders     []string
	RepoProviders    []string
}

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Domain      string `yaml:"domain,omitempty"`

	// Cluster Provider configuration.
	// Only one provider can be configured at a time.
	Cluster *ClusterConfig `yaml:"cluster,omitempty"`

	// DNS provider configuration (optional).
	// Only one provider can be configured at a time.
	DNS *DNSConfig `yaml:"dns,omitempty"`

	// Repo configures the GitOps repository provider (optional).
	// Only one provider can be configured at a time.
	Repo *RepoConfig `yaml:"repo,omitempty"`

	// Certificate configuration (optional)
	Certificate *CertificateConfig `yaml:"certificate,omitempty"`
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

// RepoConfig holds typed GitOps repository provider configuration.
// The provider name is the map key, the provider config is the map value.
// Example YAML:
//
//	repo:
//	  existing:
//	    url: "git@github.com:my-org/my-gitops-repo.git"
//	    branch: main
type RepoConfig struct {
	// Providers captures the provider name as key and its config as value.
	Providers map[string]any `yaml:",inline"`
}

// Validate checks that exactly one valid repo provider is configured.
// When validProviders is non-empty, the provider name is checked against the list.
func (r *RepoConfig) Validate(validProviders []string) error {
	if len(r.Providers) == 0 {
		return fmt.Errorf("repo block is present but no provider is configured")
	}
	if len(r.Providers) > 1 {
		return fmt.Errorf("only one repo provider can be configured at a time")
	}
	name := r.ProviderName()
	if len(validProviders) > 0 && !slices.Contains(validProviders, name) {
		return fmt.Errorf("invalid repo provider %q, must be one of: %v", name, validProviders)
	}
	if r.ProviderConfig() == nil {
		return fmt.Errorf("repo provider %q config must be a mapping, not a scalar value", name)
	}
	return nil
}

// ProviderName returns the name of the configured repo provider,
// or an empty string if none is configured.
// Precondition: Validate() ensures exactly one entry in the map.
func (r *RepoConfig) ProviderName() string {
	if r == nil {
		return ""
	}
	for name := range r.Providers {
		return name
	}
	return ""
}

// ProviderConfig returns the repo provider config as a map.
// Returns nil if no provider is configured or the value is not a map.
// Precondition: Validate() ensures exactly one entry in the map.
func (r *RepoConfig) ProviderConfig() map[string]any {
	if r == nil {
		return nil
	}
	for _, v := range r.Providers {
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

	if c.Repo == nil {
		return fmt.Errorf("repo field is required")
	}
	if err := c.Repo.Validate(opts.RepoProviders); err != nil {
		return fmt.Errorf("invalid repo: %w", err)
	}

	return nil
}
