package config

import "fmt"

type ClusterConfig struct {
	// Providers captures the provider name as key and its config as value.
	Providers map[string]any `yaml:",inline"`
}

// Validate checks that exactly one valid DNS provider is configured.
func (c *ClusterConfig) Validate(validProviders []string) error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("cluster provider block is present but no provider is configured")
	}
	if len(c.Providers) > 1 {
		return fmt.Errorf("only one Cluster provider can be configured at a time")
	}
	name := c.ProviderName()
	if !isValidProvider(name, validProviders) {
		return fmt.Errorf("invalid DNS provider %q, must be one of: %v", name, validProviders)
	}
	if c.ProviderConfig() == nil {
		return fmt.Errorf("DNS provider %q config must be a mapping, not a scalar value", name)
	}
	return nil
}

// ProviderName returns the name of the configured Cluster provider,
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
