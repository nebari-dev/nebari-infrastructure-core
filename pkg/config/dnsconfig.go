package config

import "fmt"

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
func (d *DNSConfig) Validate(validProviders []string) error {
	if len(d.Providers) == 0 {
		return fmt.Errorf("dns block is present but no provider is configured")
	}
	if len(d.Providers) > 1 {
		return fmt.Errorf("only one DNS provider can be configured at a time")
	}
	name := d.ProviderName()
	if !isValidProvider(name, validProviders) {
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
