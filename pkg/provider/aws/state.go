package aws

// AWSInfrastructureState extends the generic InfrastructureState with AWS-specific details
// This is an in-memory representation populated by querying AWS APIs
// It is NEVER persisted to disk (stateless architecture)
type InfrastructureState struct {
	ClusterName string
	Region      string

	// VPC and networking
	VPC *VPCState

	// EKS cluster
	Cluster *ClusterState

	// EKS node groups
	NodeGroups []NodeGroupState

	// EFS storage
	Storage *StorageState

	// IAM roles
	IAMRoles *IAMRoles
}

// VPCState represents AWS VPC state discovered from EC2 APIs
type VPCState struct {
	// VPC ID
	VPCID string

	// VPC CIDR block
	CIDR string

	// Public subnet IDs (across availability zones)
	PublicSubnetIDs []string

	// Private subnet IDs (across availability zones)
	PrivateSubnetIDs []string

	// Availability zones
	AvailabilityZones []string

	// Internet Gateway ID
	InternetGatewayID string

	// NAT Gateway IDs (one per AZ)
	NATGatewayIDs []string

	// Route table IDs
	PublicRouteTableID   string
	PrivateRouteTableIDs []string

	// Security group IDs
	SecurityGroupIDs []string

	// VPC tags
	Tags map[string]string
}

// ClusterState represents EKS cluster state discovered from EKS APIs
type ClusterState struct {
	// Cluster name
	Name string

	// Cluster ARN
	ARN string

	// Kubernetes API endpoint
	Endpoint string

	// Kubernetes version
	Version string

	// Cluster status (CREATING, ACTIVE, UPDATING, DELETING, FAILED)
	Status string

	// Certificate authority data (base64 encoded)
	CertificateAuthority string

	// VPC configuration
	VPCID             string
	SubnetIDs         []string
	SecurityGroupIDs  []string
	EndpointPublic    bool
	EndpointPrivate   bool
	PublicAccessCIDRs []string

	// OIDC provider ARN for IRSA (IAM Roles for Service Accounts)
	OIDCProviderARN string

	// Encryption config
	EncryptionKMSKeyARN string

	// Control plane logging
	EnabledLogTypes []string

	// Cluster tags
	Tags map[string]string

	// Platform version
	PlatformVersion string

	// Created timestamp
	CreatedAt string
}

// NodeGroupState represents EKS node group state discovered from EKS APIs
type NodeGroupState struct {
	// Node group name
	Name string

	// Node group ARN
	ARN string

	// Cluster name this node group belongs to
	ClusterName string

	// Instance types
	InstanceTypes []string

	// Autoscaling configuration
	MinSize     int
	MaxSize     int
	DesiredSize int

	// Current size
	CurrentSize int

	// Subnet IDs where nodes are placed
	SubnetIDs []string

	// Node IAM role ARN
	NodeRoleARN string

	// AMI type (AL2_x86_64, AL2_x86_64_GPU, AL2_ARM_64, etc.)
	AMIType string

	// Disk size in GB
	DiskSize int

	// Status (CREATING, ACTIVE, UPDATING, DELETING, etc.)
	Status string

	// Kubernetes labels
	Labels map[string]string

	// Kubernetes taints
	Taints []Taint

	// Launch template info
	LaunchTemplateID      string
	LaunchTemplateVersion string

	// Capacity type (ON_DEMAND or SPOT)
	CapacityType string

	// Node group tags
	Tags map[string]string

	// Health status
	Health NodeGroupHealth

	// Created timestamp
	CreatedAt string

	// Modified timestamp
	ModifiedAt string
}

// Taint represents a Kubernetes taint on AWS node groups
type Taint struct {
	Key    string
	Value  string
	Effect string // NO_SCHEDULE, NO_EXECUTE, PREFER_NO_SCHEDULE
}

// NodeGroupHealth represents the health status of a node group
type NodeGroupHealth struct {
	// Issues affecting node group
	Issues []string
}

// StorageState represents EFS state discovered from EFS APIs
type StorageState struct {
	// File system ID
	FileSystemID string

	// File system ARN
	ARN string

	// Lifecycle state (creating, available, updating, deleting, deleted)
	LifeCycleState string

	// Performance mode (generalPurpose, maxIO)
	PerformanceMode string

	// Throughput mode (bursting, provisioned, elastic)
	ThroughputMode string

	// Provisioned throughput in MiB/s (if throughput mode is provisioned)
	ProvisionedThroughputMiBps float64

	// Mount target IDs and subnets
	MountTargets []MountTarget

	// Security group IDs for mount targets
	SecurityGroupIDs []string

	// Size in bytes
	SizeInBytes int64

	// Encrypted
	Encrypted bool

	// KMS key ID (if encrypted)
	KMSKeyID string

	// Tags
	Tags map[string]string

	// Created timestamp
	CreatedAt string
}

// MountTarget represents an EFS mount target
type MountTarget struct {
	// Mount target ID
	MountTargetID string

	// Subnet ID where mount target is placed
	SubnetID string

	// IP address
	IPAddress string

	// Availability zone
	AvailabilityZone string

	// Life cycle state
	LifeCycleState string
}

// IAMRoles represents IAM roles created for the cluster
type IAMRoles struct {
	// EKS cluster service role
	ClusterRoleARN string

	// EKS node instance role
	NodeRoleARN string

	// OIDC provider ARN for IRSA
	OIDCProviderARN string

	// Additional service account roles (for IRSA)
	ServiceAccountRoles map[string]string
}
