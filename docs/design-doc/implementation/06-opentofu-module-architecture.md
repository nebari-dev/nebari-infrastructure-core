# OpenTofu Module Architecture

## 6.1 Scope

This document describes how OpenTofu is used inside NIC. **OpenTofu is not used by every provider.** Only the AWS cluster provider uses tofu today; Hetzner shells out to the `hetzner-k3s` binary, the local provider relies on Kind (driven from the Makefile), and the `existing` provider does not provision any infrastructure at all.

For the contract between CLI and provider implementations - which is what actually defines NIC's architecture - see the `Provider` interface in `pkg/provider/provider.go` and [System Overview](../architecture/02-system-overview.md).

## 6.2 Repository Layout (Real)

There is **no root-level `terraform/` directory**. AWS-specific templates live inside the AWS provider package:

```
pkg/provider/aws/
├── config.go                          # aws.Config struct (yaml/json tags)
├── provider.go                        # Implements provider.Provider
├── state.go                           # S3 state-bucket lifecycle (ensure / destroy)
├── tofu.go                            # Builds tfvars and invokes pkg/tofu.Setup
├── k8s.go                             # Shared kube-client construction
├── kubeconfig.go                      # GetKubeconfig implementation
├── efs.go                             # EFS storage-class wiring
├── longhorn.go                        # Longhorn storage installation
├── aws_load_balancer_controller.go    # AWS Load Balancer Controller install
├── cleanup.go / cleanup_k8s.go        # Pre-destroy resource cleanup
├── version.go                         # Provider-version probe
└── templates/                         # Embedded via go:embed
    ├── main.tf                        # Calls upstream nebari-dev/eks-cluster module
    ├── variables.tf                   # tfvars input schema
    ├── outputs.tf                     # Cluster name, endpoint, OIDC issuer, etc.
    ├── provider.tf                    # AWS provider config
    └── backend.tf                     # S3 backend with use_lockfile = true
```

Other cluster providers do not use OpenTofu and therefore have no `templates/` directory:

```
pkg/provider/hetzner/      # Wraps the hetzner-k3s binary
pkg/provider/local/        # Kind stub (Makefile creates the cluster)
pkg/provider/existing/     # Adopts an existing kubeconfig
pkg/provider/gcp/          # Stub: emits a "(stub)" status message and returns nil
pkg/provider/azure/        # Stub: emits a "(stub)" status message and returns nil
```

## 6.3 AWS Root Module

The AWS root module is intentionally thin. It is a single Terraform file that calls the upstream community module `nebari-dev/eks-cluster/aws`:

```hcl
# pkg/provider/aws/templates/main.tf
module "eks_cluster" {
  source  = "nebari-dev/eks-cluster/aws"
  version = "0.4.0"

  project_name                  = var.project_name
  tags                          = var.tags
  availability_zones            = var.availability_zones
  create_vpc                    = var.create_vpc
  vpc_cidr_block                = var.vpc_cidr_block
  kubernetes_version            = var.kubernetes_version
  endpoint_private_access       = var.endpoint_private_access
  endpoint_public_access        = var.endpoint_public_access
  node_groups                   = var.node_groups
  efs_enabled                   = var.efs_enabled
  efs_performance_mode          = var.efs_performance_mode
  efs_throughput_mode           = var.efs_throughput_mode
  efs_encrypted                 = var.efs_encrypted
  # ... see real templates/main.tf for the full set
}
```

The variables file (`templates/variables.tf`) declares the input schema (project name, availability zones, VPC, EKS version, node groups, EFS settings, etc.) and the outputs file (`templates/outputs.tf`) exposes the cluster name, API endpoint, certificate authority data, OIDC issuer URL, OIDC provider ARN, VPC ID, private subnet IDs, and EFS file system ID.

The backend is configured for S3 with native lockfile-based locking (`use_lockfile = true`). Bucket name and key are supplied via `-backend-config` at `tofu init` time. See [State Management](../architecture/05-state-management.md) for bucket naming and lifecycle.

## 6.4 Embedding and Extraction

Templates are embedded into the NIC binary via Go's `embed.FS` declared inside `pkg/provider/aws/`. At deploy time, the AWS provider:

1. Constructs a `map[string]any` of tfvars from the parsed AWS config (region, project name, node groups, EFS settings, etc.).
2. Calls `pkg/tofu.Setup(ctx, templatesFS, tfvars)`, which extracts the embedded files into a fresh temp directory, downloads the OpenTofu binary if not cached, writes `terraform.tfvars.json`, and returns a `TerraformExecutor`.
3. Calls `Init`, `Plan` (or `Apply`/`Destroy`) and lets `pkg/tofu` stream JSON output through the status channel.

There is no in-tree EKS HCL, no in-tree node group resources, and no in-tree IAM HCL beyond what the upstream module provides. The intent is to leverage a battle-tested community module instead of maintaining a parallel implementation.

## 6.5 Non-AWS Providers (Brief)

For completeness, the other providers do not have any `.tf` files:

| Provider | What it actually does |
|----------|----------------------|
| Hetzner | Generates a `hetzner-k3s` config file, invokes the binary, parses its output |
| Local | The provider itself is a thin adapter; the cluster is created by `make localkind-up` (Kind), and NIC's job is the bootstrap that follows |
| Existing | Reads `kubeconfig` and `context` from config; performs no provisioning |
| GCP, Azure | Registered, but every method currently returns "not yet implemented" |

If and when GCP/Azure are implemented, each provider package will decide independently whether to use OpenTofu (e.g., with the upstream `terraform-google-modules/kubernetes-engine` module for GKE) or another mechanism. The `Provider` interface is the boundary, not Terraform.

## 6.6 Adding a New Terraform-Backed Provider

The pattern, if you choose tofu for a new provider:

1. Create `pkg/provider/<name>/` with `config.go`, `provider.go`, and `tofu.go`.
2. Add a `templates/` directory inside the package with `main.tf`, `variables.tf`, `outputs.tf`, `provider.tf`, and (optionally) `backend.tf`. Embed it via `go:embed`.
3. Implement the `provider.Provider` interface. `Deploy` should build a tfvars map, call `pkg/tofu.Setup`, and invoke `Init`/`Plan`/`Apply` (or `Plan` only when `DeployOptions.DryRun` is true).
4. Implement `InfraSettings(cfg)` to return provider-shaped capabilities (`StorageClass`, `NeedsMetalLB`, `LoadBalancerAnnotations`, `KeycloakBasePath`, `HTTPSPort`, etc.). Do not add `switch` statements on provider name elsewhere in the codebase.
5. Register the provider in `cmd/nic/main.go` via `reg.ClusterProviders.Register(ctx, "<name>", New())`.
6. Add an example config under `examples/` and validate against `pkg/config`.

## 6.7 Anti-Patterns to Avoid

These came up during the previous design-doc audit and are not how NIC actually works:

- **Root `terraform/` directory with modules per provider.** Each provider owns its templates, embedded in the package.
- **A single root tofu module with `local.is_aws / is_gcp / is_azure` conditionals.** There is no single module; each provider has its own.
- **OpenTofu installing ArgoCD via `helm_release`.** NIC installs ArgoCD via the embedded Helm Go SDK (`pkg/helm`).
- **OpenTofu applying ArgoCD `Application` manifests via the Terraform kubernetes provider.** ArgoCD manifests are rendered into a Git repository by `pkg/argocd` and synced by ArgoCD.

See [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md) for the planned direction (out-of-tree gRPC plugins per provider kind), which makes the per-provider tool choice even more explicit.
