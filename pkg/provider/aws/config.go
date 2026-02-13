package aws

// Config represents AWS-specific configuration for deploying Nebari on Amazon EKS.
type Config struct {
	// Region is the AWS region to deploy resources in (e.g., us-west-2, eu-west-1)
	Region string `yaml:"region"`
	// StateBucket is the S3 bucket name for storing Terraform state
	StateBucket string `yaml:"state_bucket,omitempty"`
	// AvailabilityZones specifies which AZs to deploy to (defaults to all available in region)
	AvailabilityZones []string `yaml:"availability_zones,omitempty"`
	// VPCCIDRBlock is the CIDR block for the VPC (e.g., 10.0.0.0/16)
	VPCCIDRBlock string `yaml:"vpc_cidr_block,omitempty"`
	// ExistingVPCID allows using an existing VPC instead of creating a new one
	ExistingVPCID string `yaml:"existing_vpc_id,omitempty"`
	// ExistingPrivateSubnetIDs specifies existing subnets to use with ExistingVPCID
	ExistingPrivateSubnetIDs []string `yaml:"existing_private_subnet_ids,omitempty"`
	// ExistingSecurityGroupID specifies an existing security group to use
	ExistingSecurityGroupID string `yaml:"existing_security_group_id,omitempty"`
	// KubernetesVersion is the EKS Kubernetes version (e.g., 1.28, 1.29)
	KubernetesVersion string `yaml:"kubernetes_version"`
	// EndpointPrivateAccess enables private API server endpoint access
	EndpointPrivateAccess bool `yaml:"endpoint_private_access,omitempty"`
	// EndpointPublicAccess enables public API server endpoint access (default: true)
	EndpointPublicAccess bool `yaml:"endpoint_public_access,omitempty"`
	// EKSKMSArn is the ARN of KMS key for EKS secrets encryption
	EKSKMSArn string `yaml:"eks_kms_arn,omitempty"`
	// EnabledLogTypes specifies which EKS control plane logs to enable
	EnabledLogTypes []string `yaml:"enabled_log_types,omitempty"`
	// ExistingClusterRoleArn uses an existing IAM role for the EKS cluster
	ExistingClusterRoleArn string `yaml:"existing_cluster_role_arn,omitempty"`
	// ExistingNodeRoleArn uses an existing IAM role for EKS node groups
	ExistingNodeRoleArn string `yaml:"existing_node_role_arn,omitempty"`
	// PermissionsBoundary is the ARN of IAM permissions boundary to apply to created roles
	PermissionsBoundary string `yaml:"permissions_boundary,omitempty"`
	// NodeGroups defines the EKS managed node groups
	NodeGroups map[string]NodeGroup `yaml:"node_groups"`
	// Tags are AWS resource tags applied to all created resources
	Tags map[string]string `yaml:"tags,omitempty"`
	// EFS configures Amazon Elastic File System for shared storage
	EFS *EFSConfig `yaml:"efs,omitempty"`
}

// NodeGroup represents an EKS managed node group configuration.
type NodeGroup struct {
	// Instance is the EC2 instance type (e.g., m5.xlarge, r5.2xlarge)
	Instance string `yaml:"instance" json:"instance"`
	// MinNodes is the minimum number of nodes (for autoscaling)
	MinNodes int `yaml:"min_nodes,omitempty" json:"min_nodes"`
	// MaxNodes is the maximum number of nodes (for autoscaling)
	MaxNodes int `yaml:"max_nodes,omitempty" json:"max_nodes"`
	// GPU indicates this node group uses GPU instances
	GPU bool `yaml:"gpu,omitempty" json:"gpu"`
	// AMIType specifies the AMI type (AL2_x86_64, AL2_x86_64_GPU, AL2_ARM_64)
	AMIType *string `yaml:"ami_type,omitempty" json:"ami_type,omitempty"`
	// Spot enables EC2 Spot instances for cost savings
	Spot bool `yaml:"spot,omitempty" json:"spot"`
	// DiskSize is the EBS volume size in GB for each node
	DiskSize *int `yaml:"disk_size,omitempty" json:"disk_size,omitempty"`
	// Labels are Kubernetes labels applied to nodes in this group
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	// Taints are Kubernetes taints applied to nodes in this group
	Taints []Taint `yaml:"taints,omitempty" json:"taints,omitempty"`
}

// Taint represents a Kubernetes taint for node scheduling.
type Taint struct {
	// Key is the taint key
	Key string `yaml:"key" json:"key"`
	// Value is the taint value
	Value string `yaml:"value" json:"value"`
	// Effect is the taint effect: NO_SCHEDULE, NO_EXECUTE, or PREFER_NO_SCHEDULE
	Effect string `yaml:"effect" json:"effect"`
}

// EFSConfig configures Amazon Elastic File System for shared persistent storage.
type EFSConfig struct {
	// Enabled activates EFS provisioning
	Enabled bool `yaml:"enabled,omitempty"`
	// PerformanceMode is generalPurpose (default) or maxIO
	PerformanceMode string `yaml:"performance_mode,omitempty"`
	// ThroughputMode is bursting (default), provisioned, or elastic
	ThroughputMode string `yaml:"throughput_mode,omitempty"`
	// ProvisionedThroughput is the throughput in MiB/s (only for provisioned mode)
	ProvisionedThroughput int `yaml:"provisioned_throughput_mibps,omitempty"`
	// Encrypted enables encryption at rest (default: true)
	Encrypted bool `yaml:"encrypted,omitempty"`
	// KMSKeyArn is the ARN of KMS key for EFS encryption
	KMSKeyArn string `yaml:"kms_key_arn,omitempty"`
}
