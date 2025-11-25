package aws

// Config represents AWS-specific configuration
type Config struct {
	Region                  string                 `yaml:"region"`
	KubernetesVersion       string                 `yaml:"kubernetes_version"`
	AvailabilityZones       []string               `yaml:"availability_zones,omitempty"`
	NodeGroups              map[string]NodeGroup   `yaml:"node_groups,omitempty"`
	EKSEndpointAccess       string                 `yaml:"eks_endpoint_access,omitempty"`
	EKSPublicAccessCIDRs    []string               `yaml:"eks_public_access_cidrs,omitempty"`
	EKSKMSArn               string                 `yaml:"eks_kms_arn,omitempty"`
	ExistingSubnetIDs       []string               `yaml:"existing_subnet_ids,omitempty"`
	ExistingSecurityGroupID string                 `yaml:"existing_security_group_id,omitempty"`
	VPCCIDRBlock            string                 `yaml:"vpc_cidr_block,omitempty"`
	PermissionsBoundary     string                 `yaml:"permissions_boundary,omitempty"`
	Tags                    map[string]string      `yaml:"tags,omitempty"`
	EFS                     *EFSConfig             `yaml:"efs,omitempty"`
	AdditionalFields        map[string]interface{} `yaml:",inline"`
}

// EFSConfig represents AWS EFS configuration for shared storage
type EFSConfig struct {
	Enabled          bool   `yaml:"enabled,omitempty"`
	PerformanceMode  string `yaml:"performance_mode,omitempty"` // generalPurpose or maxIO
	ThroughputMode   string `yaml:"throughput_mode,omitempty"`  // bursting, provisioned, or elastic
	ProvisionedMBps  int    `yaml:"provisioned_mbps,omitempty"` // Required if throughput_mode is provisioned
	Encrypted        bool   `yaml:"encrypted,omitempty"`
	KMSKeyID         string `yaml:"kms_key_id,omitempty"`
	StorageClassName string `yaml:"storage_class_name,omitempty"` // Name for the K8s StorageClass (default: efs-sc)
}

// NodeGroup represents AWS-specific node group configuration
type NodeGroup struct {
	Instance            string  `yaml:"instance"`
	MinNodes            int     `yaml:"min_nodes,omitempty"`
	MaxNodes            int     `yaml:"max_nodes,omitempty"`
	Taints              []Taint `yaml:"taints,omitempty"`
	GPU                 bool    `yaml:"gpu,omitempty"`
	AMIType             string  `yaml:"ami_type,omitempty"`
	SingleSubnet        bool    `yaml:"single_subnet,omitempty"`
	PermissionsBoundary string  `yaml:"permissions_boundary,omitempty"`
	Spot                bool    `yaml:"spot,omitempty"`
}

// Taint represents a Kubernetes taint
type Taint struct {
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
	Effect string `yaml:"effect"` // NoSchedule, PreferNoSchedule, NoExecute
}
