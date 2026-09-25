# Local Provider Configuration

Configuration options for local Kubernetes deployments.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [KindConfig](#kindconfig)
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
| Workers | `workers` | int | No | Workers is the number of worker nodes created alongside the control plane (default 0, a single node that runs everything). With workers, kind keeps the control plane tainted, so workloads run on th... |

---

## KindMount

KindMount mounts a host directory into every kind node container.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| HostPath | `host_path` | string | Yes |  |
| ContainerPath | `container_path` | string | Yes |  |
| ReadOnly | `read_only` | bool | No |  |

