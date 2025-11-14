package aws

// AWSInfrastructureState extends the generic InfrastructureState with AWS-specific details
// This is an in-memory representation populated by querying AWS APIs
// It is NEVER persisted to disk (stateless architecture)
type AWSInfrastructureState struct {
	ClusterName string
	Region      string

	// VPC and networking
	VPC *AWSVPCState

	// EKS cluster
	Cluster *AWSClusterState

	// EKS node groups
	NodeGroups []AWSNodeGroupState

	// EFS storage
	Storage *AWSStorageState

	// IAM roles
	IAMRoles *AWSIAMRoles
}

// AWSVPCState represents AWS VPC state discovered from EC2 APIs
type AWSVPCState struct {
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
	PublicRouteTableID  string
	PrivateRouteTableIDs []string

	// Security group IDs
	SecurityGroupIDs []string

	// VPC tags
	Tags map[string]string
}

// AWSClusterState represents EKS cluster state discovered from EKS APIs
type AWSClusterState struct {
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

// AWSNodeGroupState represents EKS node group state discovered from EKS APIs
type AWSNodeGroupState struct {
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
	Taints []AWSTaint

	// Launch template info
	LaunchTemplateID      string
	LaunchTemplateVersion string

	// Capacity type (ON_DEMAND or SPOT)
	CapacityType string

	// Node group tags
	Tags map[string]string

	// Health status
	Health AWSNodeGroupHealth

	// Created timestamp
	CreatedAt string

	// Modified timestamp
	ModifiedAt string
}

// AWSTaint represents a Kubernetes taint on AWS node groups
type AWSTaint struct {
	Key    string
	Value  string
	Effect string // NO_SCHEDULE, NO_EXECUTE, PREFER_NO_SCHEDULE
}

// AWSNodeGroupHealth represents the health status of a node group
type AWSNodeGroupHealth struct {
	// Issues affecting node group
	Issues []string
}

// AWSStorageState represents EFS state discovered from EFS APIs
type AWSStorageState struct {
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
	MountTargets []AWSMountTarget

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

// AWSMountTarget represents an EFS mount target
type AWSMountTarget struct {
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

// AWSIAMRoles represents IAM roles created for the cluster
type AWSIAMRoles struct {
	// EKS cluster service role
	ClusterRoleARN string

	// EKS node instance role
	NodeRoleARN string

	// OIDC provider ARN for IRSA
	OIDCProviderARN string

	// Additional service account roles (for IRSA)
	ServiceAccountRoles map[string]string
}
