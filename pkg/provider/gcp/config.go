package gcp

// Config represents GCP-specific configuration for deploying Nebari on Google Kubernetes Engine.
type Config struct {
	// Project is the GCP project ID to deploy resources in
	Project string `yaml:"project"`
	// Region is the GCP region (e.g., us-central1, europe-west1)
	Region string `yaml:"region"`
	// KubernetesVersion is the GKE Kubernetes version (e.g., 1.28, 1.29)
	KubernetesVersion string `yaml:"kubernetes_version"`
	// AvailabilityZones specifies which zones to deploy to within the region
	AvailabilityZones []string `yaml:"availability_zones,omitempty"`
	// ReleaseChannel is the GKE release channel (RAPID, REGULAR, STABLE)
	ReleaseChannel string `yaml:"release_channel,omitempty"`
	// NodeGroups defines the GKE node pools
	NodeGroups map[string]NodeGroup `yaml:"node_groups,omitempty"`
	// Tags are network tags applied to GKE nodes
	Tags []string `yaml:"tags,omitempty"`
	// NetworkingMode is VPC_NATIVE (recommended) or ROUTES
	NetworkingMode string `yaml:"networking_mode,omitempty"`
	// Network is the VPC network name (uses default if not specified)
	Network string `yaml:"network,omitempty"`
	// Subnetwork is the VPC subnetwork name
	Subnetwork string `yaml:"subnetwork,omitempty"`
	// IPAllocationPolicy configures pod and service IP ranges for VPC-native clusters
	IPAllocationPolicy map[string]string `yaml:"ip_allocation_policy,omitempty"`
	// MasterAuthorizedNetworksConfig restricts API server access to specific CIDRs
	MasterAuthorizedNetworksConfig map[string]string `yaml:"master_authorized_networks_config,omitempty"`
	// PrivateClusterConfig enables private GKE cluster with private nodes
	PrivateClusterConfig map[string]any `yaml:"private_cluster_config,omitempty"`
	// AdditionalFields captures any extra GCP-specific configuration
	AdditionalFields map[string]any `yaml:",inline"`
}

// NodeGroup represents a GKE node pool configuration.
type NodeGroup struct {
	// Instance is the GCE machine type (e.g., n1-standard-4, e2-standard-8)
	Instance string `yaml:"instance"`
	// MinNodes is the minimum number of nodes (for autoscaling)
	MinNodes int `yaml:"min_nodes,omitempty"`
	// MaxNodes is the maximum number of nodes (for autoscaling)
	MaxNodes int `yaml:"max_nodes,omitempty"`
	// Taints are Kubernetes taints applied to nodes in this pool
	Taints []Taint `yaml:"taints,omitempty"`
	// Preemptible uses preemptible VMs for cost savings (may be terminated)
	Preemptible bool `yaml:"preemptible,omitempty"`
	// Labels are Kubernetes labels applied to nodes in this pool
	Labels map[string]string `yaml:"labels,omitempty"`
	// GuestAccelerators attaches GPUs to nodes in this pool
	GuestAccelerators []GuestAccelerator `yaml:"guest_accelerators,omitempty"`
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

// GuestAccelerator configures GPU attachment for GKE nodes.
type GuestAccelerator struct {
	// Name is the GPU type (e.g., nvidia-tesla-t4, nvidia-tesla-a100)
	Name string `yaml:"name"`
	// Count is the number of GPUs to attach per node
	Count int `yaml:"count,omitempty"`
}
