# Local Provider Configuration

Configuration options for local Kubernetes deployments.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [KindConfig](#kindconfig)
- [KindNodeGroup](#kindnodegroup)
- [KindMount](#kindmount)

---

## Config

Config represents local provider configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Kind | `kind` | `*KindConfig` | No |  |
| NodeSelectors | `node_selectors` | `map[string]map[string]string` | No |  |
| HTTPSPort | `https_port` | int | No | HTTPSPort is the host port the gateway's HTTPS listener is published on (default 443). Override it when 443 is taken on the host or when running several local clusters side by side. Takes effect on... |
| HTTPPort | `http_port` | int | No | HTTPPort is the host port the gateway's HTTP listener (the HTTPS redirect) is published on (default 80). Override it under the same circumstances as HTTPSPort, including rootless container runtimes... |

---

## KindConfig

KindConfig holds optional config for the deployed kind cluster. It may be
omitted entirely (nil), in which case the cluster is created with defaults.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| NodeImage | `node_image` | string | No | NodeImage is the kindest/node image to use (e.g. "kindest/node:v1.32.2"). Empty means the default image of the bundled kind version. |
| ExtraMounts | `extra_mounts` | `[]KindMount` | No | ExtraMounts are additional host directories mounted into every cluster node container. NIC mounts its auto-created local GitOps repository automatically; an explicit file:// repository needs a matc... |
| NodeGroups | `node_groups` | `map[string]KindNodeGroup` | No | NodeGroups are the cluster's worker nodes, keyed by group name. NIC always creates exactly one control-plane node, which is not configurable here: node_groups defines workers only. With no node gro... |

---

## KindNodeGroup

KindNodeGroup is a set of identical kind worker nodes. Every node in the
group is labeled nebari.dev/node-group=<group name>, so workloads can
target a group with a nodeSelector without extra labels.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Count | `count` | int | Yes | Count is the number of worker nodes in the group (at least 1). |
| Image | `image` | string | No | Image overrides node_image for this group's nodes (e.g. "kindest/node:v1.32.2"). Empty means node_image, or kind's default image when that is unset too. kind allows nodes of different Kubernetes ve... |
| Labels | `labels` | `map[string]string` | No | Labels are added to every node in the group. Keys in the kubernetes.io and k8s.io namespaces are rejected unless the kubelet may set them itself (node.kubernetes.io/ and kubelet.kubernetes.io/ pref... |
| ExtraMounts | `extra_mounts` | `[]KindMount` | No | ExtraMounts are mounted into this group's nodes only, in addition to the shared extra_mounts and NIC's GitOps mount. |

---

## KindMount

KindMount mounts a host directory into every kind node container.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| HostPath | `host_path` | string | Yes |  |
| ContainerPath | `container_path` | string | Yes |  |
| ReadOnly | `read_only` | bool | No |  |

