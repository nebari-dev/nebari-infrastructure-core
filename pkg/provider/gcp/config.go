package gcp

// Config represents GCP-specific configuration
type Config struct {
	Project                        string               `yaml:"project"`
	Region                         string               `yaml:"region"`
	KubernetesVersion              string               `yaml:"kubernetes_version"`
	AvailabilityZones              []string             `yaml:"availability_zones,omitempty"`
	ReleaseChannel                 string               `yaml:"release_channel,omitempty"`
	NodeGroups                     map[string]NodeGroup `yaml:"node_groups,omitempty"`
	Tags                           []string             `yaml:"tags,omitempty"`
	NetworkingMode                 string               `yaml:"networking_mode,omitempty"`
	Network                        string               `yaml:"network,omitempty"`
	Subnetwork                     string               `yaml:"subnetwork,omitempty"`
	IPAllocationPolicy             map[string]string    `yaml:"ip_allocation_policy,omitempty"`
	MasterAuthorizedNetworksConfig map[string]string    `yaml:"master_authorized_networks_config,omitempty"`
	PrivateClusterConfig           map[string]any       `yaml:"private_cluster_config,omitempty"`
	AdditionalFields               map[string]any       `yaml:",inline"`
}

// NodeGroup represents GCP-specific node group configuration
type NodeGroup struct {
	Instance          string             `yaml:"instance"`
	MinNodes          int                `yaml:"min_nodes,omitempty"`
	MaxNodes          int                `yaml:"max_nodes,omitempty"`
	Taints            []Taint            `yaml:"taints,omitempty"`
	Preemptible       bool               `yaml:"preemptible,omitempty"`
	Labels            map[string]string  `yaml:"labels,omitempty"`
	GuestAccelerators []GuestAccelerator `yaml:"guest_accelerators,omitempty"`
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
