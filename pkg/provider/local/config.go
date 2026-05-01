package local

// Config represents local provider configuration
type Config struct {
	KubeContext      string                       `yaml:"kube_context,omitempty"`
	NodeSelectors    map[string]map[string]string `yaml:"node_selectors,omitempty"`
	StorageClass     string                       `yaml:"storage_class,omitempty"`
	HTTPSPort        int                          `yaml:"https_port,omitempty"`
	MetalLB          *MetalLBConfig               `yaml:"metallb,omitempty"`
	AdditionalFields map[string]any               `yaml:",inline"`
}

// MetalLBConfig holds MetalLB-specific settings for the local provider.
type MetalLBConfig struct {
	// Enabled controls whether MetalLB is deployed. Default: true.
	// Use a pointer to distinguish "not set" (default true) from "explicitly false".
	Enabled *bool `yaml:"enabled,omitempty"`

	// AddressPool is the IP range for MetalLB's IPAddressPool.
	// Default: "192.168.1.100-192.168.1.110"
	AddressPool string `yaml:"address_pool,omitempty"`
}
