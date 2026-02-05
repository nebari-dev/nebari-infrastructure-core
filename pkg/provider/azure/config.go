package azure

// Config represents Azure-specific configuration for deploying Nebari on Azure Kubernetes Service.
type Config struct {
	// Region is the Azure region (e.g., eastus, westeurope)
	Region string `yaml:"region"`
	// KubernetesVersion is the AKS Kubernetes version (e.g., 1.28, 1.29)
	KubernetesVersion string `yaml:"kubernetes_version,omitempty"`
	// StorageAccountPostfix is appended to create unique storage account names
	StorageAccountPostfix string `yaml:"storage_account_postfix"`
	// AuthorizedIPRanges restricts API server access to specific CIDRs
	AuthorizedIPRanges []string `yaml:"authorized_ip_ranges,omitempty"`
	// ResourceGroupName specifies an existing resource group (created if not specified)
	ResourceGroupName string `yaml:"resource_group_name,omitempty"`
	// NodeResourceGroupName is the resource group for AKS node resources
	NodeResourceGroupName string `yaml:"node_resource_group_name,omitempty"`
	// NodeGroups defines the AKS node pools
	NodeGroups map[string]NodeGroup `yaml:"node_groups,omitempty"`
	// VnetSubnetID specifies an existing subnet for AKS nodes
	VnetSubnetID string `yaml:"vnet_subnet_id,omitempty"`
	// PrivateClusterEnabled makes the API server only accessible from private networks
	PrivateClusterEnabled bool `yaml:"private_cluster_enabled,omitempty"`
	// Tags are Azure resource tags applied to all created resources
	Tags map[string]string `yaml:"tags,omitempty"`
	// NetworkProfile configures AKS networking (network_plugin, network_policy, etc.)
	NetworkProfile map[string]string `yaml:"network_profile,omitempty"`
	// MaxPods is the maximum number of pods per node (default: 110)
	MaxPods int `yaml:"max_pods,omitempty"`
	// WorkloadIdentityEnabled enables Azure Workload Identity for pod authentication
	WorkloadIdentityEnabled bool `yaml:"workload_identity_enabled,omitempty"`
	// AzurePolicyEnabled enables Azure Policy for AKS
	AzurePolicyEnabled bool `yaml:"azure_policy_enabled,omitempty"`
	// AdditionalFields captures any extra Azure-specific configuration
	AdditionalFields map[string]any `yaml:",inline"`
}

// NodeGroup represents an AKS node pool configuration.
type NodeGroup struct {
	// Instance is the Azure VM size (e.g., Standard_D4s_v3, Standard_NC6s_v3)
	Instance string `yaml:"instance"`
	// MinNodes is the minimum number of nodes (for autoscaling)
	MinNodes int `yaml:"min_nodes,omitempty"`
	// MaxNodes is the maximum number of nodes (for autoscaling)
	MaxNodes int `yaml:"max_nodes,omitempty"`
	// Taints are Kubernetes taints applied to nodes in this pool
	Taints []Taint `yaml:"taints,omitempty"`
}

// Taint represents a Kubernetes taint for node scheduling.
type Taint struct {
	// Key is the taint key
	Key string `yaml:"key"`
	// Value is the taint value
	Value string `yaml:"value"`
	// Effect is the taint effect: NoSchedule, PreferNoSchedule, or NoExecute
	Effect string `yaml:"effect"`
}
