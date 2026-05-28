package aws

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Region                   string               `yaml:"region"`
	StateBucket              string               `yaml:"state_bucket,omitempty"`
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
	Longhorn                 *LonghornConfig      `yaml:"longhorn,omitempty"`
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
