package local

// Config represents local provider configuration
type Config struct {
	Kind             *KindConfig                  `yaml:"kind,omitempty"`
	NodeSelectors    map[string]map[string]string `yaml:"node_selectors,omitempty"`
	HTTPSPort        int                          `yaml:"https_port,omitempty"`
	MetalLB          *MetalLBConfig               `yaml:"metallb,omitempty"`
	AdditionalFields map[string]any               `yaml:",inline"`
}

// KindConfig holds optional confg for the deployed kind cluster. It may be
// omitted entirely (nil), in which case the cluster is created with defaults.
type KindConfig struct {
	// NodeImage is the kindest/node image to use (e.g. "kindest/node:v1.32.2").
	// Empty means the default image of the bundled kind version.
	NodeImage string `yaml:"node_image,omitempty"`

	// ExtraMounts are additional host directories mounted into the cluster
	// node container. The local file:// gitops repository (explicit or
	// auto-created) is mounted automatically and does not need to be listed.
	ExtraMounts []KindMount `yaml:"extra_mounts,omitempty"`
}

// KindMount mounts a host directory into the kind node container.
type KindMount struct {
	HostPath      string `yaml:"host_path"`
	ContainerPath string `yaml:"container_path"`
	ReadOnly      bool   `yaml:"read_only,omitempty"`
}

// MetalLBConfig holds MetalLB-specific settings for the local provider.
// MetalLB is always enabled on local clusters — kind has no built-in
// LoadBalancer, so disabling it would leave the gateway without an IP.
type MetalLBConfig struct {
	// AddressPool is the IP range for MetalLB's IPAddressPool. When unset, NIC
	// derives a pool from the kind Docker network during Deploy.
	AddressPool string `yaml:"address_pool,omitempty"`
}
