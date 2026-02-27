package aws

import "embed"

// Embed all files in the templates directory, including dotfiles (i.e. .terraform.lock.hcl)
//
//go:embed all:templates
var tofuTemplates embed.FS

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
}

func resolveNodeGroupAMIs(nodeGroups map[string]NodeGroup) map[string]NodeGroup {
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
		result[name] = group
	}
	return result
}

func (c *Config) toTFVars(projectName string) TFVars {
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
		NodeGroups:             resolveNodeGroupAMIs(c.NodeGroups),
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

	if c.LonghornEnabled() {
		vars.NodeSGAdditionalRules = map[string]any{
			"longhorn_webhook_admission": map[string]any{
				"description":                   "Cluster API to Longhorn admission webhook",
				"protocol":                      "tcp",
				"from_port":                     9502,
				"to_port":                       9502,
				"type":                          "ingress",
				"source_cluster_security_group": true,
			},
			"longhorn_webhook_conversion": map[string]any{
				"description":                   "Cluster API to Longhorn conversion webhook",
				"protocol":                      "tcp",
				"from_port":                     9501,
				"to_port":                       9501,
				"type":                          "ingress",
				"source_cluster_security_group": true,
			},
		}
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
