package aws

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
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
	CoreDNSCorefile               *string              `json:"coredns_corefile,omitempty"`
}

// gatewayProxyServiceFQDN returns the in-cluster DNS name of the Envoy proxy
// Service that envoy-gateway auto-creates for the nebari-gateway Gateway.
// The hash suffix is the first 8 hex chars of sha256("<ns>/<name>"), matching
// envoy-gateway's internal/utils/misc.go::GetHashedName algorithm.
func gatewayProxyServiceFQDN() string {
	sum := sha256.Sum256([]byte(gatewayNamespace + "/" + gatewayName))
	hash := hex.EncodeToString(sum[:])[:8]
	return fmt.Sprintf("envoy-%s-%s-%s.%s.svc.cluster.local", gatewayNamespace, gatewayName, hash, gatewayNamespace)
}

// buildCoreDNSCorefile returns a Corefile that rewrites in-cluster DNS lookups
// of the public domain (and its subdomains) to the in-cluster Envoy proxy
// Service. This avoids AWS NLB hairpin failures for in-cluster pods that
// resolve their own public hostnames - notably cert-manager's HTTP-01
// self-check. The base config matches the EKS-shipped Corefile so behavior
// for all other names is unchanged.
func buildCoreDNSCorefile(domain string) string {
	target := gatewayProxyServiceFQDN()
	return fmt.Sprintf(`.:53 {
    errors
    health {
        lameduck 5s
    }
    ready
    rewrite stop name exact %[1]s %[2]s
    rewrite stop name regex .*\.%[3]s %[2]s
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
        fallthrough in-addr.arpa ip6.arpa
    }
    prometheus :9153
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}
`, domain, target, regexEscapeDomain(domain))
}

// regexEscapeDomain escapes a DNS domain for use inside a CoreDNS rewrite
// regex. CoreDNS uses Go's regexp/syntax; the only metacharacter that appears
// in DNS labels is `.`, so escaping dots is sufficient.
func regexEscapeDomain(domain string) string {
	out := make([]byte, 0, len(domain)+8)
	for i := 0; i < len(domain); i++ {
		if domain[i] == '.' {
			out = append(out, '\\', '.')
			continue
		}
		out = append(out, domain[i])
	}
	return string(out)
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

// toTFVars converts the AWS provider config into terraform variables.
// domain is the public DNS domain from NebariConfig; when non-empty, a
// CoreDNS Corefile with a rewrite rule for that domain is generated and
// passed to the EKS managed addon so in-cluster pods bypass the NLB
// hairpin when resolving the cluster's public hostnames.
func (c *Config) toTFVars(projectName, domain string) TFVars {
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

	if domain != "" {
		corefile := buildCoreDNSCorefile(domain)
		vars.CoreDNSCorefile = &corefile
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
