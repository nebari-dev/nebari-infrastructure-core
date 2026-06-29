package config

import (
	"fmt"
	"regexp"
	"slices"

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

	// Backups configures off-cluster backup scheduling (Longhorn). Optional.
	Backups *BackupsConfig `yaml:"backups,omitempty"`
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

// Certificate type values accepted in CertificateConfig.Type.
const (
	// CertificateTypeSelfSigned mints a cert via cert-manager's self-signed ClusterIssuer.
	CertificateTypeSelfSigned = "selfsigned"
	// CertificateTypeLetsEncrypt mints a cert via cert-manager's ACME (Let's Encrypt) ClusterIssuer.
	CertificateTypeLetsEncrypt = "letsencrypt"
	// CertificateTypeExisting uses a user-supplied TLS certificate instead of cert-manager.
	CertificateTypeExisting = "existing"

	// DefaultGatewayTLSSecretName is the default name of the TLS secret the gateway references.
	// The cert is the gateway's, shared across argocd / keycloak / the apex domain.
	DefaultGatewayTLSSecretName = "nebari-gateway-tls"
	// DefaultGatewayTLSNamespace is the namespace the gateway expects its TLS secret in.
	DefaultGatewayTLSNamespace = "envoy-gateway-system"
)

// CertificateConfig holds TLS certificate configuration
type CertificateConfig struct {
	// Type is the certificate type: "selfsigned", "letsencrypt", or "existing"
	Type string `yaml:"type,omitempty"`

	// ACME configuration for Let's Encrypt
	ACME *ACMEConfig `yaml:"acme,omitempty"`

	// SecretName overrides the name of the TLS secret the gateway references.
	// Defaults to "nebari-gateway-tls". For type=existing with existing_secret,
	// the gateway references ExistingSecret.Name instead.
	SecretName string `yaml:"secret_name,omitempty"`

	// ExistingSecret references a kubernetes.io/tls secret the user already created.
	// Mutually exclusive with Files and Env. Only valid when Type=existing.
	ExistingSecret *ExistingSecretRef `yaml:"existing_secret,omitempty"`

	// Files reads PEM material from disk; NIC creates the secret directly.
	// Mutually exclusive with ExistingSecret and Env. Only valid when Type=existing.
	Files *CertFiles `yaml:"files,omitempty"`

	// Env reads raw PEM material from environment variables; NIC creates the secret directly.
	// Mutually exclusive with ExistingSecret and Files. Only valid when Type=existing.
	Env *CertEnv `yaml:"env,omitempty"`
}

// ExistingSecretRef references a pre-existing TLS secret.
type ExistingSecretRef struct {
	// Name is the secret name (required).
	Name string `yaml:"name"`
	// Namespace is the secret's namespace. Defaults to envoy-gateway-system.
	Namespace string `yaml:"namespace,omitempty"`
}

// CertFiles points at PEM cert/key files on disk.
type CertFiles struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// CertEnv names environment variables holding raw (non-base64) PEM material.
type CertEnv struct {
	CertEnv string `yaml:"cert_env"`
	KeyEnv  string `yaml:"key_env"`
}

// Validate checks the certificate configuration. A nil receiver is valid
// (no certificate block configured).
func (c *CertificateConfig) Validate() error {
	if c == nil {
		return nil
	}
	switch c.Type {
	case "", CertificateTypeSelfSigned, CertificateTypeLetsEncrypt:
		return nil
	case CertificateTypeExisting:
		return c.validateExisting()
	default:
		return fmt.Errorf("invalid certificate type %q, must be one of: %s, %s, %s",
			c.Type, CertificateTypeSelfSigned, CertificateTypeLetsEncrypt, CertificateTypeExisting)
	}
}

// validateExisting validates the type=existing variant: exactly one source,
// complete file/env pairs, and no ACME combination.
func (c *CertificateConfig) validateExisting() error {
	if c.ACME != nil {
		return fmt.Errorf("certificate.acme cannot be combined with type=existing")
	}

	sources := 0
	if c.ExistingSecret != nil {
		sources++
	}
	if c.Files != nil {
		sources++
	}
	if c.Env != nil {
		sources++
	}
	if sources != 1 {
		return fmt.Errorf("certificate type=existing requires exactly one of existing_secret, files, or env (found %d)", sources)
	}

	if c.ExistingSecret != nil {
		if c.ExistingSecret.Name == "" {
			return fmt.Errorf("certificate.existing_secret.name is required")
		}
		if c.SecretName != "" {
			return fmt.Errorf("certificate.secret_name cannot be combined with existing_secret (the gateway references existing_secret.name directly)")
		}
	}
	if c.Files != nil && (c.Files.CertFile == "" || c.Files.KeyFile == "") {
		return fmt.Errorf("certificate.files requires both cert_file and key_file")
	}
	if c.Env != nil && (c.Env.CertEnv == "" || c.Env.KeyEnv == "") {
		return fmt.Errorf("certificate.env requires both cert_env and key_env")
	}
	return nil
}

// ResolvedSecretName returns the name of the TLS secret NIC creates (files/env
// sources) or, for selfsigned/letsencrypt, the cert-manager secret name.
// Defaults to DefaultGatewayTLSSecretName.
func (c *CertificateConfig) ResolvedSecretName() string {
	if c != nil && c.SecretName != "" {
		return c.SecretName
	}
	return DefaultGatewayTLSSecretName
}

// GatewaySecretRef returns the (name, namespace) the gateway listener should
// reference. For existing_secret it points at the user's secret; otherwise it
// is ResolvedSecretName in DefaultGatewayTLSNamespace.
func (c *CertificateConfig) GatewaySecretRef() (name, namespace string) {
	namespace = DefaultGatewayTLSNamespace
	name = c.ResolvedSecretName()
	if c != nil && c.Type == CertificateTypeExisting && c.ExistingSecret != nil {
		name = c.ExistingSecret.Name
		if c.ExistingSecret.Namespace != "" {
			namespace = c.ExistingSecret.Namespace
		}
	}
	return name, namespace
}

// IsCrossNamespaceSecret reports whether the gateway references a TLS secret in
// a namespace other than DefaultGatewayTLSNamespace, which requires a ReferenceGrant.
func (c *CertificateConfig) IsCrossNamespaceSecret() bool {
	_, namespace := c.GatewaySecretRef()
	return namespace != DefaultGatewayTLSNamespace
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

	if err := c.Certificate.Validate(); err != nil {
		return fmt.Errorf("invalid certificate: %w", err)
	}

	if err := c.Backups.Validate(c.Cluster.ProviderName()); err != nil {
		return fmt.Errorf("invalid backups: %w", err)
	}

	return nil
}
