# Local Provider Configuration

Configuration options for local Kubernetes (K3s) deployments.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config represents configuration for local Kubernetes deployments (K3s, kind, minikube).
Used for development and testing, or for "bring your own cluster" scenarios.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| KubeContext | `kube_context` | string | No | KubeContext specifies which kubectl context to use (from ~/.kube/config) |
| NodeSelectors | `node_selectors` | `map[string]map[string]string` | No | NodeSelectors maps workload types to node label selectors for scheduling |
