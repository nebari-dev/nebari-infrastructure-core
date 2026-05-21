package azure

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Config is the user-facing Azure cluster configuration as parsed from the
// `cluster.azure:` block of NIC YAML.
type Config struct {
	Region            string `yaml:"region"`
	ResourceGroupName string `yaml:"resource_group_name,omitempty"`
	// CreateResourceGroup is tri-state: nil = infer (true unless ResourceGroupName
	// is set), &true = always create, &false = never create (must supply ResourceGroupName).
	CreateResourceGroup   *bool                `yaml:"create_resource_group,omitempty"`
	KubernetesVersion     string               `yaml:"kubernetes_version,omitempty"`
	SKUTier               string               `yaml:"sku_tier,omitempty"`
	PrivateClusterEnabled bool                 `yaml:"private_cluster_enabled,omitempty"`
	AuthorizedIPRanges    []string             `yaml:"authorized_ip_ranges,omitempty"`
	Network               *NetworkConfig       `yaml:"network,omitempty"`
	NodeGroups            map[string]NodeGroup `yaml:"node_groups"`
	Tags                  map[string]string    `yaml:"tags,omitempty"`
}

// NetworkConfig groups all VNet/subnet/CIDR knobs.
type NetworkConfig struct {
	VNetCIDRBlock        string `yaml:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock  string `yaml:"node_subnet_cidr_block,omitempty"`
	PodCIDR              string `yaml:"pod_cidr,omitempty"`
	ServiceCIDR          string `yaml:"service_cidr,omitempty"`
	DNSServiceIP         string `yaml:"dns_service_ip,omitempty"`
	ExistingVNetID       string `yaml:"existing_vnet_id,omitempty"`
	ExistingNodeSubnetID string `yaml:"existing_node_subnet_id,omitempty"`
}

// NodeGroup describes one AKS node pool.
type NodeGroup struct {
	Instance     string            `yaml:"instance"`
	MinNodes     int               `yaml:"min_nodes"`
	MaxNodes     int               `yaml:"max_nodes"`
	Mode         string            `yaml:"mode,omitempty"` // "System" | "User"; defaults to "User"
	OSDiskSizeGB int               `yaml:"os_disk_size_gb,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty"`
	// Taints in "key=value:Effect" form, e.g. "dedicated=gpu:NoSchedule".
	Taints []string `yaml:"taints,omitempty"`
	Zones  []string `yaml:"zones,omitempty"`
}

var kubernetesVersionRE = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

// Validate checks that the Config is internally consistent and that all
// references between fields are coherent. It does NOT make any cloud calls.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Region) == "" {
		return fmt.Errorf("cluster.azure.region is required")
	}

	if len(c.NodeGroups) == 0 {
		return fmt.Errorf("cluster.azure.node_groups must contain at least one entry")
	}

	systemCount := 0
	for _, ng := range c.NodeGroups {
		if ng.Mode == "System" {
			systemCount++
		}
	}
	if systemCount > 1 {
		return fmt.Errorf("at most one node group may have mode=\"System\" (got %d)", systemCount)
	}

	if c.CreateResourceGroup != nil && !*c.CreateResourceGroup && strings.TrimSpace(c.ResourceGroupName) == "" {
		return fmt.Errorf("cluster.azure.resource_group_name is required when create_resource_group=false")
	}

	if c.KubernetesVersion != "" && !kubernetesVersionRE.MatchString(c.KubernetesVersion) {
		return fmt.Errorf("cluster.azure.kubernetes_version %q is not a valid semver-ish version (expected e.g. \"1.34\" or \"1.34.0\")", c.KubernetesVersion)
	}

	if c.Network != nil {
		if err := c.Network.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (n *NetworkConfig) validate() error {
	// BYO networking: both ID fields must be set together.
	if (n.ExistingVNetID != "") != (n.ExistingNodeSubnetID != "") {
		if n.ExistingVNetID == "" {
			return fmt.Errorf("cluster.azure.network.existing_vnet_id is required when existing_node_subnet_id is set")
		}
		return fmt.Errorf("cluster.azure.network.existing_node_subnet_id is required when existing_vnet_id is set")
	}

	for label, cidr := range map[string]string{
		"vnet_cidr_block":        n.VNetCIDRBlock,
		"node_subnet_cidr_block": n.NodeSubnetCIDRBlock,
		"pod_cidr":               n.PodCIDR,
		"service_cidr":           n.ServiceCIDR,
	} {
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("cluster.azure.network.%s: %w", label, err)
		}
	}

	if n.DNSServiceIP != "" && net.ParseIP(n.DNSServiceIP) == nil {
		return fmt.Errorf("cluster.azure.network.dns_service_ip: %q is not a valid IP address", n.DNSServiceIP)
	}

	return nil
}
