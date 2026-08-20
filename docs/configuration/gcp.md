# GCP Provider Configuration

Configuration options specific to Google Cloud Platform (GKE).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [NodeGroup](#nodegroup)
- [Taint](#taint)
- [GuestAccelerator](#guestaccelerator)

---

## Config

Config represents GCP-specific configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Project | `project` | string | Yes |  |
| Region | `region` | string | Yes |  |
| KubernetesVersion | `kubernetes_version` | string | Yes |  |
| AvailabilityZones | `availability_zones` | `[]string` | No |  |
| ReleaseChannel | `release_channel` | string | No |  |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | No |  |
| Tags | `tags` | `[]string` | No |  |
| NetworkingMode | `networking_mode` | string | No |  |
| Network | `network` | string | No |  |
| Subnetwork | `subnetwork` | string | No |  |
| IPAllocationPolicy | `ip_allocation_policy` | `map[string]string` | No |  |
| MasterAuthorizedNetworksConfig | `master_authorized_networks_config` | `map[string]string` | No |  |
| PrivateClusterConfig | `private_cluster_config` | `map[string]any` | No |  |

---

## NodeGroup

NodeGroup represents GCP-specific node group configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes |  |
| MinNodes | `min_nodes` | int | No |  |
| MaxNodes | `max_nodes` | int | No |  |
| Taints | `taints` | `[]Taint` | No |  |
| Preemptible | `preemptible` | bool | No |  |
| Labels | `labels` | `map[string]string` | No |  |
| GuestAccelerators | `guest_accelerators` | `[]GuestAccelerator` | No |  |

---

## Taint

Taint represents a Kubernetes taint

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Key | `key` | string | Yes |  |
| Value | `value` | string | Yes |  |
| Effect | `effect` | string | Yes | NoSchedule, PreferNoSchedule, NoExecute |

---

## GuestAccelerator

GuestAccelerator represents a GCP GPU configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Name | `name` | string | Yes |  |
| Count | `count` | int | No |  |

