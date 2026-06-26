# Hetzner Provider Configuration

Configuration options specific to Hetzner Cloud.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [NodeGroup](#nodegroup)
- [Autoscaling](#autoscaling)
- [NetworkConfig](#networkconfig)
- [SSHConfig](#sshconfig)

---

## Config

Config holds Hetzner-specific provider configuration.
Parsed from the "hetzner_cloud" key in nebari-config.yaml.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Location | `location` | string | Yes |  |
| KubernetesVersion | `kubernetes_version` | string | Yes |  |
| NodeGroups | `node_groups` | `map[string]NodeGroup` | Yes |  |
| ScheduleWorkloadsOnMasters | `schedule_workloads_on_masters` | `*bool` | No | ScheduleWorkloadsOnMasters controls whether application pods can be scheduled on control-plane nodes. Defaults to true, which enables single-node clusters and makes better use of small Hetzner inst... |
| PersistData | `persist_data` | bool | No | PersistData controls whether CSI volumes survive cluster destruction. When true, volumes are labeled persist=true during deploy, and destroy skips them. When false (the default), destroy deletes al... |
| SSH | `ssh` | `*SSHConfig` | No |  |
| Network | `network` | `*NetworkConfig` | No |  |
| Longhorn | `longhorn` | `*longhorn.Config` | No | Longhorn configures the Longhorn distributed block storage install. Hetzner's hcloud-volumes CSI is RWO-only; charts that need RWX (e.g. jupyterhub shared-storage for group dirs) require Longhorn â... |

---

## NodeGroup

NodeGroup defines a pool of Hetzner Cloud instances. Exactly one node group
must have Master set to true to serve as the k3s control plane.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| InstanceType | `instance_type` | string | Yes |  |
| Count | `count` | int | Yes |  |
| Master | `master` | bool | No | Master marks this node group as the k3s control plane. Exactly one node group must have this set to true. Master nodes run etcd and the Kubernetes API server. Whether they also run application work... |
| Location | `location` | string | No | Location overrides the top-level location for this node group. Only valid for worker (non-master) node groups. |
| Autoscaling | `autoscaling` | `*Autoscaling` | No |  |

---

## Autoscaling

Autoscaling configures automatic node pool scaling.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | bool | Yes |  |
| MinInstances | `min_instances` | int | Yes |  |
| MaxInstances | `max_instances` | int | Yes |  |

---

## NetworkConfig

NetworkConfig controls firewall rules for SSH and Kubernetes API access.
Defaults to 0.0.0.0/0 (open to all) if not specified - restrict these
in production to your IP ranges.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| SSHAllowedCIDRs | `ssh_allowed_cidrs` | `[]string` | No |  |
| APIAllowedCIDRs | `api_allowed_cidrs` | `[]string` | No |  |

---

## SSHConfig

SSHConfig allows users to provide their own SSH keys instead of auto-generated ones.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| PublicKeyPath | `public_key_path` | string | Yes |  |
| PrivateKeyPath | `private_key_path` | string | Yes |  |

