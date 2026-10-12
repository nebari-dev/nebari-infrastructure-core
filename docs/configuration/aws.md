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
- [FSxOpenZFSConfig](#fsxopenzfsconfig)

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
| ExistingPrivateRouteTableIDs | `existing_private_route_table_ids` | `[]string` | No | ExistingPrivateRouteTableIDs lists the route tables of the existing private subnets. Only used when existing_vpc_id is set. A MULTI_AZ_1 FSx for OpenZFS filesystem adds routes to its floating endpo... |
| ExistingSecurityGroupID | `existing_security_group_id` | string | No |  |
| KubernetesVersion | `kubernetes_version` | string | No |  |
| EndpointPrivateAccess | `endpoint_private_access` | bool | No |  |
| EndpointPublicAccess | `endpoint_public_access` | bool | No |  |
| EKSKMSArn | `eks_kms_arn` | string | No |  |
| EBSKMSKeyARNs | `ebs_kms_key_arns` | `[]string` | No | KMS key ARNs (not aliases) the EBS CSI driver may use; key policies must delegate to IAM. NIC adds the default EBS key when the deployer has ec2:GetEbsDefaultKmsKeyId and kms:DescribeKey. |
| EnabledLogTypes | `enabled_log_types` | `[]string` | No |  |
| ExistingClusterRoleArn | `existing_cluster_role_arn` | string | No |  |
| ExistingNodeRoleArn | `existing_node_role_arn` | string | No |  |
| PermissionsBoundary | `permissions_boundary` | string | No |  |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | Yes |  |
| Tags | `tags` | `map[string]string` | No |  |
| EFS | `efs` | `*EFSConfig` | No |  |
| FSxOpenZFS | `fsx_openzfs` | `*FSxOpenZFSConfig` | No |  |
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
| AMIReleaseVersion | `ami_release_version` | `*string` | No | AMI release to pin, e.g. 1.34.11-20260923; must match kubernetes_version. "latest" follows each new release. Unset: a deploy that changes nothing keeps the group on its current release. |

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

---

## FSxOpenZFSConfig

FSxOpenZFSConfig configures an FSx for OpenZFS filesystem shared by the
cluster through the FSx for OpenZFS CSI driver. Fields left unset fall back
to the terraform-aws-eks-cluster module defaults, which also validates their
ranges.

Writing the block turns FSx on, as with the Longhorn block. Enabled exists
to turn it off while keeping the settings, so it is a pointer: unset means
on.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No |  |
| DeploymentType | `deployment_type` | string | No | MULTI_AZ_1 (default) or SINGLE_AZ_2 |
| StorageCapacityGiB | `storage_capacity_gib` | int | No | default: 64 |
| ThroughputMiBps | `throughput_mibps` | int | No | default: 160 |
| AutomaticBackupRetentionDays | `automatic_backup_retention_days` | `*int` | No | AutomaticBackupRetentionDays is a pointer because 0 is meaningful: it disables automatic backups. Unset keeps the module default of 7. |
| SkipFinalBackup | `skip_final_backup` | bool | No | SkipFinalBackup skips the backup FSx takes when the filesystem is deleted. The final backup outlives the cluster and is billed until it is deleted separately. |
| DeleteChildVolumesOnDestroy | `delete_child_volumes_on_destroy` | bool | No | DeleteChildVolumesOnDestroy deletes the volumes and snapshots the CSI driver created together with the filesystem. When false, destroy fails while any remain rather than deleting user data. |
| StorageClassName | `storage_class_name` | string | No | default: fsx-openzfs-sc |
| CSIChartVersion | `csi_chart_version` | string | No |  |

