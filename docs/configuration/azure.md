# Azure Provider Configuration

Configuration options specific to Microsoft Azure (AKS).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [NodeGroup](#nodegroup)
- [Taint](#taint)

---

## Config

Config represents Azure-specific configuration for deploying Nebari on Azure Kubernetes Service.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Region | `region` | string | Yes | Region is the Azure region (e.g., eastus, westeurope) |
| KubernetesVersion | `kubernetes_version` | string | No | KubernetesVersion is the AKS Kubernetes version (e.g., 1.28, 1.29) |
| StorageAccountPostfix | `storage_account_postfix` | string | Yes | StorageAccountPostfix is appended to create unique storage account names |
| AuthorizedIPRanges | `authorized_ip_ranges` | `[]string` | No | AuthorizedIPRanges restricts API server access to specific CIDRs |
| ResourceGroupName | `resource_group_name` | string | No | ResourceGroupName specifies an existing resource group (created if not specified) |
| NodeResourceGroupName | `node_resource_group_name` | string | No | NodeResourceGroupName is the resource group for AKS node resources |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | No | NodeGroups defines the AKS node pools |
| VnetSubnetID | `vnet_subnet_id` | string | No | VnetSubnetID specifies an existing subnet for AKS nodes |
| PrivateClusterEnabled | `private_cluster_enabled` | bool | No | PrivateClusterEnabled makes the API server only accessible from private networks |
| Tags | `tags` | `map[string]string` | No | Tags are Azure resource tags applied to all created resources |
| NetworkProfile | `network_profile` | `map[string]string` | No | NetworkProfile configures AKS networking (network_plugin, network_policy, etc.) |
| MaxPods | `max_pods` | int | No | MaxPods is the maximum number of pods per node (default: 110) |
| WorkloadIdentityEnabled | `workload_identity_enabled` | bool | No | WorkloadIdentityEnabled enables Azure Workload Identity for pod authentication |
| AzurePolicyEnabled | `azure_policy_enabled` | bool | No | AzurePolicyEnabled enables Azure Policy for AKS |

---

## NodeGroup

NodeGroup represents an AKS node pool configuration.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes | Instance is the Azure VM size (e.g., Standard_D4s_v3, Standard_NC6s_v3) |
| MinNodes | `min_nodes` | int | No | MinNodes is the minimum number of nodes (for autoscaling) |
| MaxNodes | `max_nodes` | int | No | MaxNodes is the maximum number of nodes (for autoscaling) |
| Taints | `taints` | `[]Taint` | No | Taints are Kubernetes taints applied to nodes in this pool |

---

## Taint

Taint represents a Kubernetes taint for node scheduling.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Key | `key` | string | Yes | Key is the taint key |
| Value | `value` | string | Yes | Value is the taint value |
| Effect | `effect` | string | Yes | Effect is the taint effect: NoSchedule, PreferNoSchedule, or NoExecute |
