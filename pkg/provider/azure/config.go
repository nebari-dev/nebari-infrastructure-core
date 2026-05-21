package azure

// Config is the user-facing Azure cluster configuration as parsed from the
// `cluster.azure:` block of NIC YAML.
type Config struct {
	Region                string               `yaml:"region"`
	ResourceGroupName     string               `yaml:"resource_group_name,omitempty"`
	CreateResourceGroup   *bool                `yaml:"create_resource_group,omitempty"`
	KubernetesVersion     string               `yaml:"kubernetes_version,omitempty"`
	SKUTier               string               `yaml:"sku_tier,omitempty"`
	PrivateClusterEnabled bool                 `yaml:"private_cluster_enabled,omitempty"`
	AuthorizedIPRanges    []string             `yaml:"authorized_ip_ranges,omitempty"`
	Network               *NetworkConfig       `yaml:"network,omitempty"`
	NodeGroups            map[string]NodeGroup `yaml:"node_groups"`
	Tags                  map[string]string    `yaml:"tags,omitempty"`
}

// NetworkConfig groups all VNet/subnet/CIDR knobs.
type NetworkConfig struct {
	VNetCIDRBlock        string `yaml:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock  string `yaml:"node_subnet_cidr_block,omitempty"`
	PodCIDR              string `yaml:"pod_cidr,omitempty"`
	ServiceCIDR          string `yaml:"service_cidr,omitempty"`
	DNSServiceIP         string `yaml:"dns_service_ip,omitempty"`
	ExistingVNetID       string `yaml:"existing_vnet_id,omitempty"`
	ExistingNodeSubnetID string `yaml:"existing_node_subnet_id,omitempty"`
}

// NodeGroup describes one AKS node pool.
type NodeGroup struct {
	Instance     string            `yaml:"instance"`
	MinNodes     int               `yaml:"min_nodes"`
	MaxNodes     int               `yaml:"max_nodes"`
	Mode         string            `yaml:"mode,omitempty"` // "System" | "User"; defaults to "User"
	OSDiskSizeGB int               `yaml:"os_disk_size_gb,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty"`
	Taints       []string          `yaml:"taints,omitempty"`
	Zones        []string          `yaml:"zones,omitempty"`
}
