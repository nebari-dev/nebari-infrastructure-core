# Longhorn Storage Configuration

Distributed block storage settings shared by the cloud providers, including dedicated-node scheduling.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config carries the user-tunable Longhorn settings shared across providers.

A nil *Config means "do not install" (see IsEnabled). When the user supplies
a non-nil Config, Enabled defaults to true so an empty block (`longhorn: {}`)
is the minimal opt-in. ReplicaCount defaults to 2 — appropriate for small
clusters; production deploys should raise it.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No |  |
| ReplicaCount | `replica_count` | int | No |  |
| DedicatedNodes | `dedicated_nodes` | bool | No | DedicatedNodes confines Longhorn replica data to a dedicated, tainted storage node group (disks are created only there; see NodeSelector and CreateDefaultDiskLabel).  WARNING — toggling this is a M... |
| NodeSelector | `node_selector` | `map[string]string` | No | NodeSelector is the label set that identifies the dedicated storage nodes when DedicatedNodes is true (defaults to {node.longhorn.io/storage: "true"}, i.e. NodeStorageLabel). It no longer pins Long... |
| InstanceManagerCPUPercent | `instance_manager_cpu_percent` | `*int` | No | InstanceManagerCPUPercent overrides Longhorn's "Guaranteed Instance Manager CPU" setting: the percentage (0-40) of each node's allocatable CPU reserved for every instance-manager pod, per node. Lon... |

