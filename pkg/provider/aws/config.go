package aws

type Config struct {
	Region                   string               `yaml:"region"`
	AvailabilityZones        []string             `yaml:"availability_zones,omitempty"`
	VPCCIDRBlock             string               `yaml:"vpc_cidr_block,omitempty"`
	ExistingVPCID            string               `yaml:"existing_vpc_id,omitempty"`
	ExistingPrivateSubnetIDs []string             `yaml:"existing_private_subnet_ids,omitempty"`
	ExistingSecurityGroupID  string               `yaml:"existing_security_group_id,omitempty"`
	KubernetesVersion        string               `yaml:"kubernetes_version"`
	EndpointPrivateAccess    bool                 `yaml:"endpoint_private_access,omitempty"`
	EndpointPublicAccess     bool                 `yaml:"endpoint_public_access,omitempty"`
	EKSKMSArn                string               `yaml:"eks_kms_arn,omitempty"`
	EnabledLogTypes          []string             `yaml:"enabled_log_types,omitempty"`
	ExistingClusterRoleArn   string               `yaml:"existing_cluster_role_arn,omitempty"`
	ExistingNodeRoleArn      string               `yaml:"existing_node_role_arn,omitempty"`
	PermissionsBoundary      string               `yaml:"permissions_boundary,omitempty"`
	NodeGroups               map[string]NodeGroup `yaml:"node_groups"`
	Tags                     map[string]string    `yaml:"tags,omitempty"`
	EFS                      *EFSConfig           `yaml:"efs,omitempty"`
}

type NodeGroup struct {
	Instance string            `yaml:"instance" json:"instance"`
	MinNodes int               `yaml:"min_nodes,omitempty" json:"min_nodes"`
	MaxNodes int               `yaml:"max_nodes,omitempty" json:"max_nodes"`
	GPU      bool              `yaml:"gpu,omitempty" json:"gpu"`
	AMIType  *string           `yaml:"ami_type,omitempty" json:"ami_type,omitempty"`
	Spot     bool              `yaml:"spot,omitempty" json:"spot"`
	DiskSize *int              `yaml:"disk_size,omitempty" json:"disk_size,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Taints   []Taint           `yaml:"taints,omitempty" json:"taints,omitempty"`
}

type Taint struct {
	Key    string `yaml:"key" json:"key"`
	Value  string `yaml:"value" json:"value"`
	Effect string `yaml:"effect" json:"effect"` // NO_SCHEDULE, NO_EXECUTE, PREFER_NO_SCHEDULE
}

type EFSConfig struct {
	Enabled               bool   `yaml:"enabled,omitempty"`
	PerformanceMode       string `yaml:"performance_mode,omitempty"` // default: generalPurpose
	ThroughputMode        string `yaml:"throughput_mode,omitempty"`  // default: bursting
	ProvisionedThroughput int    `yaml:"provisioned_throughput_mibps,omitempty"`
	Encrypted             bool   `yaml:"encrypted,omitempty"` // default: true
	KMSKeyArn             string `yaml:"kms_key_arn,omitempty"`
}
