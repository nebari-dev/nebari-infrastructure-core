package aws

import "time"

type Config struct {
	Region                    string                           `yaml:"region"`
	StateBucket               string                           `yaml:"state_bucket,omitempty"`
	AvailabilityZones         []string                         `yaml:"availability_zones,omitempty"`
	VPCCIDRBlock              string                           `yaml:"vpc_cidr_block,omitempty"`
	ExistingVPCID             string                           `yaml:"existing_vpc_id,omitempty"`
	ExistingPrivateSubnetIDs  []string                         `yaml:"existing_private_subnet_ids,omitempty"`
	ExistingSecurityGroupID   string                           `yaml:"existing_security_group_id,omitempty"`
	KubernetesVersion         string                           `yaml:"kubernetes_version"`
	EndpointPrivateAccess     bool                             `yaml:"endpoint_private_access,omitempty"`
	EndpointPublicAccess      bool                             `yaml:"endpoint_public_access,omitempty"`
	EKSKMSArn                 string                           `yaml:"eks_kms_arn,omitempty"`
	EnabledLogTypes           []string                         `yaml:"enabled_log_types,omitempty"`
	ExistingClusterRoleArn    string                           `yaml:"existing_cluster_role_arn,omitempty"`
	ExistingNodeRoleArn       string                           `yaml:"existing_node_role_arn,omitempty"`
	PermissionsBoundary       string                           `yaml:"permissions_boundary,omitempty"`
	NodeGroups                map[string]NodeGroup             `yaml:"node_groups"`
	Tags                      map[string]string                `yaml:"tags,omitempty"`
	EFS                       *EFSConfig                       `yaml:"efs,omitempty"`
	Longhorn                  *LonghornConfig                  `yaml:"longhorn,omitempty"`
	AWSLoadBalancerController *AWSLoadBalancerControllerConfig `yaml:"aws_load_balancer_controller,omitempty"`
}

type AWSLoadBalancerControllerConfig struct {
	Enabled        *bool          `yaml:"enabled,omitempty"`
	ChartVersion   string         `yaml:"chart_version,omitempty"`
	DestroyTimeout *time.Duration `yaml:"destroy_timeout,omitempty"`
}

// defaultLBCChartVersion pins the aws-load-balancer-controller Helm chart.
// Bump to track the latest v3.x line; v2/chart-v1 is EOL.
const defaultLBCChartVersion = "3.2.1"

// defaultLBCDestroyTimeout is the maximum time the graceful Kubernetes-side
// cleanup will wait for LBC's finalizer to drain load balancers before falling
// through to the SDK sweep.
const defaultLBCDestroyTimeout = 5 * time.Minute

// LoadBalancerControllerEnabled returns whether the AWS Load Balancer Controller
// should be installed. Defaults to true.
func (c *Config) LoadBalancerControllerEnabled() bool {
	if c.AWSLoadBalancerController == nil || c.AWSLoadBalancerController.Enabled == nil {
		return true
	}
	return *c.AWSLoadBalancerController.Enabled
}

// LoadBalancerControllerChartVersion returns the Helm chart version for the
// AWS Load Balancer Controller. Returns defaultLBCChartVersion when unset.
func (c *Config) LoadBalancerControllerChartVersion() string {
	if c.AWSLoadBalancerController == nil || c.AWSLoadBalancerController.ChartVersion == "" {
		return defaultLBCChartVersion
	}
	return c.AWSLoadBalancerController.ChartVersion
}

// LoadBalancerControllerDestroyTimeout returns the maximum time the graceful
// Kubernetes-side cleanup will wait for LBC's finalizer to drain load
// balancers before falling through to the SDK sweep.
func (c *Config) LoadBalancerControllerDestroyTimeout() time.Duration {
	if c.AWSLoadBalancerController == nil || c.AWSLoadBalancerController.DestroyTimeout == nil {
		return defaultLBCDestroyTimeout
	}
	return *c.AWSLoadBalancerController.DestroyTimeout
}

type NodeGroup struct {
	Instance string            `yaml:"instance" json:"instance"`
	MinNodes int               `yaml:"min_nodes,omitempty" json:"min_nodes"`
	MaxNodes int               `yaml:"max_nodes,omitempty" json:"max_nodes"`
	GPU      bool              `yaml:"gpu,omitempty" json:"-"`
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

// LonghornEnabled returns whether Longhorn distributed block storage should
// be deployed on this AWS cluster. Defaults to true when the Longhorn config
// is nil or Enabled is not set.
func (c *Config) LonghornEnabled() bool {
	if c.Longhorn == nil {
		return true
	}
	if c.Longhorn.Enabled == nil {
		return true
	}
	return *c.Longhorn.Enabled
}

// LonghornReplicaCount returns the number of Longhorn volume replicas.
// Defaults to 2 when not set.
func (c *Config) LonghornReplicaCount() int {
	if c.Longhorn == nil || c.Longhorn.ReplicaCount == 0 {
		return 2
	}
	return c.Longhorn.ReplicaCount
}

type EFSConfig struct {
	Enabled               bool   `yaml:"enabled,omitempty"`
	PerformanceMode       string `yaml:"performance_mode,omitempty"` // default: generalPurpose
	ThroughputMode        string `yaml:"throughput_mode,omitempty"`  // default: bursting
	ProvisionedThroughput int    `yaml:"provisioned_throughput_mibps,omitempty"`
	Encrypted             bool   `yaml:"encrypted,omitempty"` // default: true
	KMSKeyArn             string `yaml:"kms_key_arn,omitempty"`
	StorageClassName      string `yaml:"storage_class_name,omitempty"` // default: efs-sc
}

const defaultEFSStorageClassName = "efs-sc"

// EFSStorageClassName returns the StorageClass name for EFS volumes.
// Returns the default "efs-sc" when EFS is not configured or when
// StorageClassName is empty.
func (c *Config) EFSStorageClassName() string {
	if c.EFS == nil || c.EFS.StorageClassName == "" {
		return defaultEFSStorageClassName
	}
	return c.EFS.StorageClassName
}

type LonghornConfig struct {
	Enabled        *bool             `yaml:"enabled,omitempty"`
	ReplicaCount   int               `yaml:"replica_count,omitempty"`
	DedicatedNodes bool              `yaml:"dedicated_nodes,omitempty"`
	NodeSelector   map[string]string `yaml:"node_selector,omitempty"`
}
