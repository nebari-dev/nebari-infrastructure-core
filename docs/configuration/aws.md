# AWS Provider Configuration

Configuration options specific to Amazon Web Services (EKS).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [AWSLoadBalancerControllerConfig](#awsloadbalancercontrollerconfig)
- [ClusterAutoscalerConfig](#clusterautoscalerconfig)
- [NodeGroup](#nodegroup)
- [Taint](#taint)
- [EFSConfig](#efsconfig)

---

## Config

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Region | `region` | string | Yes |  |
| StateBucket | `state_bucket` | string | No |  |
| AvailabilityZones | `availability_zones` | `[]string` | No |  |
| VPCCIDRBlock | `vpc_cidr_block` | string | No |  |
| ExistingVPCID | `existing_vpc_id` | string | No |  |
| ExistingPrivateSubnetIDs | `existing_private_subnet_ids` | `[]string` | No |  |
| ExistingSecurityGroupID | `existing_security_group_id` | string | No |  |
| KubernetesVersion | `kubernetes_version` | string | No |  |
| EndpointPrivateAccess | `endpoint_private_access` | bool | No |  |
| EndpointPublicAccess | `endpoint_public_access` | bool | No |  |
| EKSKMSArn | `eks_kms_arn` | string | No |  |
| EnabledLogTypes | `enabled_log_types` | `[]string` | No |  |
| ExistingClusterRoleArn | `existing_cluster_role_arn` | string | No |  |
| ExistingNodeRoleArn | `existing_node_role_arn` | string | No |  |
| PermissionsBoundary | `permissions_boundary` | string | No |  |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | Yes |  |
| Tags | `tags` | `map[string]string` | No |  |
| EFS | `efs` | `*EFSConfig` | No |  |
| Longhorn | `longhorn` | `*longhorn.Config` | No |  |
| AWSLoadBalancerController | `aws_load_balancer_controller` | `*AWSLoadBalancerControllerConfig` | No |  |
| ClusterAutoscaler | `cluster_autoscaler` | `*ClusterAutoscalerConfig` | No |  |
| LoadBalancerScheme | `load_balancer_scheme` | string | No |  |
| EnableIRSA | `enable_irsa` | `*bool` | No | EnableIRSA toggles creation of the EKS OIDC provider for IAM Roles for Service Accounts. When unset, the upstream module default (true) applies. Set false when the cluster relies exclusively on EKS... |

---

## AWSLoadBalancerControllerConfig

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No |  |
| ChartVersion | `chart_version` | string | No |  |
| DestroyTimeout | `destroy_timeout` | `*time.Duration` | No |  |

---

## ClusterAutoscalerConfig

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No |  |
| ChartVersion | `chart_version` | string | No |  |
| ImageTag | `image_tag` | string | No |  |

---

## NodeGroup

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes |  |
| MinNodes | `min_nodes` | int | No |  |
| MaxNodes | `max_nodes` | int | No |  |
| GPU | `gpu` | bool | No |  |
| AMIType | `ami_type` | `*string` | No |  |
| Spot | `spot` | bool | No |  |
| DiskSize | `disk_size` | `*int` | No |  |
| Labels | `labels` | `map[string]string` | No |  |
| Taints | `taints` | `[]Taint` | No |  |

---

## Taint

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Key | `key` | string | Yes |  |
| Value | `value` | string | Yes |  |
| Effect | `effect` | string | Yes | NO_SCHEDULE, NO_EXECUTE, PREFER_NO_SCHEDULE |

---

## EFSConfig

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | bool | No |  |
| PerformanceMode | `performance_mode` | string | No | default: generalPurpose |
| ThroughputMode | `throughput_mode` | string | No | default: bursting |
| ProvisionedThroughput | `provisioned_throughput_mibps` | int | No |  |
| Encrypted | `encrypted` | bool | No | default: true |
| KMSKeyArn | `kms_key_arn` | string | No |  |
| StorageClassName | `storage_class_name` | string | No | default: efs-sc |

