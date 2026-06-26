# Local Provider Configuration

Configuration options for local Kubernetes deployments.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [MetalLBConfig](#metallbconfig)

---

## Config

Config represents local provider configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| KubeContext | `kube_context` | string | No |  |
| NodeSelectors | `node_selectors` | `map[string]map[string]string` | No |  |
| StorageClass | `storage_class` | string | No |  |
| HTTPSPort | `https_port` | int | No |  |
| MetalLB | `metallb` | `*MetalLBConfig` | No |  |

---

## MetalLBConfig

MetalLBConfig holds MetalLB-specific settings for the local provider.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No | Enabled controls whether MetalLB is deployed. Default: true. Use a pointer to distinguish "not set" (default true) from "explicitly false". |
| AddressPool | `address_pool` | string | No | AddressPool is the IP range for MetalLB's IPAddressPool. Default: "192.168.1.100-192.168.1.110" |

