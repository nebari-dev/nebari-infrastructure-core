package azure

// Config represents Azure-specific configuration
type Config struct {
	Region                  string               `yaml:"region"`
	KubernetesVersion       string               `yaml:"kubernetes_version,omitempty"`
	StorageAccountPostfix   string               `yaml:"storage_account_postfix"`
	AuthorizedIPRanges      []string             `yaml:"authorized_ip_ranges,omitempty"`
	ResourceGroupName       string               `yaml:"resource_group_name,omitempty"`
	NodeResourceGroupName   string               `yaml:"node_resource_group_name,omitempty"`
	NodeGroups              map[string]NodeGroup `yaml:"node_groups,omitempty"`
	VnetSubnetID            string               `yaml:"vnet_subnet_id,omitempty"`
	PrivateClusterEnabled   bool                 `yaml:"private_cluster_enabled,omitempty"`
	Tags                    map[string]string    `yaml:"tags,omitempty"`
	NetworkProfile          map[string]string    `yaml:"network_profile,omitempty"`
	MaxPods                 int                  `yaml:"max_pods,omitempty"`
	WorkloadIdentityEnabled bool                 `yaml:"workload_identity_enabled,omitempty"`
	AzurePolicyEnabled      bool                 `yaml:"azure_policy_enabled,omitempty"`
	AdditionalFields        map[string]any       `yaml:",inline"`
}

// NodeGroup represents Azure-specific node group configuration
type NodeGroup struct {
	Instance string  `yaml:"instance"`
	MinNodes int     `yaml:"min_nodes,omitempty"`
	MaxNodes int     `yaml:"max_nodes,omitempty"`
	Taints   []Taint `yaml:"taints,omitempty"`
}

// Taint represents a Kubernetes taint
type Taint struct {
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
	Effect string `yaml:"effect"` // NoSchedule, PreferNoSchedule, NoExecute
}
