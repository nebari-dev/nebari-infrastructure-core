package azure

import "embed"

// tofuTemplates contains the OpenTofu shim that calls the Track A module.
// The embedded files are extracted to the working directory at deploy time
// alongside a generated terraform.tfvars.json.
//
//go:embed all:templates
var tofuTemplates embed.FS

// TFVars is the JSON marshalling layer between the parsed Config and OpenTofu.
// Field names use snake_case to match the Terraform module variables. Pointer
// types + `omitempty` let us pass null-as-omitted so the module's defaults win
// when a user doesn't set a value.
type TFVars struct {
	ProjectName               string                 `json:"project_name"`
	Location                  string                 `json:"location"`
	Tags                      map[string]string      `json:"tags,omitempty"`
	CreateResourceGroup       bool                   `json:"create_resource_group"`
	ExistingResourceGroupName *string                `json:"existing_resource_group_name,omitempty"`
	CreateVNet                bool                   `json:"create_vnet"`
	VNetCIDRBlock             string                 `json:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock       string                 `json:"node_subnet_cidr_block,omitempty"`
	ExistingVNetID            *string                `json:"existing_vnet_id,omitempty"`
	ExistingNodeSubnetID      *string                `json:"existing_node_subnet_id,omitempty"`
	NetworkPlugin             string                 `json:"network_plugin"`
	NetworkPluginMode         string                 `json:"network_plugin_mode"`
	PodCIDR                   string                 `json:"pod_cidr,omitempty"`
	ServiceCIDR               string                 `json:"service_cidr,omitempty"`
	DNSServiceIP              string                 `json:"dns_service_ip,omitempty"`
	KubernetesVersion         *string                `json:"kubernetes_version,omitempty"`
	PrivateClusterEnabled     bool                   `json:"private_cluster_enabled"`
	AuthorizedIPRanges        []string               `json:"authorized_ip_ranges,omitempty"`
	SKUTier                   string                 `json:"sku_tier"`
	IdentityType              string                 `json:"identity_type"`
	NodeGroups                map[string]TFNodeGroup `json:"node_groups"`
}

// TFNodeGroup is the JSON shape the Terraform module expects for each node
// group. Differs from the YAML-facing NodeGroup by field naming and the
// `mode` defaulting (handled in toTFVars).
type TFNodeGroup struct {
	VMSize       string            `json:"vm_size"`
	MinCount     int               `json:"min_count"`
	MaxCount     int               `json:"max_count"`
	Mode         string            `json:"mode"`
	OSDiskSizeGB int               `json:"os_disk_size_gb,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Taints       []string          `json:"taints,omitempty"`
	Zones        []string          `json:"zones,omitempty"`
}

// providerName is the NIC provider name returned by Provider.Name() and used
// as the value for the "provider" OTel span attribute. Distinct from the
// AKS "azure" network-plugin literal in TFVars (which is the upstream
// `azurerm_kubernetes_cluster.network_profile.network_plugin` value).
const providerName = "azure"

// modeSystem and modeUser are the AKS node-pool mode values. Each pool's
// mode determines whether it can host critical system workloads or only
// user workloads.
const (
	modeSystem = "System"
	modeUser   = "User"
)

// Azure tag names disallow these reserved chars: < > % & \ ? /
// (the Kubernetes-style "domain/key" convention doesn't survive on Azure), so
// we substitute "_" for "/" while keeping the nic.nebari.dev namespace.
const (
	tagClusterName = "nic.nebari.dev_cluster-name"
	tagManagedBy   = "nic.nebari.dev_managed-by"
)

// toTFVars converts a parsed Config into the JSON-friendly TFVars accepted by
// the embedded Terraform shim. Performs three transforms:
//  1. Default each node group's Mode to "User" if empty.
//  2. Resolve create_resource_group / create_vnet flags from BYO presence.
//  3. Inject NIC-required tags for tag-based discovery.
func (c *Config) toTFVars(projectName string) TFVars {
	vars := TFVars{
		ProjectName:           projectName,
		Location:              c.Region,
		Tags:                  mergeTags(c.Tags, projectName),
		CreateResourceGroup:   c.CreateResourceGroup == nil && c.ResourceGroupName == "" || (c.CreateResourceGroup != nil && *c.CreateResourceGroup),
		CreateVNet:            c.Network == nil || c.Network.ExistingVNetID == "",
		NetworkPlugin:         "azure",
		NetworkPluginMode:     "overlay",
		PrivateClusterEnabled: c.PrivateClusterEnabled,
		AuthorizedIPRanges:    c.AuthorizedIPRanges,
		SKUTier:               defaultIfEmpty(c.SKUTier, "Free"),
		IdentityType:          "UserAssigned",
		NodeGroups:            convertNodeGroups(c.NodeGroups),
	}

	if c.ResourceGroupName != "" {
		vars.CreateResourceGroup = false
		vars.ExistingResourceGroupName = &c.ResourceGroupName
	}

	if c.KubernetesVersion != "" {
		vars.KubernetesVersion = &c.KubernetesVersion
	}

	if c.Network != nil {
		vars.VNetCIDRBlock = c.Network.VNetCIDRBlock
		vars.NodeSubnetCIDRBlock = c.Network.NodeSubnetCIDRBlock
		vars.PodCIDR = c.Network.PodCIDR
		vars.ServiceCIDR = c.Network.ServiceCIDR
		vars.DNSServiceIP = c.Network.DNSServiceIP
		if c.Network.ExistingVNetID != "" {
			vars.ExistingVNetID = &c.Network.ExistingVNetID
		}
		if c.Network.ExistingNodeSubnetID != "" {
			vars.ExistingNodeSubnetID = &c.Network.ExistingNodeSubnetID
		}
	}

	return vars
}

func convertNodeGroups(in map[string]NodeGroup) map[string]TFNodeGroup {
	out := make(map[string]TFNodeGroup, len(in))
	for name, ng := range in {
		mode := ng.Mode
		if mode == "" {
			mode = modeUser
		}
		out[name] = TFNodeGroup{
			VMSize:       ng.Instance,
			MinCount:     ng.MinNodes,
			MaxCount:     ng.MaxNodes,
			Mode:         mode,
			OSDiskSizeGB: ng.OSDiskSizeGB,
			Labels:       ng.Labels,
			Taints:       ng.Taints,
			Zones:        ng.Zones,
		}
	}
	return out
}

func mergeTags(user map[string]string, projectName string) map[string]string {
	out := make(map[string]string, len(user)+2)
	for k, v := range user {
		out[k] = v
	}
	out[tagClusterName] = projectName
	out[tagManagedBy] = "nic"
	return out
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
