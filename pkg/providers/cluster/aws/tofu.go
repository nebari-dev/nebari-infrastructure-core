package aws

import (
	"embed"
	"maps"
	"slices"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// Embed all files in the templates directory, including dotfiles (i.e. .terraform.lock.hcl)
//
//go:embed all:templates
var tofuTemplates embed.FS

// Security group rule keys for Longhorn webhook traffic between the EKS control plane
// and worker nodes.
const (
	longhornWebhookAdmissionKey  = "longhorn_webhook_admission"
	longhornWebhookConversionKey = "longhorn_webhook_conversion"
)

// gpuTaintKey is the taint key applied to GPU node groups. It matches the key
// the NVIDIA GPU Operator's operands tolerate out of the box, so operator
// components keep scheduling while ordinary pods are kept off GPU nodes.
const gpuTaintKey = "nvidia.com/gpu"

type TFVars struct {
	Region                        string               `json:"region"`
	ProjectName                   string               `json:"project_name,omitempty"`
	Tags                          map[string]string    `json:"tags,omitempty"`
	AvailabilityZones             []string             `json:"availability_zones,omitempty"`
	CreateVPC                     bool                 `json:"create_vpc"`
	VPCCIDRBlock                  *string              `json:"vpc_cidr_block,omitempty"`
	ExistingVPCID                 *string              `json:"existing_vpc_id,omitempty"`
	ExistingPrivateSubnetIDs      []string             `json:"existing_private_subnet_ids,omitempty"`
	CreateSecurityGroup           bool                 `json:"create_security_group"`
	ExistingSecurityGroupID       *string              `json:"existing_security_group_id,omitempty"`
	KubernetesVersion             string               `json:"kubernetes_version"`
	EndpointPrivateAccess         bool                 `json:"endpoint_private_access"`
	EndpointPublicAccess          bool                 `json:"endpoint_public_access"`
	EKSKMSArn                     *string              `json:"eks_kms_arn,omitempty"`
	ClusterEnabledLogTypes        []string             `json:"cluster_enabled_log_types,omitempty"`
	CreateIAMRoles                bool                 `json:"create_iam_roles"`
	ExistingClusterIAMRoleArn     *string              `json:"existing_cluster_iam_role_arn,omitempty"`
	ExistingNodeIAMRoleArn        *string              `json:"existing_node_iam_role_arn,omitempty"`
	IAMRolePermissionsBoundary    *string              `json:"iam_role_permissions_boundary,omitempty"`
	NodeGroups                    map[string]NodeGroup `json:"node_groups"`
	EFSEnabled                    bool                 `json:"efs_enabled"`
	EFSPerformanceMode            string               `json:"efs_performance_mode,omitempty"`
	EFSThroughputMode             string               `json:"efs_throughput_mode,omitempty"`
	EFSProvisionedThroughputMibps *int                 `json:"efs_provisioned_throughput_in_mibps,omitempty"`
	EFSEncrypted                  bool                 `json:"efs_encrypted"`
	EFSKMSKeyArn                  *string              `json:"efs_kms_key_arn,omitempty"`
	NodeSGAdditionalRules         map[string]any       `json:"node_security_group_additional_rules,omitempty"`
	ExtraCABundle                 *string              `json:"extra_ca_bundle,omitempty"`
	// No omitempty: a false value must be emitted so it overrides the module's
	// `true` default when the autoscaler is disabled.
	EnableClusterAutoscalerPodIdentity bool  `json:"enable_cluster_autoscaler_pod_identity"`
	EnableIRSA                         *bool `json:"enable_irsa,omitempty"`
	// CrossplaneCapabilities lists the bare capability keys (e.g. "s3", "rds")
	// the cluster opted into. crossplane-iam.tf expands capability dependencies
	// and provisions one scoped Pod Identity role per AWS provider controller.
	// Sorted for deterministic tfvars output.
	CrossplaneCapabilities []string `json:"crossplane_capabilities,omitempty"`
}

// resolveNodeGroupDefaults derives per-node-group defaults from the parsed
// config: the EKS AMI type (NVIDIA for GPU groups, standard otherwise) and the
// GPU taint. It returns a new map and never mutates the caller's node groups.
func resolveNodeGroupDefaults(nodeGroups map[string]NodeGroup) map[string]NodeGroup {
	result := make(map[string]NodeGroup, len(nodeGroups))
	for name, group := range nodeGroups {
		if group.AMIType == nil {
			var ami string
			switch {
			case group.GPU:
				ami = "AL2023_x86_64_NVIDIA"
			default:
				ami = "AL2023_x86_64_STANDARD"
			}
			group.AMIType = &ami
		}
		result[name] = applyGPUTaint(group)
	}
	return result
}

// applyGPUTaint ensures a GPU node group carries the nvidia.com/gpu taint so
// that only pods tolerating it schedule onto GPU hardware. The NVIDIA GPU
// Operator does not taint nodes itself; it only tolerates this taint on its own
// operands, so applying it is the caller's responsibility. A node group that
// already has an nvidia.com/gpu taint (any value or effect) is left untouched.
// The returned group never shares its Taints backing array with the input, so
// the caller's config is not mutated.
func applyGPUTaint(group NodeGroup) NodeGroup {
	if !group.GPU {
		return group
	}
	for _, t := range group.Taints {
		if t.Key == gpuTaintKey {
			return group
		}
	}
	taints := make([]Taint, len(group.Taints), len(group.Taints)+1)
	copy(taints, group.Taints)
	taints = append(taints, Taint{Key: gpuTaintKey, Value: "true", Effect: "NO_SCHEDULE"})
	group.Taints = taints
	return group
}

// applyLonghornDiskLabel ensures every node group that matches the Longhorn
// storage selector carries the labels Longhorn needs on a storage node (the
// selector labels plus the create-default-disk label) so Longhorn
// auto-provisions a disk there (#369). The label policy is owned by the
// longhorn package (StorageSelector/StorageNodeLabels); this only applies it to
// whichever AWS node group is the storage pool. Non-storage groups are left
// untouched.
func applyLonghornDiskLabel(nodeGroups map[string]NodeGroup, cfg *longhorn.Config) map[string]NodeGroup {
	sel := longhorn.StorageSelector(cfg)
	storageLabels := longhorn.StorageNodeLabels(cfg)
	result := make(map[string]NodeGroup, len(nodeGroups))
	for name, group := range nodeGroups {
		if nodeGroupMatchesSelector(group.Labels, sel) {
			labels := make(map[string]string, len(group.Labels)+len(storageLabels))
			maps.Copy(labels, group.Labels)
			maps.Copy(labels, storageLabels)
			group.Labels = labels
		}
		result[name] = group
	}
	return result
}

// nodeGroupMatchesSelector reports whether labels is a superset of sel (i.e. the
// node group carries every storage selector label). An empty selector never
// matches, so no group is treated as storage by accident.
func nodeGroupMatchesSelector(labels, sel map[string]string) bool {
	if len(sel) == 0 {
		return false
	}
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// toTFVars builds the OpenTofu variables for the cluster. caBundle is the
// top-level trust_bundle resolved by the orchestration layer (base64-encoded
// PEM), empty when no bundle is configured.
func (c *Config) toTFVars(projectName, caBundle string) TFVars {
	nodeGroups := resolveNodeGroupDefaults(c.NodeGroups)
	// When Longhorn runs on dedicated nodes, the storage node group(s) must carry
	// the create-default-disk label or Longhorn provisions no disks and every
	// volume faults (#369). Inject it here so it is applied by kubelet at node
	// registration (autoscaler-safe) rather than via a post-install reconcile.
	if c.LonghornEnabled() && c.Longhorn != nil && c.Longhorn.DedicatedNodes {
		nodeGroups = applyLonghornDiskLabel(nodeGroups, c.Longhorn)
	}

	vars := TFVars{
		Region:                 c.Region,
		ProjectName:            projectName,
		Tags:                   c.Tags,
		AvailabilityZones:      c.AvailabilityZones,
		CreateVPC:              c.ExistingVPCID == "" && len(c.ExistingPrivateSubnetIDs) == 0,
		CreateSecurityGroup:    c.ExistingSecurityGroupID == "",
		KubernetesVersion:      c.KubernetesVersion,
		EndpointPrivateAccess:  c.EndpointPrivateAccess,
		EndpointPublicAccess:   c.EndpointPublicAccess,
		ClusterEnabledLogTypes: c.EnabledLogTypes,
		CreateIAMRoles:         c.ExistingClusterRoleArn == "" && c.ExistingNodeRoleArn == "",
		NodeGroups:             nodeGroups,
		// Only provision the autoscaler's IAM role / pod identity association
		// when the autoscaler itself will be installed (see provider deploy).
		EnableClusterAutoscalerPodIdentity: c.ClusterAutoscalerEnabled(),
	}

	// Set pointer fields only when values are provided, so omitempty excludes them from JSON.
	// This lets Terraform use its defaults instead of receiving empty strings.
	if c.VPCCIDRBlock != "" {
		vars.VPCCIDRBlock = &c.VPCCIDRBlock
	}
	if c.ExistingVPCID != "" {
		vars.ExistingVPCID = &c.ExistingVPCID
	}
	if len(c.ExistingPrivateSubnetIDs) > 0 {
		vars.ExistingPrivateSubnetIDs = c.ExistingPrivateSubnetIDs
	}
	if c.ExistingSecurityGroupID != "" {
		vars.ExistingSecurityGroupID = &c.ExistingSecurityGroupID
	}
	if c.EKSKMSArn != "" {
		vars.EKSKMSArn = &c.EKSKMSArn
	}
	if c.ExistingClusterRoleArn != "" {
		vars.ExistingClusterIAMRoleArn = &c.ExistingClusterRoleArn
	}
	if c.ExistingNodeRoleArn != "" {
		vars.ExistingNodeIAMRoleArn = &c.ExistingNodeRoleArn
	}
	if c.PermissionsBoundary != "" {
		vars.IAMRolePermissionsBoundary = &c.PermissionsBoundary
	}
	if c.EnableIRSA != nil {
		vars.EnableIRSA = c.EnableIRSA
	}

	if len(c.CrossplaneCapabilities) > 0 {
		caps := slices.Clone(c.CrossplaneCapabilities)
		slices.Sort(caps)
		vars.CrossplaneCapabilities = caps
	}

	if c.LonghornEnabled() {
		vars.NodeSGAdditionalRules = map[string]any{
			longhornWebhookAdmissionKey: map[string]any{
				"description":                   "Cluster API to Longhorn admission webhook",
				"protocol":                      "tcp",
				"from_port":                     9502,
				"to_port":                       9502,
				"type":                          "ingress",
				"source_cluster_security_group": true,
			},
			longhornWebhookConversionKey: map[string]any{
				"description":                   "Cluster API to Longhorn conversion webhook",
				"protocol":                      "tcp",
				"from_port":                     9501,
				"to_port":                       9501,
				"type":                          "ingress",
				"source_cluster_security_group": true,
			},
		}
	}

	if caBundle != "" {
		vars.ExtraCABundle = &caBundle
	}

	if c.EFS != nil {
		vars.EFSEnabled = c.EFS.Enabled
		vars.EFSPerformanceMode = c.EFS.PerformanceMode
		vars.EFSThroughputMode = c.EFS.ThroughputMode
		vars.EFSEncrypted = c.EFS.Encrypted
		if c.EFS.ProvisionedThroughput > 0 {
			vars.EFSProvisionedThroughputMibps = &c.EFS.ProvisionedThroughput
		}
		if c.EFS.KMSKeyArn != "" {
			vars.EFSKMSKeyArn = &c.EFS.KMSKeyArn
		}
	}

	return vars
}
