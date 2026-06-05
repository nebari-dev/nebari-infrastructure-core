package aws

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

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
	Longhorn                  *longhorn.Config                 `yaml:"longhorn,omitempty"`
	AWSLoadBalancerController *AWSLoadBalancerControllerConfig `yaml:"aws_load_balancer_controller,omitempty"`
	ClusterAutoscaler         *ClusterAutoscalerConfig         `yaml:"cluster_autoscaler,omitempty"`
	LoadBalancerScheme        string                           `yaml:"load_balancer_scheme,omitempty"`
	// TrustBundle, when set, installs the given PEM bundle into the OS trust
	// store of every EKS worker node before kubelet starts. Required when nodes
	// must reach the EKS control plane, ECR, or pull container images through a
	// TLS-inspecting egress proxy. Will likely move to a top-level NebariConfig
	// field once trust-manager (the in-pod half of nebari-dev/nebari-infrastructure-core#307)
	// lands; keeping it provider-scoped here matches the current Provider interface.
	TrustBundle *TrustBundleConfig `yaml:"trust_bundle,omitempty"`
}

// TrustBundleConfig specifies the source of an extra CA bundle. Exactly one of
// Path or Inline must be set. Path is a filesystem path to a PEM file on the
// operator's machine; Inline is the PEM text itself.
type TrustBundleConfig struct {
	Path   string `yaml:"path,omitempty"`
	Inline string `yaml:"inline,omitempty"`
}

const (
	loadBalancerSchemeInternetFacing = "internet-facing"
	loadBalancerSchemeInternal       = "internal"
)

var validLoadBalancerSchemes = []string{
	loadBalancerSchemeInternetFacing,
	loadBalancerSchemeInternal,
}

// LoadBalancerSchemeOrDefault returns the configured AWS load balancer scheme,
// defaulting to "internet-facing" when unset. Values are validated at config
// load time, so callers can trust the result is one of the supported schemes.
func (c *Config) LoadBalancerSchemeOrDefault() string {
	if c.LoadBalancerScheme == "" {
		return loadBalancerSchemeInternetFacing
	}
	return c.LoadBalancerScheme
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

type ClusterAutoscalerConfig struct {
	Enabled      *bool  `yaml:"enabled,omitempty"`
	ChartVersion string `yaml:"chart_version,omitempty"`
	ImageTag     string `yaml:"image_tag,omitempty"`
}

// defaultClusterAutoscalerChartVersion pins the cluster-autoscaler Helm chart.
// Chart 9.57.0 ships appVersion 1.35.0. The autoscaler image version is not
// pinned by the chart here - it is derived from the cluster's Kubernetes
// version at install time (see ClusterAutoscalerImageTag), because AWS requires
// the autoscaler's version to match the cluster's Kubernetes minor version.
const defaultClusterAutoscalerChartVersion = "9.57.0"

// ClusterAutoscalerEnabled returns whether the Kubernetes Cluster Autoscaler
// should be installed. Defaults to true.
func (c *Config) ClusterAutoscalerEnabled() bool {
	if c.ClusterAutoscaler == nil || c.ClusterAutoscaler.Enabled == nil {
		return true
	}
	return *c.ClusterAutoscaler.Enabled
}

// ClusterAutoscalerChartVersion returns the Helm chart version for the Cluster
// Autoscaler. Returns defaultClusterAutoscalerChartVersion when unset.
func (c *Config) ClusterAutoscalerChartVersion() string {
	if c.ClusterAutoscaler == nil || c.ClusterAutoscaler.ChartVersion == "" {
		return defaultClusterAutoscalerChartVersion
	}
	return c.ClusterAutoscaler.ChartVersion
}

// ClusterAutoscalerImageTag returns the cluster-autoscaler container image tag.
// AWS requires the autoscaler version to match the cluster's Kubernetes minor
// version (cross-version is unsupported). When not explicitly set, the tag is
// derived from KubernetesVersion as `v<version>.0` (the autoscaler publishes a
// `.0` patch release for every supported minor). Returns "" when neither an
// explicit tag nor a Kubernetes version is available, letting the chart's
// bundled appVersion stand.
func (c *Config) ClusterAutoscalerImageTag() string {
	if c.ClusterAutoscaler != nil && c.ClusterAutoscaler.ImageTag != "" {
		return c.ClusterAutoscaler.ImageTag
	}
	if c.KubernetesVersion == "" {
		return ""
	}
	return fmt.Sprintf("v%s.0", c.KubernetesVersion)
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

// hasGPUNodeGroups reports whether any node group is tagged gpu: true. This is
// the AWS idiom for "this cluster has GPU hardware" — the infra layer already
// selects the AL2023_x86_64_NVIDIA AMI for these node groups, and the GPU
// operator install (see gpu_operator.go) keys off it.
func (c *Config) hasGPUNodeGroups() bool {
	for _, ng := range c.NodeGroups {
		if ng.GPU {
			return true
		}
	}
	return false
}

// LonghornEnabled returns whether Longhorn distributed block storage should
// be deployed on this AWS cluster. Defaults to true when the Longhorn block
// is omitted entirely — Longhorn is the AWS storage default. The shared
// longhorn.Config defaults to disabled-when-nil because non-AWS providers
// require an explicit opt-in.
func (c *Config) LonghornEnabled() bool {
	if c.Longhorn == nil {
		return true
	}
	return c.Longhorn.IsEnabled()
}

// LonghornReplicaCount returns the number of Longhorn volume replicas.
// Safe to call when c.Longhorn is nil — Replicas() is a nil-receiver method
// and returns the package default (this matches the LonghornEnabled() == true
// path when no longhorn block is configured on AWS).
func (c *Config) LonghornReplicaCount() int {
	return c.Longhorn.Replicas()
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

// ResolveBase64 returns the configured CA bundle as a base64-encoded PEM string,
// suitable for passing straight to the terraform-aws-eks-cluster module's
// extra_ca_bundle input. Returns an empty string when the bundle is unset.
func (t *TrustBundleConfig) ResolveBase64() (string, error) {
	if t == nil {
		return "", nil
	}
	pathSet := t.Path != ""
	inlineSet := strings.TrimSpace(t.Inline) != ""
	if pathSet && inlineSet {
		return "", errors.New("trust_bundle: only one of path or inline may be set")
	}
	if !pathSet && !inlineSet {
		return "", nil
	}
	var pem []byte
	if pathSet {
		data, err := os.ReadFile(t.Path)
		if err != nil {
			return "", fmt.Errorf("trust_bundle: read %s: %w", t.Path, err)
		}
		pem = data
	} else {
		pem = []byte(t.Inline)
	}
	if !strings.Contains(string(pem), "-----BEGIN CERTIFICATE-----") {
		return "", fmt.Errorf("trust_bundle: no PEM certificate found in %s",
			func() string {
				if pathSet {
					return t.Path
				}
				return "inline value"
			}())
	}
	return base64.StdEncoding.EncodeToString(pem), nil
}
