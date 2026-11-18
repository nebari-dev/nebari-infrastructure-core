# Azure Provider Configuration

Configuration options specific to Microsoft Azure (AKS).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [FilesConfig](#filesconfig)
- [NetworkConfig](#networkconfig)
- [NodeGroup](#nodegroup)

---

## Config

Config is the user-facing Azure cluster configuration as parsed from the
`cluster.azure:` block of NIC YAML.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Region | `region` | string | Yes |  |
| ResourceGroupName | `resource_group_name` | string | No |  |
| CreateResourceGroup | `create_resource_group` | `*bool` | No | CreateResourceGroup is tri-state: nil = infer (true unless ResourceGroupName is set), &true = always create, &false = never create (must supply ResourceGroupName). |
| KubernetesVersion | `kubernetes_version` | string | No |  |
| SKUTier | `sku_tier` | string | No |  |
| PrivateClusterEnabled | `private_cluster_enabled` | bool | No |  |
| AuthorizedIPRanges | `authorized_ip_ranges` | `[]string` | No |  |
| Network | `network` | `*NetworkConfig` | No |  |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | Yes |  |
| Tags | `tags` | `map[string]string` | No |  |
| Files | `files` | `*FilesConfig` | No |  |

---

## FilesConfig

FilesConfig enables a ReadWriteMany StorageClass backed by Azure Files over
NFS. The Azure Files CSI driver ships with AKS and creates a storage account
and one file share per PersistentVolumeClaim, so NIC only creates the
StorageClass and opens the node subnet to Azure Storage.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | bool | No |  |
| SKU | `sku` | string | No | default: PremiumV2_LRS. PremiumV2_LRS, PremiumV2_ZRS, Premium_LRS or Premium_ZRS. PremiumV2 needs kubernetes_version 1.35 or later |
| StorageClassName | `storage_class_name` | string | No | default: azurefile-nfs |

---

## NetworkConfig

NetworkConfig groups all VNet/subnet/CIDR knobs.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| VNetCIDRBlock | `vnet_cidr_block` | string | No |  |
| NodeSubnetCIDRBlock | `node_subnet_cidr_block` | string | No |  |
| PodCIDR | `pod_cidr` | string | No |  |
| ServiceCIDR | `service_cidr` | string | No |  |
| DNSServiceIP | `dns_service_ip` | string | No |  |
| DataPlane | `dataplane` | string | No | DataPlane selects the AKS network dataplane: "azure" (default) or "cilium" (Azure CNI Powered by Cilium). |
| ExistingVNetID | `existing_vnet_id` | string | No |  |
| ExistingNodeSubnetID | `existing_node_subnet_id` | string | No |  |

---

## NodeGroup

NodeGroup describes one AKS node pool.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes |  |
| MinNodes | `min_nodes` | int | Yes |  |
| MaxNodes | `max_nodes` | int | Yes |  |
| Mode | `mode` | string | No | "System" \| "User"; defaults to "User" |
| OSDiskSizeGB | `os_disk_size_gb` | int | No |  |
| Labels | `labels` | `map[string]string` | No |  |
| Taints | `taints` | `[]string` | No | Taints in "key=value:Effect" form, e.g. "dedicated=gpu:NoSchedule". |
| Zones | `zones` | `[]string` | No |  |

