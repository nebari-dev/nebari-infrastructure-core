package config

// TODO: implement cert provider pattern and support multiple cert providers like DNS providers
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
