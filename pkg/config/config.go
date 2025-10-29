package config

// NebariConfig represents the parsed nebari-config.yaml structure
type NebariConfig struct {
	ProjectName string `yaml:"project_name"`
	Provider    string `yaml:"provider"`
	Domain      string `yaml:"domain,omitempty"`

	// Provider-specific configurations
	AmazonWebServices   *AWSConfig   `yaml:"amazon_web_services,omitempty"`
	GoogleCloudPlatform *GCPConfig   `yaml:"google_cloud_platform,omitempty"`
	Azure               *AzureConfig `yaml:"azure,omitempty"`
	Local               *LocalConfig `yaml:"local,omitempty"`

	// Additional fields can be added as needed
	// Using map to capture additional fields for lenient parsing
	AdditionalFields map[string]interface{} `yaml:",inline"`
}

// AWSConfig represents AWS-specific configuration
type AWSConfig struct {
	Region                  string                  `yaml:"region"`
	KubernetesVersion       string                  `yaml:"kubernetes_version"`
	AvailabilityZones       []string                `yaml:"availability_zones,omitempty"`
	NodeGroups              map[string]AWSNodeGroup `yaml:"node_groups,omitempty"`
	EKSEndpointAccess       string                  `yaml:"eks_endpoint_access,omitempty"`
	EKSPublicAccessCIDRs    []string                `yaml:"eks_public_access_cidrs,omitempty"`
	EKSKMSArn               string                  `yaml:"eks_kms_arn,omitempty"`
	ExistingSubnetIDs       []string                `yaml:"existing_subnet_ids,omitempty"`
	ExistingSecurityGroupID string                  `yaml:"existing_security_group_id,omitempty"`
	VPCCIDRBlock            string                  `yaml:"vpc_cidr_block,omitempty"`
	PermissionsBoundary     string                  `yaml:"permissions_boundary,omitempty"`
	Tags                    map[string]string       `yaml:"tags,omitempty"`
	AdditionalFields        map[string]interface{}  `yaml:",inline"`
}

// GCPConfig represents GCP-specific configuration
type GCPConfig struct {
	Project                        string                  `yaml:"project"`
	Region                         string                  `yaml:"region"`
	KubernetesVersion              string                  `yaml:"kubernetes_version"`
	AvailabilityZones              []string                `yaml:"availability_zones,omitempty"`
	ReleaseChannel                 string                  `yaml:"release_channel,omitempty"`
	NodeGroups                     map[string]GCPNodeGroup `yaml:"node_groups,omitempty"`
	Tags                           []string                `yaml:"tags,omitempty"`
	NetworkingMode                 string                  `yaml:"networking_mode,omitempty"`
	Network                        string                  `yaml:"network,omitempty"`
	Subnetwork                     string                  `yaml:"subnetwork,omitempty"`
	IPAllocationPolicy             map[string]string       `yaml:"ip_allocation_policy,omitempty"`
	MasterAuthorizedNetworksConfig map[string]string       `yaml:"master_authorized_networks_config,omitempty"`
	PrivateClusterConfig           map[string]interface{}  `yaml:"private_cluster_config,omitempty"`
	AdditionalFields               map[string]interface{}  `yaml:",inline"`
}

// AzureConfig represents Azure-specific configuration
type AzureConfig struct {
	Region                  string                    `yaml:"region"`
	KubernetesVersion       string                    `yaml:"kubernetes_version,omitempty"`
	StorageAccountPostfix   string                    `yaml:"storage_account_postfix"`
	AuthorizedIPRanges      []string                  `yaml:"authorized_ip_ranges,omitempty"`
	ResourceGroupName       string                    `yaml:"resource_group_name,omitempty"`
	NodeResourceGroupName   string                    `yaml:"node_resource_group_name,omitempty"`
	NodeGroups              map[string]AzureNodeGroup `yaml:"node_groups,omitempty"`
	VnetSubnetID            string                    `yaml:"vnet_subnet_id,omitempty"`
	PrivateClusterEnabled   bool                      `yaml:"private_cluster_enabled,omitempty"`
	Tags                    map[string]string         `yaml:"tags,omitempty"`
	NetworkProfile          map[string]string         `yaml:"network_profile,omitempty"`
	MaxPods                 int                       `yaml:"max_pods,omitempty"`
	WorkloadIdentityEnabled bool                      `yaml:"workload_identity_enabled,omitempty"`
	AzurePolicyEnabled      bool                      `yaml:"azure_policy_enabled,omitempty"`
	AdditionalFields        map[string]interface{}    `yaml:",inline"`
}

// LocalConfig represents local K3s configuration
type LocalConfig struct {
	KubeContext      string                       `yaml:"kube_context,omitempty"`
	NodeSelectors    map[string]map[string]string `yaml:"node_selectors,omitempty"`
	AdditionalFields map[string]interface{}       `yaml:",inline"`
}

// NodeGroup represents a base Kubernetes node group configuration
// Note: Nebari uses "instance" and "min_nodes"/"max_nodes"
type NodeGroup struct {
	Instance string  `yaml:"instance"` // Required in Nebari
	MinNodes int     `yaml:"min_nodes,omitempty"`
	MaxNodes int     `yaml:"max_nodes,omitempty"`
	Taints   []Taint `yaml:"taints,omitempty"`
}

// AWSNodeGroup represents AWS-specific node group configuration
type AWSNodeGroup struct {
	Instance            string  `yaml:"instance"`
	MinNodes            int     `yaml:"min_nodes,omitempty"`
	MaxNodes            int     `yaml:"max_nodes,omitempty"`
	Taints              []Taint `yaml:"taints,omitempty"`
	GPU                 bool    `yaml:"gpu,omitempty"`
	SingleSubnet        bool    `yaml:"single_subnet,omitempty"`
	PermissionsBoundary string  `yaml:"permissions_boundary,omitempty"`
	Spot                bool    `yaml:"spot,omitempty"`
}

// GCPNodeGroup represents GCP-specific node group configuration
type GCPNodeGroup struct {
	Instance          string             `yaml:"instance"`
	MinNodes          int                `yaml:"min_nodes,omitempty"`
	MaxNodes          int                `yaml:"max_nodes,omitempty"`
	Taints            []Taint            `yaml:"taints,omitempty"`
	Preemptible       bool               `yaml:"preemptible,omitempty"`
	Labels            map[string]string  `yaml:"labels,omitempty"`
	GuestAccelerators []GuestAccelerator `yaml:"guest_accelerators,omitempty"`
}

// AzureNodeGroup represents Azure-specific node group configuration
type AzureNodeGroup struct {
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

// GuestAccelerator represents a GCP GPU configuration
type GuestAccelerator struct {
	Name  string `yaml:"name"`
	Count int    `yaml:"count,omitempty"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type             string                 `yaml:"type,omitempty"`
	Size             int                    `yaml:"size,omitempty"`
	AdditionalFields map[string]interface{} `yaml:",inline"`
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
