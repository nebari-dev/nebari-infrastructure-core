package azure

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
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
	Files                 *FilesConfig         `yaml:"files,omitempty"`
}

// FilesConfig enables a ReadWriteMany StorageClass backed by Azure Files over
// NFS. The Azure Files CSI driver ships with AKS and creates a storage account
// and one file share per PersistentVolumeClaim, so NIC only creates the
// StorageClass and opens the node subnet to Azure Storage.
type FilesConfig struct {
	Enabled          bool   `yaml:"enabled,omitempty"`
	SKU              string `yaml:"sku,omitempty"`                // default: PremiumV2_LRS. PremiumV2_LRS, PremiumV2_ZRS, Premium_LRS or Premium_ZRS. PremiumV2 needs kubernetes_version 1.35 or later
	StorageClassName string `yaml:"storage_class_name,omitempty"` // default: azurefile-nfs
}

// NetworkConfig groups all VNet/subnet/CIDR knobs.
type NetworkConfig struct {
	VNetCIDRBlock       string `yaml:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock string `yaml:"node_subnet_cidr_block,omitempty"`
	PodCIDR             string `yaml:"pod_cidr,omitempty"`
	ServiceCIDR         string `yaml:"service_cidr,omitempty"`
	DNSServiceIP        string `yaml:"dns_service_ip,omitempty"`
	// DataPlane selects the AKS network dataplane: "azure" (default) or
	// "cilium" (Azure CNI Powered by Cilium).
	DataPlane            string `yaml:"dataplane,omitempty"`
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
		if ng.Mode == modeSystem {
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

	if c.FilesEnabled() {
		if err := c.Files.validate(c.KubernetesVersion); err != nil {
			return err
		}
	}

	return nil
}

const (
	defaultFilesSKU              = "PremiumV2_LRS"
	defaultFilesStorageClassName = "azurefile-nfs"

	// filesV2MinMinor is the first AKS minor version whose managed Azure Files
	// CSI driver (v1.35.0 and later) supports the provisioned v2 SKUs. AKS
	// ships driver v1.<minor>.x on Kubernetes 1.<minor>.
	filesV2MinMinor = 35
)

// filesSKUs lists the SKUs that support NFS file shares. NFS needs SSD
// (Premium) storage, so the Standard SKUs are rejected.
var filesSKUs = []string{"PremiumV2_LRS", "PremiumV2_ZRS", "Premium_LRS", "Premium_ZRS"}

// FilesEnabled reports whether the Azure Files NFS StorageClass is enabled.
func (c *Config) FilesEnabled() bool {
	return c.Files != nil && c.Files.Enabled
}

// FilesSKU returns the storage account SKU for NFS file shares.
func (c *Config) FilesSKU() string {
	if c.Files == nil || c.Files.SKU == "" {
		return defaultFilesSKU
	}
	return c.Files.SKU
}

// FilesStorageClassName returns the name of the Azure Files NFS StorageClass.
func (c *Config) FilesStorageClassName() string {
	if c.Files == nil || c.Files.StorageClassName == "" {
		return defaultFilesStorageClassName
	}
	return c.Files.StorageClassName
}

func (f *FilesConfig) validate(kubernetesVersion string) error {
	sku := f.SKU
	if sku == "" {
		sku = defaultFilesSKU
	}
	if !slices.Contains(filesSKUs, sku) {
		return fmt.Errorf("cluster.azure.files.sku %q is invalid (expected one of %s)", f.SKU, strings.Join(filesSKUs, ", "))
	}

	if strings.HasPrefix(sku, "PremiumV2_") && kubernetesVersion != "" {
		// kubernetesVersion already matched kubernetesVersionRE in Validate.
		minor, _ := strconv.Atoi(strings.Split(kubernetesVersion, ".")[1])
		if minor < filesV2MinMinor {
			return fmt.Errorf("cluster.azure.files.sku %q needs kubernetes_version 1.%d or later (got %q). Use Premium_LRS or Premium_ZRS on older versions", sku, filesV2MinMinor, kubernetesVersion)
		}
	}

	if f.StorageClassName != "" {
		if errs := validation.IsDNS1123Subdomain(f.StorageClassName); len(errs) > 0 {
			return fmt.Errorf("cluster.azure.files.storage_class_name %q is invalid: %s", f.StorageClassName, strings.Join(errs, ", "))
		}
	}

	return nil
}

func (n *NetworkConfig) validate() error {
	switch n.DataPlane {
	case "", dataPlaneAzure, dataPlaneCilium:
	default:
		return fmt.Errorf("cluster.azure.network.dataplane %q is invalid (expected %q or %q)", n.DataPlane, dataPlaneAzure, dataPlaneCilium)
	}

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
