# Existing Cluster Configuration

Configuration options for attaching to an existing Kubernetes cluster.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config represents configuration for connecting to a pre-existing Kubernetes cluster.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Kubeconfig | `kubeconfig` | string | No | Kubeconfig is the path to the kubeconfig file. Defaults to KUBECONFIG env or ~/.kube/config when empty. |
| Context | `context` | string | Yes | Context is the name of the context entry in the kubeconfig file. Required — must be explicitly set to avoid accidentally deploying to the wrong cluster. |
| StorageClass | `storage_class` | string | No | StorageClass is the default Kubernetes StorageClass for persistent volumes. Defaults to "standard" when empty, or to "longhorn" when Longhorn is enabled below and StorageClass is left unset. |
| LoadBalancerAnnotations | `load_balancer_annotations` | `map[string]string` | No | LoadBalancerAnnotations are added to the Gateway's LoadBalancer Service. Use this to pass cloud-specific annotations the Cloud Controller Manager may require for provisioning LoadBalancers (e.g., "... |
| Longhorn | `longhorn` | `*longhorn.Config` | No | Longhorn opts the existing-cluster provider into installing Longhorn for distributed/replicated block + RWX storage. The block is required to opt-in (nil means "do not install"). Use this on bare-m... |

