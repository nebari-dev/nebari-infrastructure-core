package config

import "time"

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Provider    string `yaml:"provider"`
	Domain      string `yaml:"domain,omitempty"`

	// Provider-specific configurations (stored as interface{}, will be type-asserted by providers)
	AmazonWebServices   interface{} `yaml:"amazon_web_services,omitempty"`
	GoogleCloudPlatform interface{} `yaml:"google_cloud_platform,omitempty"`
	Azure               interface{} `yaml:"azure,omitempty"`
	Local               interface{} `yaml:"local,omitempty"`

	// DNS provider configuration (optional)
	DNSProvider string         `yaml:"dns_provider,omitempty"`
	DNS         map[string]any `yaml:"dns,omitempty"` // Dynamic DNS config parsed by specific provider

	// Additional fields can be added as needed
	// Using map to capture additional fields for lenient parsing
	AdditionalFields map[string]any `yaml:",inline"`

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
