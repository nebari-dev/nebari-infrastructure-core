package azure

import "embed"

// tofuTemplates contains the OpenTofu shim that calls the Track A module.
// The embedded files are extracted to the working directory at deploy time
// alongside a generated terraform.tfvars.json.
//
//go:embed all:templates
var tofuTemplates embed.FS //nolint:unused // consumed by provider.go in a follow-up task

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
