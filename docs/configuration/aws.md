# AWS Provider Configuration

Configuration options specific to Amazon Web Services (EKS).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [NodeGroup](#nodegroup)
- [Taint](#taint)
- [EFSConfig](#efsconfig)

---

## Config

Config represents AWS-specific configuration for deploying Nebari on Amazon EKS.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Region | `region` | string | Yes | Region is the AWS region to deploy resources in (e.g., us-west-2, eu-west-1) |
| StateBucket | `state_bucket` | string | No | StateBucket is the S3 bucket name for storing Terraform state |
| AvailabilityZones | `availability_zones` | `[]string` | No | AvailabilityZones specifies which AZs to deploy to (defaults to all available in region) |
| VPCCIDRBlock | `vpc_cidr_block` | string | No | VPCCIDRBlock is the CIDR block for the VPC (e.g., 10.0.0.0/16) |
| ExistingVPCID | `existing_vpc_id` | string | No | ExistingVPCID allows using an existing VPC instead of creating a new one |
| ExistingPrivateSubnetIDs | `existing_private_subnet_ids` | `[]string` | No | ExistingPrivateSubnetIDs specifies existing subnets to use with ExistingVPCID |
| ExistingSecurityGroupID | `existing_security_group_id` | string | No | ExistingSecurityGroupID specifies an existing security group to use |
| KubernetesVersion | `kubernetes_version` | string | Yes | KubernetesVersion is the EKS Kubernetes version (e.g., 1.28, 1.29) |
| EndpointPrivateAccess | `endpoint_private_access` | bool | No | EndpointPrivateAccess enables private API server endpoint access |
| EndpointPublicAccess | `endpoint_public_access` | bool | No | EndpointPublicAccess enables public API server endpoint access (default: true) |
| EKSKMSArn | `eks_kms_arn` | string | No | EKSKMSArn is the ARN of KMS key for EKS secrets encryption |
| EnabledLogTypes | `enabled_log_types` | `[]string` | No | EnabledLogTypes specifies which EKS control plane logs to enable |
| ExistingClusterRoleArn | `existing_cluster_role_arn` | string | No | ExistingClusterRoleArn uses an existing IAM role for the EKS cluster |
| ExistingNodeRoleArn | `existing_node_role_arn` | string | No | ExistingNodeRoleArn uses an existing IAM role for EKS node groups |
| PermissionsBoundary | `permissions_boundary` | string | No | PermissionsBoundary is the ARN of IAM permissions boundary to apply to created roles |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | Yes | NodeGroups defines the EKS managed node groups |
| Tags | `tags` | `map[string]string` | No | Tags are AWS resource tags applied to all created resources |
| EFS | `efs` | `*EFSConfig` | No | EFS configures Amazon Elastic File System for shared storage |

---

## NodeGroup

NodeGroup represents an EKS managed node group configuration.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes | Instance is the EC2 instance type (e.g., m5.xlarge, r5.2xlarge) |
| MinNodes | `min_nodes` | int | No | MinNodes is the minimum number of nodes (for autoscaling) |
| MaxNodes | `max_nodes` | int | No | MaxNodes is the maximum number of nodes (for autoscaling) |
| GPU | `gpu` | bool | No | GPU indicates this node group uses GPU instances |
| AMIType | `ami_type` | `*string` | No | AMIType specifies the AMI type (AL2_x86_64, AL2_x86_64_GPU, AL2_ARM_64) |
| Spot | `spot` | bool | No | Spot enables EC2 Spot instances for cost savings |
| DiskSize | `disk_size` | `*int` | No | DiskSize is the EBS volume size in GB for each node |
| Labels | `labels` | `map[string]string` | No | Labels are Kubernetes labels applied to nodes in this group |
| Taints | `taints` | `[]Taint` | No | Taints are Kubernetes taints applied to nodes in this group |

---

## Taint

Taint represents a Kubernetes taint for node scheduling.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Key | `key` | string | Yes | Key is the taint key |
| Value | `value` | string | Yes | Value is the taint value |
| Effect | `effect` | string | Yes | Effect is the taint effect: NO_SCHEDULE, NO_EXECUTE, or PREFER_NO_SCHEDULE |

---

## EFSConfig

EFSConfig configures Amazon Elastic File System for shared persistent storage.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | bool | No | Enabled activates EFS provisioning |
| PerformanceMode | `performance_mode` | string | No | PerformanceMode is generalPurpose (default) or maxIO |
| ThroughputMode | `throughput_mode` | string | No | ThroughputMode is bursting (default), provisioned, or elastic |
| ProvisionedThroughput | `provisioned_throughput_mibps` | int | No | ProvisionedThroughput is the throughput in MiB/s (only for provisioned mode) |
| Encrypted | `encrypted` | bool | No | Encrypted enables encryption at rest (default: true) |
| KMSKeyArn | `kms_key_arn` | string | No | KMSKeyArn is the ARN of KMS key for EFS encryption |
