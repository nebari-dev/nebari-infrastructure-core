package local

// Config represents local provider configuration
type Config struct {
	Kind          *KindConfig                  `yaml:"kind,omitempty"`
	NodeSelectors map[string]map[string]string `yaml:"node_selectors,omitempty"`
	// HTTPSPort is the host port the gateway's HTTPS listener is published on
	// (default 443). Override it when 443 is taken on the host or when running
	// several local clusters side by side. Takes effect on cluster creation
	// only. kind port mappings cannot be changed on an existing cluster.
	HTTPSPort int `yaml:"https_port,omitempty"`
	// HTTPPort is the host port the gateway's HTTP listener (the HTTPS
	// redirect) is published on (default 80). Override it under the same
	// circumstances as HTTPSPort, including rootless container runtimes that
	// cannot bind ports below 1024. Takes effect on cluster creation only.
	HTTPPort         int            `yaml:"http_port,omitempty"`
	AdditionalFields map[string]any `yaml:",inline"`
}

// KindConfig holds optional config for the deployed kind cluster. It may be
// omitted entirely (nil), in which case the cluster is created with defaults.
type KindConfig struct {
	// NodeImage is the kindest/node image to use (e.g. "kindest/node:v1.32.2").
	// Empty means the default image of the bundled kind version.
	NodeImage string `yaml:"node_image,omitempty"`

	// ExtraMounts are additional host directories mounted into the cluster node
	// container. NIC mounts its auto-created local GitOps repository
	// automatically; an explicit file:// repository needs a matching entry here.
	// Other custom mounts are user-managed, and NIC does not recursively
	// normalize their permissions.
	ExtraMounts []KindMount `yaml:"extra_mounts,omitempty"`
}

// KindMount mounts a host directory into the kind node container.
type KindMount struct {
	HostPath      string `yaml:"host_path"`
	ContainerPath string `yaml:"container_path"`
	ReadOnly      bool   `yaml:"read_only,omitempty"`
}
