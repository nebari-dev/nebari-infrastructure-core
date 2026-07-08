# Local Provider Configuration

Configuration options for local Kubernetes deployments.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [KindConfig](#kindconfig)
- [KindMount](#kindmount)
- [MetalLBConfig](#metallbconfig)

---

## Config

Config represents local provider configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Kind | `kind` | `*KindConfig` | No |  |
| NodeSelectors | `node_selectors` | `map[string]map[string]string` | No |  |
| HTTPSPort | `https_port` | int | No |  |
| MetalLB | `metallb` | `*MetalLBConfig` | No |  |

---

## KindConfig

KindConfig holds optional config for the deployed kind cluster. It may be
omitted entirely (nil), in which case the cluster is created with defaults.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| NodeImage | `node_image` | string | No | NodeImage is the kindest/node image to use (e.g. "kindest/node:v1.32.2"). Empty means the default image of the bundled kind version. |
| ExtraMounts | `extra_mounts` | `[]KindMount` | No | ExtraMounts are additional host directories mounted into the cluster node container. The local file:// gitops repository (explicit or auto-created) is mounted automatically and does not need to be ... |

---

## KindMount

KindMount mounts a host directory into the kind node container.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| HostPath | `host_path` | string | Yes |  |
| ContainerPath | `container_path` | string | Yes |  |
| ReadOnly | `read_only` | bool | No |  |

---

## MetalLBConfig

MetalLBConfig holds MetalLB-specific settings for the local provider.
MetalLB is always enabled on local clusters — kind has no built-in
LoadBalancer, so disabling it would leave the gateway without an IP.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| AddressPool | `address_pool` | string | No | AddressPool is the IP range for MetalLB's IPAddressPool. When unset, NIC derives a pool from the kind Docker network during Deploy. |

