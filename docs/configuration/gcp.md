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

Config represents GCP-specific configuration for deploying Nebari on Google Kubernetes Engine.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Project | `project` | string | Yes | Project is the GCP project ID to deploy resources in |
| Region | `region` | string | Yes | Region is the GCP region (e.g., us-central1, europe-west1) |
| KubernetesVersion | `kubernetes_version` | string | Yes | KubernetesVersion is the GKE Kubernetes version (e.g., 1.28, 1.29) |
| AvailabilityZones | `availability_zones` | `[]string` | No | AvailabilityZones specifies which zones to deploy to within the region |
| ReleaseChannel | `release_channel` | string | No | ReleaseChannel is the GKE release channel (RAPID, REGULAR, STABLE) |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | No | NodeGroups defines the GKE node pools |
| Tags | `tags` | `[]string` | No | Tags are network tags applied to GKE nodes |
| NetworkingMode | `networking_mode` | string | No | NetworkingMode is VPC_NATIVE (recommended) or ROUTES |
| Network | `network` | string | No | Network is the VPC network name (uses default if not specified) |
| Subnetwork | `subnetwork` | string | No | Subnetwork is the VPC subnetwork name |
| IPAllocationPolicy | `ip_allocation_policy` | `map[string]string` | No | IPAllocationPolicy configures pod and service IP ranges for VPC-native clusters |
| MasterAuthorizedNetworksConfig | `master_authorized_networks_config` | `map[string]string` | No | MasterAuthorizedNetworksConfig restricts API server access to specific CIDRs |
| PrivateClusterConfig | `private_cluster_config` | `map[string]any` | No | PrivateClusterConfig enables private GKE cluster with private nodes |

---

## NodeGroup

NodeGroup represents a GKE node pool configuration.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Instance | `instance` | string | Yes | Instance is the GCE machine type (e.g., n1-standard-4, e2-standard-8) |
| MinNodes | `min_nodes` | int | No | MinNodes is the minimum number of nodes (for autoscaling) |
| MaxNodes | `max_nodes` | int | No | MaxNodes is the maximum number of nodes (for autoscaling) |
| Taints | `taints` | `[]Taint` | No | Taints are Kubernetes taints applied to nodes in this pool |
| Preemptible | `preemptible` | bool | No | Preemptible uses preemptible VMs for cost savings (may be terminated) |
| Labels | `labels` | `map[string]string` | No | Labels are Kubernetes labels applied to nodes in this pool |
| GuestAccelerators | `guest_accelerators` | `[]GuestAccelerator` | No | GuestAccelerators attaches GPUs to nodes in this pool |

---

## Taint

Taint represents a Kubernetes taint for node scheduling.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Key | `key` | string | Yes | Key is the taint key |
| Value | `value` | string | Yes | Value is the taint value |
| Effect | `effect` | string | Yes | Effect is the taint effect: NoSchedule, PreferNoSchedule, or NoExecute |

---

## GuestAccelerator

GuestAccelerator configures GPU attachment for GKE nodes.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Name | `name` | string | Yes | Name is the GPU type (e.g., nvidia-tesla-t4, nvidia-tesla-a100) |
| Count | `count` | int | No | Count is the number of GPUs to attach per node |
