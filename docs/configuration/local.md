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
| Kind | `kind` | `*KindConfig` | No |  |
| NodeSelectors | `node_selectors` | `map[string]map[string]string` | No |  |
| HTTPSPort | `https_port` | int | No |  |
| MetalLB | `metallb` | `*MetalLBConfig` | No |  |

---

## MetalLBConfig

MetalLBConfig holds MetalLB-specific settings for the local provider.
MetalLB is always enabled on local clusters — kind has no built-in
LoadBalancer, so disabling it would leave the gateway without an IP.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| AddressPool | `address_pool` | string | No | AddressPool is the IP range for MetalLB's IPAddressPool. When unset, NIC derives a pool from the kind Docker network during Deploy. |

