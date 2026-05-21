# terraform-azurerm-aks-cluster Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and release `github.com/nebari-dev/terraform-azurerm-aks-cluster` v0.1.0 — a standalone Terraform module that provisions an Azure AKS cluster (system + user node pools, VNet with Azure CNI Overlay, optional BYO networking/resource group), mirroring the conventions of `terraform-aws-eks-cluster`.

**Architecture:** Single root module under `main.tf` composing `azurerm_resource_group`, `azurerm_virtual_network`, `azurerm_subnet`, `azurerm_user_assigned_identity`, `azurerm_kubernetes_cluster` (system pool inline), `azurerm_kubernetes_cluster_node_pool` (user pools via `for_each`), and `azurerm_role_assignment` (BYO-network case only). A `modules/identity/` sub-module is created as a seam for future user-assigned-identity / Workload Identity work. CI mirrors the AWS sibling: OpenTofu fmt/validate/tflint/terraform-docs on every push; Terratest gated on `workflow_dispatch` + ready-for-review PRs with Azure OIDC auth. Tagging `v0.1.0` triggers Terraform Registry auto-publish at `nebari-dev/aks-cluster/azurerm`.

**Tech Stack:** OpenTofu 1.11+, Terraform azurerm provider >= 4.0, Go 1.25+ for Terratest, terraform-docs, TFLint with azurerm ruleset, GitHub Actions, Azure OIDC.

**Reference spec:** `docs/superpowers/specs/2026-05-21-azure-aks-terraform-module-and-nic-provider-design.md` in the NIC repo.

---

## Prerequisites (before Task 1)

The engineer should confirm before starting:

- An Azure subscription accessible via `az login` (`az account show` returns a subscription).
- A GitHub org `nebari-dev` with permission to create a public repo named `terraform-azurerm-aks-cluster`.
- Local tools installed:
  - OpenTofu >= 1.11 (`tofu version`)
  - Go >= 1.25 (`go version`)
  - `terraform-docs` (`terraform-docs --version`)
  - `tflint` >= 0.60 (`tflint --version`)
  - `pre-commit` (`pre-commit --version`)
  - `az` CLI (`az version`)
- Cross-reference repo: `~/gh/terraform-aws-eks-cluster` (used throughout as the pattern reference).

Create and `cd` into the new repo for all tasks:

```bash
mkdir -p ~/gh/terraform-azurerm-aks-cluster
cd ~/gh/terraform-azurerm-aks-cluster
git init -b main
```

---

## File Structure

The final repo will contain:

| Path | Responsibility |
|---|---|
| `versions.tf` | Required Terraform + azurerm provider versions |
| `providers.tf` | Placeholder for consumer-configured azurerm provider |
| `variables.tf` | All input variables with types, defaults, validations |
| `outputs.tf` | All outputs the consumer needs (cluster, kubeconfig, identity, network) |
| `locals.tf` | RG/VNet ID resolution, tag merging, system-pool selection |
| `main.tf` | Composes RG, VNet, subnet, identity, AKS cluster, user node pools |
| `modules/identity/` | Seam sub-module — initially empty interface for role-assignment helpers |
| `examples/complete/` | Reference deployment exercising every input |
| `examples/existing-resources/` | BYO resource group + BYO VNet |
| `test/module_test.go` | Terratest end-to-end (managed-disk CSI smoke) |
| `test/fixtures/disk-csi/` | k8s manifests applied by the test |
| `.github/workflows/ci.yml` | fmt/validate/lint/docs on every push |
| `.github/workflows/test.yml` | Terratest on workflow_dispatch + ready-for-review |
| `.tflint.hcl` | TFLint config with azurerm ruleset |
| `.terraform-docs.yml` | terraform-docs config (README injection) |
| `.pre-commit-config.yaml` | Pre-commit hooks |
| `Makefile` | `init/fmt/validate/lint/test/docs/clean/ci` targets |
| `.gitignore`, `LICENSE` (Apache 2.0), `README.md` | Repo meta |

---

## Phase A1 — Scaffolding (Tasks 1–9)

### Task 1: Repo bootstrap and meta files

**Files:**
- Create: `~/gh/terraform-azurerm-aks-cluster/.gitignore`
- Create: `~/gh/terraform-azurerm-aks-cluster/LICENSE`
- Create: `~/gh/terraform-azurerm-aks-cluster/README.md`

- [ ] **Step 1: Create `.gitignore` (copied from AWS sibling)**

```gitignore
# Local .terraform directories
**/.terraform/*

# .tfstate files
*.tfstate
*.tfstate.*

# Crash log files
crash.log

# Exclude all .tfvars files (may contain secrets)
*.tfvars
*.tfvars.json

# Ignore override files
override.tf
override.tf.json
*_override.tf
*_override.tf.json

# Ignore CLI configuration
.terraformrc
terraform.rc

# Go test artifacts
test/vendor/
test/.terraform.lock.hcl

# Editor
.vscode/
.idea/
*.swp
```

- [ ] **Step 2: Add `LICENSE` (Apache 2.0 matching AWS sibling)**

Copy verbatim from `~/gh/terraform-aws-eks-cluster/LICENSE`. The AWS sibling uses the unmodified Apache License 2.0 template; no year/copyright-holder edits are needed.

```bash
cp ~/gh/terraform-aws-eks-cluster/LICENSE ~/gh/terraform-azurerm-aks-cluster/LICENSE
```

- [ ] **Step 3: Write `README.md` skeleton (terraform-docs will fill it later)**

```markdown
# terraform-azurerm-aks-cluster

A Terraform module that provisions an Azure Kubernetes Service (AKS) cluster for Nebari.

Mirrors the conventions of [terraform-aws-eks-cluster](https://github.com/nebari-dev/terraform-aws-eks-cluster).

## Usage

See [`examples/complete`](./examples/complete) for a reference deployment.

<!-- BEGIN_TF_DOCS -->
<!-- END_TF_DOCS -->
```

- [ ] **Step 4: Commit**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
git add .gitignore LICENSE README.md
git commit -m "chore: initial repo bootstrap"
```

---

### Task 2: Add Terraform versions + providers placeholder

**Files:**
- Create: `versions.tf`
- Create: `providers.tf`

- [ ] **Step 1: Write `versions.tf`**

```hcl
terraform {
  required_version = ">= 1.9"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0"
    }
  }
}
```

- [ ] **Step 2: Write `providers.tf` (empty — consumer configures)**

```hcl
# This file is intentionally empty.
# Consumers of this module configure the azurerm provider in their root module.
# See examples/complete/providers.tf for a reference configuration.
```

- [ ] **Step 3: Verify `tofu init -backend=false` works**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
tofu init -backend=false
```

Expected: `OpenTofu has been successfully initialized!` (no errors). Will pull the azurerm provider.

- [ ] **Step 4: Commit**

```bash
git add versions.tf providers.tf
git commit -m "feat: add versions and providers placeholder"
```

---

### Task 3: Add `.tflint.hcl`, `.terraform-docs.yml`, `.pre-commit-config.yaml`

**Files:**
- Create: `.tflint.hcl`
- Create: `.terraform-docs.yml`
- Create: `.pre-commit-config.yaml`

- [ ] **Step 1: Write `.tflint.hcl`**

```hcl
config {
  format = "compact"
}

plugin "terraform" {
  enabled = true
  preset  = "recommended"
}

plugin "azurerm" {
  enabled = true
  version = "0.30.0"
  source  = "github.com/terraform-linters/tflint-ruleset-azurerm"
}
```

- [ ] **Step 2: Write `.terraform-docs.yml` (mirrors AWS sibling)**

Copy `~/gh/terraform-aws-eks-cluster/.terraform-docs.yml` as the starting point:

```bash
cp ~/gh/terraform-aws-eks-cluster/.terraform-docs.yml ~/gh/terraform-azurerm-aks-cluster/.terraform-docs.yml
```

Then edit the `content:` section's "Usage" example to reference Azure resources (placeholder for now; will refine in Task 21):

Open `.terraform-docs.yml` and replace any `aws` / `eks` / `vpc` references in the `content:` block with `azurerm` / `aks` / `vnet` equivalents. Keep the structure (Header, Usage, Requirements, Providers, Modules, Resources, Inputs, Outputs).

- [ ] **Step 3: Write `.pre-commit-config.yaml`**

```yaml
repos:
  - repo: https://github.com/antonbabenko/pre-commit-terraform
    rev: v1.96.1
    hooks:
      - id: terraform_fmt
      - id: terraform_validate
        args:
          - --hook-config=--retry-once-with-cleanup=true
          - --tf-init-args=-backend=false
      - id: terraform_docs
        args:
          - --hook-config=--path-to-file=README.md
          - --hook-config=--add-to-existing-file=true
          - --hook-config=--create-file-if-not-exist=true
      - id: terraform_tflint
        args:
          - --args=--config=__GIT_WORKING_DIR__/.tflint.hcl

  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.6.0
    hooks:
      - id: trailing-whitespace
      - id: end-of-file-fixer
      - id: check-yaml
      - id: check-added-large-files
        args: ['--maxkb=100']
      - id: detect-private-key
```

- [ ] **Step 4: Install tflint plugin and run validate**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
tflint --init
tflint
```

Expected: `tflint --init` downloads the azurerm plugin; `tflint` returns 0 (no issues on an empty module).

- [ ] **Step 5: Commit**

```bash
git add .tflint.hcl .terraform-docs.yml .pre-commit-config.yaml
git commit -m "chore: add tflint, terraform-docs, and pre-commit configs"
```

---

### Task 4: Add Makefile

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Write `Makefile`**

Use `~/gh/terraform-aws-eks-cluster/Makefile` as the starting point; the target names are identical. The only target-body difference is no AWS-specific tooling.

```bash
cp ~/gh/terraform-aws-eks-cluster/Makefile ~/gh/terraform-azurerm-aks-cluster/Makefile
```

Open `Makefile` and edit any AWS-specific references. Specifically:

- Any test target should reference `./test/...` (no change needed — already generic).
- Any tflint plugin install line: change `tflint-ruleset-aws` references to `tflint-ruleset-azurerm`.
- Repo description in any help text: replace `EKS` with `AKS`, `terraform-aws-eks-cluster` with `terraform-azurerm-aks-cluster`.

- [ ] **Step 2: Verify make targets work**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
make fmt
make validate
make lint
make docs
```

Expected: each completes without error. `make docs` updates README between the BEGIN/END markers (will be empty Inputs/Outputs at this point — that's fine).

- [ ] **Step 3: Commit**

```bash
git add Makefile README.md
git commit -m "chore: add Makefile with standard targets"
```

---

### Task 5: Add CI workflow (fmt/validate/lint/docs)

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write `.github/workflows/ci.yml`**

Use `~/gh/terraform-aws-eks-cluster/.github/workflows/ci.yml` as the template. The only changes are TFLint plugin name and any project-name references.

```bash
mkdir -p ~/gh/terraform-azurerm-aks-cluster/.github/workflows
cp ~/gh/terraform-aws-eks-cluster/.github/workflows/ci.yml \
   ~/gh/terraform-azurerm-aks-cluster/.github/workflows/ci.yml
```

Open `.github/workflows/ci.yml` and replace any AWS-specific references:

- `terraform-linters/tflint-ruleset-aws` → `terraform-linters/tflint-ruleset-azurerm`
- Pin the azurerm plugin version to `0.30.0` (or current matching `.tflint.hcl`).
- Any workflow name with "AWS" or "EKS" → "Azure" or "AKS".
- Job environment variables: remove `AWS_REGION` if present; nothing Azure-specific is needed for fmt/validate/lint/docs.

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add fmt/validate/lint/docs workflow"
```

---

### Task 6: Skeleton `variables.tf` (no resources yet)

**Files:**
- Create: `variables.tf`

- [ ] **Step 1: Write `variables.tf`**

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Common
# ───────────────────────────────────────────────────────────────────────────

variable "project_name" {
  type        = string
  description = "Name prefix applied to all resources (e.g. \"my-nebari-azure\")."
}

variable "location" {
  type        = string
  description = "Azure region (e.g. \"eastus\")."
}

variable "tags" {
  type        = map(string)
  description = "Additional tags applied to all resources. Module-level NIC tags are merged in."
  default     = {}
}

# ───────────────────────────────────────────────────────────────────────────
# Resource group
# ───────────────────────────────────────────────────────────────────────────

variable "create_resource_group" {
  type        = bool
  description = "If true, the module creates the resource group. If false, existing_resource_group_name must be set."
  default     = true
}

variable "existing_resource_group_name" {
  type        = string
  description = "Name of an existing resource group to use when create_resource_group=false."
  default     = null
}

# ───────────────────────────────────────────────────────────────────────────
# Networking
# ───────────────────────────────────────────────────────────────────────────

variable "create_vnet" {
  type        = bool
  description = "If true, the module creates a VNet and node subnet. If false, existing_vnet_id and existing_node_subnet_id must be set."
  default     = true
}

variable "vnet_cidr_block" {
  type        = string
  description = "VNet CIDR when create_vnet=true."
  default     = "10.0.0.0/16"
}

variable "node_subnet_cidr_block" {
  type        = string
  description = "Node subnet CIDR when create_vnet=true."
  default     = "10.0.0.0/22"
}

variable "existing_vnet_id" {
  type        = string
  description = "Full resource ID of an existing VNet when create_vnet=false."
  default     = null
}

variable "existing_node_subnet_id" {
  type        = string
  description = "Full resource ID of an existing subnet for AKS nodes when create_vnet=false."
  default     = null
}

variable "network_plugin" {
  type        = string
  description = "AKS network plugin. \"azure\" (recommended) or \"kubenet\"."
  default     = "azure"
  validation {
    condition     = contains(["azure", "kubenet"], var.network_plugin)
    error_message = "network_plugin must be one of: azure, kubenet."
  }
}

variable "network_plugin_mode" {
  type        = string
  description = "AKS network plugin mode. \"overlay\" (recommended for new clusters) or null for legacy Azure CNI."
  default     = "overlay"
}

variable "pod_cidr" {
  type        = string
  description = "Pod CIDR when network_plugin_mode=\"overlay\"."
  default     = "10.244.0.0/16"
}

variable "service_cidr" {
  type        = string
  description = "Kubernetes service CIDR. Must not overlap with VNet or pod_cidr."
  default     = "10.0.16.0/22"
}

variable "dns_service_ip" {
  type        = string
  description = "IP address within service_cidr used by CoreDNS."
  default     = "10.0.16.10"
}

# ───────────────────────────────────────────────────────────────────────────
# Cluster
# ───────────────────────────────────────────────────────────────────────────

variable "kubernetes_version" {
  type        = string
  description = "Kubernetes version (e.g. \"1.34\"). If null, AKS picks the current default."
  default     = null
}

variable "private_cluster_enabled" {
  type        = bool
  description = "If true, the API server is reachable only via a private endpoint."
  default     = false
}

variable "authorized_ip_ranges" {
  type        = list(string)
  description = "List of CIDRs allowed to reach the API server. Ignored when private_cluster_enabled=true."
  default     = []
}

variable "sku_tier" {
  type        = string
  description = "AKS SKU tier. \"Free\", \"Standard\", or \"Premium\"."
  default     = "Free"
  validation {
    condition     = contains(["Free", "Standard", "Premium"], var.sku_tier)
    error_message = "sku_tier must be one of: Free, Standard, Premium."
  }
}

variable "identity_type" {
  type        = string
  description = "AKS managed-identity type. Currently only \"SystemAssigned\" is supported by this module."
  default     = "SystemAssigned"
  validation {
    condition     = var.identity_type == "SystemAssigned"
    error_message = "Only SystemAssigned is supported in this version."
  }
}

# ───────────────────────────────────────────────────────────────────────────
# Node groups
# ───────────────────────────────────────────────────────────────────────────

variable "node_groups" {
  type = map(object({
    vm_size         = string
    min_count       = number
    max_count       = number
    mode            = optional(string, "User")
    os_disk_size_gb = optional(number, 128)
    labels          = optional(map(string), {})
    taints          = optional(list(string), [])
    zones           = optional(list(string), [])
  }))
  description = "Map of node-pool name to config. Exactly one pool must have mode=\"System\"; if none specified, the first entry is defaulted to System."

  validation {
    condition     = length(var.node_groups) >= 1
    error_message = "At least one node group is required."
  }

  validation {
    condition = length([
      for name, ng in var.node_groups : name if ng.mode == "System"
    ]) <= 1
    error_message = "At most one node group may have mode=\"System\"."
  }
}
```

- [ ] **Step 2: Run validate**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
tofu validate
```

Expected: `Success! The configuration is valid.` (note: no outputs yet so terraform-docs will warn — that's fine).

- [ ] **Step 3: Commit**

```bash
git add variables.tf
git commit -m "feat: declare module input variables"
```

---

### Task 7: Skeleton `outputs.tf`

**Files:**
- Create: `outputs.tf`

- [ ] **Step 1: Write `outputs.tf` (placeholder values; resources don't exist yet)**

For now every output is `null`. When resources land in later tasks, each output gets wired up. This lets `tofu validate` and terraform-docs run cleanly throughout.

```hcl
output "cluster_id" {
  description = "Full Azure resource ID of the AKS cluster."
  value       = null
}

output "cluster_name" {
  description = "Name of the AKS cluster."
  value       = null
}

output "cluster_fqdn" {
  description = "Fully-qualified domain name of the AKS API server."
  value       = null
}

output "host" {
  description = "URL of the AKS API server (for kubeconfig server field)."
  value       = null
}

output "kube_admin_config_raw" {
  description = "Ready-to-use kubeconfig for admin access."
  value       = null
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate of the AKS API server."
  value       = null
  sensitive   = true
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL of the AKS cluster (for future Workload Identity work)."
  value       = null
}

output "kubelet_identity_object_id" {
  description = "Object ID of the user-assigned kubelet identity."
  value       = null
}

output "kubelet_identity_client_id" {
  description = "Client ID of the user-assigned kubelet identity."
  value       = null
}

output "node_resource_group" {
  description = "Name of the AKS-managed node resource group (MC_*)."
  value       = null
}

output "resource_group_name" {
  description = "Name of the resource group containing the cluster (created or BYO)."
  value       = null
}

output "vnet_id" {
  description = "Full Azure resource ID of the VNet (created or BYO)."
  value       = null
}

output "node_subnet_id" {
  description = "Full Azure resource ID of the node subnet."
  value       = null
}

output "kubeconfig_command" {
  description = "Convenience command to fetch a kubeconfig via the Azure CLI."
  value       = null
}
```

- [ ] **Step 2: Run validate**

```bash
tofu validate
```

Expected: `Success! The configuration is valid.`

- [ ] **Step 3: Commit**

```bash
git add outputs.tf
git commit -m "feat: declare module outputs (placeholders)"
```

---

### Task 8: Skeleton `locals.tf` and `main.tf`

**Files:**
- Create: `locals.tf`
- Create: `main.tf`

- [ ] **Step 1: Write `locals.tf` with tag merging and system-pool selection**

```hcl
locals {
  # Tags merged onto every resource so NIC's tag-based discovery works.
  tags = merge(var.tags, {
    "nic.nebari.dev_cluster-name" = var.project_name
    "nic.nebari.dev_managed-by"   = "nic"
  })

  # Identify the system pool. If exactly one node group has mode="System",
  # use it. Otherwise default to the first key (alphabetical by Terraform's
  # map iteration).
  explicit_system_pools = [
    for name, ng in var.node_groups : name if ng.mode == "System"
  ]
  system_pool_name = length(local.explicit_system_pools) > 0 ? local.explicit_system_pools[0] : keys(var.node_groups)[0]
  system_pool      = var.node_groups[local.system_pool_name]

  # User pools = everything except the system pool.
  user_pools = {
    for name, ng in var.node_groups : name => ng
    if name != local.system_pool_name
  }
}
```

- [ ] **Step 2: Write empty `main.tf` placeholder**

```hcl
# Resources are added in subsequent tasks.
```

- [ ] **Step 3: Run validate**

```bash
tofu validate
```

Expected: `Success! The configuration is valid.`

- [ ] **Step 4: Commit**

```bash
git add locals.tf main.tf
git commit -m "feat: add locals with tag merging and system-pool selection"
```

---

### Task 9: Push initial scaffold to GitHub and verify CI passes

- [ ] **Step 1: Create the GitHub repo**

```bash
gh repo create nebari-dev/terraform-azurerm-aks-cluster --public \
  --description "Terraform module that provisions an Azure AKS cluster for Nebari" \
  --source ~/gh/terraform-azurerm-aks-cluster \
  --push
```

Expected: repo created at https://github.com/nebari-dev/terraform-azurerm-aks-cluster; `main` branch pushed.

- [ ] **Step 2: Watch CI run on the initial push**

```bash
gh run watch
```

Expected: all four ci.yml jobs (format, validate, lint, docs) pass.

If a job fails, fix locally, commit, push, and re-watch. Do not proceed until CI is green.

- [ ] **Step 3: No additional commit** — scaffold is committed and pushed.

---

## Phase A2 — Resources (Tasks 10–16)

### Task 10: Implement resource group (BYO + create)

**Files:**
- Modify: `main.tf`
- Modify: `locals.tf`
- Modify: `outputs.tf`

- [ ] **Step 1: Update `main.tf` with RG resources**

Replace `main.tf` with:

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Resource group
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_resource_group" "this" {
  count = var.create_resource_group ? 1 : 0

  name     = "${var.project_name}-rg"
  location = var.location
  tags     = local.tags
}

data "azurerm_resource_group" "existing" {
  count = var.create_resource_group ? 0 : 1

  name = var.existing_resource_group_name
}
```

- [ ] **Step 2: Add RG identity locals to `locals.tf`**

Append to `locals.tf`:

```hcl
locals {
  resource_group_name     = var.create_resource_group ? azurerm_resource_group.this[0].name     : data.azurerm_resource_group.existing[0].name
  resource_group_location = var.create_resource_group ? azurerm_resource_group.this[0].location : data.azurerm_resource_group.existing[0].location
}
```

Note: this is a *second* `locals` block. Terraform merges multiple `locals` blocks within the same file — keeping them split by concern (the first block is tags + node-group selection; this one is post-creation lookups).

- [ ] **Step 3: Wire `resource_group_name` output**

In `outputs.tf`, change:

```hcl
output "resource_group_name" {
  description = "Name of the resource group containing the cluster (created or BYO)."
  value       = local.resource_group_name
}
```

- [ ] **Step 4: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 5: Commit**

```bash
git add main.tf locals.tf outputs.tf
git commit -m "feat: provision (or look up) resource group"
```

---

### Task 11: Implement VNet, subnet, and a role assignment placeholder

**Files:**
- Modify: `main.tf`
- Modify: `locals.tf`
- Modify: `outputs.tf`

- [ ] **Step 1: Append VNet and subnet resources to `main.tf`**

Append to `main.tf`:

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Virtual network + node subnet
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_virtual_network" "this" {
  count = var.create_vnet ? 1 : 0

  name                = "${var.project_name}-vnet"
  location            = local.resource_group_location
  resource_group_name = local.resource_group_name
  address_space       = [var.vnet_cidr_block]
  tags                = local.tags
}

resource "azurerm_subnet" "nodes" {
  count = var.create_vnet ? 1 : 0

  name                 = "${var.project_name}-nodes"
  resource_group_name  = local.resource_group_name
  virtual_network_name = azurerm_virtual_network.this[0].name
  address_prefixes     = [var.node_subnet_cidr_block]
}
```

- [ ] **Step 2: Add VNet/subnet ID locals**

Append to the second `locals` block in `locals.tf`:

```hcl
  vnet_id        = var.create_vnet ? azurerm_virtual_network.this[0].id : var.existing_vnet_id
  node_subnet_id = var.create_vnet ? azurerm_subnet.nodes[0].id         : var.existing_node_subnet_id
```

- [ ] **Step 3: Wire outputs**

In `outputs.tf` update:

```hcl
output "vnet_id" {
  description = "Full Azure resource ID of the VNet (created or BYO)."
  value       = local.vnet_id
}

output "node_subnet_id" {
  description = "Full Azure resource ID of the node subnet."
  value       = local.node_subnet_id
}
```

- [ ] **Step 4: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 5: Commit**

```bash
git add main.tf locals.tf outputs.tf
git commit -m "feat: provision (or look up) VNet and node subnet"
```

---

### Task 12: Implement user-assigned kubelet identity

**Files:**
- Modify: `main.tf`
- Modify: `outputs.tf`

- [ ] **Step 1: Append the kubelet identity resource to `main.tf`**

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Kubelet identity (user-assigned)
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_user_assigned_identity" "kubelet" {
  name                = "${var.project_name}-kubelet"
  location            = local.resource_group_location
  resource_group_name = local.resource_group_name
  tags                = local.tags
}
```

- [ ] **Step 2: Wire identity outputs**

In `outputs.tf`:

```hcl
output "kubelet_identity_object_id" {
  description = "Object ID of the user-assigned kubelet identity."
  value       = azurerm_user_assigned_identity.kubelet.principal_id
}

output "kubelet_identity_client_id" {
  description = "Client ID of the user-assigned kubelet identity."
  value       = azurerm_user_assigned_identity.kubelet.client_id
}
```

- [ ] **Step 3: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 4: Commit**

```bash
git add main.tf outputs.tf
git commit -m "feat: provision user-assigned kubelet identity"
```

---

### Task 13: Implement AKS cluster with inline system node pool

**Files:**
- Modify: `main.tf`
- Modify: `outputs.tf`

- [ ] **Step 1: Append the AKS cluster resource to `main.tf`**

```hcl
# ───────────────────────────────────────────────────────────────────────────
# AKS cluster (system node pool inline)
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_kubernetes_cluster" "this" {
  name                = "${var.project_name}-aks"
  location            = local.resource_group_location
  resource_group_name = local.resource_group_name
  dns_prefix          = var.project_name
  kubernetes_version  = var.kubernetes_version
  sku_tier            = var.sku_tier

  private_cluster_enabled = var.private_cluster_enabled

  api_server_access_profile {
    authorized_ip_ranges = var.private_cluster_enabled || length(var.authorized_ip_ranges) == 0 ? null : var.authorized_ip_ranges
  }

  default_node_pool {
    name                 = local.system_pool_name
    vm_size              = local.system_pool.vm_size
    min_count            = local.system_pool.min_count
    max_count            = local.system_pool.max_count
    auto_scaling_enabled = true
    os_disk_size_gb      = local.system_pool.os_disk_size_gb
    vnet_subnet_id       = local.node_subnet_id
    node_labels          = local.system_pool.labels
    zones                = local.system_pool.zones
    tags                 = local.tags
  }

  identity {
    type = var.identity_type
  }

  kubelet_identity {
    user_assigned_identity_id = azurerm_user_assigned_identity.kubelet.id
    object_id                 = azurerm_user_assigned_identity.kubelet.principal_id
    client_id                 = azurerm_user_assigned_identity.kubelet.client_id
  }

  network_profile {
    network_plugin      = var.network_plugin
    network_plugin_mode = var.network_plugin_mode
    pod_cidr            = var.network_plugin_mode == "overlay" ? var.pod_cidr : null
    service_cidr        = var.service_cidr
    dns_service_ip      = var.dns_service_ip
  }

  tags = local.tags
}
```

- [ ] **Step 2: Wire cluster outputs**

In `outputs.tf`:

```hcl
output "cluster_id" {
  description = "Full Azure resource ID of the AKS cluster."
  value       = azurerm_kubernetes_cluster.this.id
}

output "cluster_name" {
  description = "Name of the AKS cluster."
  value       = azurerm_kubernetes_cluster.this.name
}

output "cluster_fqdn" {
  description = "Fully-qualified domain name of the AKS API server."
  value       = azurerm_kubernetes_cluster.this.fqdn
}

output "host" {
  description = "URL of the AKS API server (for kubeconfig server field)."
  value       = azurerm_kubernetes_cluster.this.kube_admin_config[0].host
  sensitive   = true
}

output "kube_admin_config_raw" {
  description = "Ready-to-use kubeconfig for admin access."
  value       = azurerm_kubernetes_cluster.this.kube_admin_config_raw
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate of the AKS API server."
  value       = azurerm_kubernetes_cluster.this.kube_admin_config[0].cluster_ca_certificate
  sensitive   = true
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL of the AKS cluster."
  value       = azurerm_kubernetes_cluster.this.oidc_issuer_url
}

output "node_resource_group" {
  description = "Name of the AKS-managed node resource group (MC_*)."
  value       = azurerm_kubernetes_cluster.this.node_resource_group
}

output "kubeconfig_command" {
  description = "Convenience command to fetch a kubeconfig via the Azure CLI."
  value       = "az aks get-credentials --resource-group ${local.resource_group_name} --name ${azurerm_kubernetes_cluster.this.name} --admin"
}
```

- [ ] **Step 3: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 4: Commit**

```bash
git add main.tf outputs.tf
git commit -m "feat: provision AKS cluster with inline system node pool"
```

---

### Task 14: Implement user node pools

**Files:**
- Modify: `main.tf`

- [ ] **Step 1: Append the user node pool resource to `main.tf`**

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Additional user node pools
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_kubernetes_cluster_node_pool" "user" {
  for_each = local.user_pools

  name                  = each.key
  kubernetes_cluster_id = azurerm_kubernetes_cluster.this.id
  vm_size               = each.value.vm_size
  min_count             = each.value.min_count
  max_count             = each.value.max_count
  auto_scaling_enabled  = true
  mode                  = each.value.mode
  os_disk_size_gb       = each.value.os_disk_size_gb
  vnet_subnet_id        = local.node_subnet_id
  node_labels           = each.value.labels
  node_taints           = each.value.taints
  zones                 = each.value.zones
  tags                  = local.tags
}
```

- [ ] **Step 2: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 3: Commit**

```bash
git add main.tf
git commit -m "feat: provision additional user node pools"
```

---

### Task 15: Implement role assignment for BYO networking

**Files:**
- Modify: `main.tf`

- [ ] **Step 1: Append the role assignment to `main.tf`**

When BYO networking is used, the AKS cluster's system-assigned identity needs `Network Contributor` on the existing subnet so AKS can manage subnet delegations.

```hcl
# ───────────────────────────────────────────────────────────────────────────
# Role assignment: AKS identity → existing subnet (only when BYO networking)
# ───────────────────────────────────────────────────────────────────────────

resource "azurerm_role_assignment" "network_contributor" {
  count = var.create_vnet ? 0 : 1

  scope                = var.existing_node_subnet_id
  role_definition_name = "Network Contributor"
  principal_id         = azurerm_kubernetes_cluster.this.identity[0].principal_id
}
```

- [ ] **Step 2: Run validate**

```bash
tofu validate
```

Expected: `Success!`

- [ ] **Step 3: Commit**

```bash
git add main.tf
git commit -m "feat: grant Network Contributor on BYO subnet"
```

---

### Task 16: Create `modules/identity/` seam sub-module

**Files:**
- Create: `modules/identity/variables.tf`
- Create: `modules/identity/main.tf`
- Create: `modules/identity/outputs.tf`
- Create: `modules/identity/versions.tf`
- Create: `modules/identity/README.md`

This sub-module is intentionally minimal in MVP — its job is to *exist* as a stable interface for future Workload Identity / user-assigned-identity-for-AKS work. It currently exposes nothing the root module uses; it's not called from the root `main.tf` yet.

- [ ] **Step 1: Write `modules/identity/versions.tf`**

```hcl
terraform {
  required_version = ">= 1.9"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0"
    }
  }
}
```

- [ ] **Step 2: Write `modules/identity/variables.tf`**

```hcl
variable "name" {
  type        = string
  description = "Name of the user-assigned managed identity."
}

variable "location" {
  type        = string
  description = "Azure region."
}

variable "resource_group_name" {
  type        = string
  description = "Resource group that contains the identity."
}

variable "role_assignments" {
  type = map(object({
    scope                = string
    role_definition_name = string
  }))
  description = "Map of role assignments to create. Key is a stable identifier; value is the scope and role."
  default     = {}
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 3: Write `modules/identity/main.tf`**

```hcl
resource "azurerm_user_assigned_identity" "this" {
  name                = var.name
  location            = var.location
  resource_group_name = var.resource_group_name
  tags                = var.tags
}

resource "azurerm_role_assignment" "this" {
  for_each = var.role_assignments

  scope                = each.value.scope
  role_definition_name = each.value.role_definition_name
  principal_id         = azurerm_user_assigned_identity.this.principal_id
}
```

- [ ] **Step 4: Write `modules/identity/outputs.tf`**

```hcl
output "id" {
  description = "Full Azure resource ID of the identity."
  value       = azurerm_user_assigned_identity.this.id
}

output "client_id" {
  description = "Client ID of the identity."
  value       = azurerm_user_assigned_identity.this.client_id
}

output "principal_id" {
  description = "Principal/object ID of the identity."
  value       = azurerm_user_assigned_identity.this.principal_id
}
```

- [ ] **Step 5: Write `modules/identity/README.md`**

```markdown
# modules/identity

User-assigned managed identity + optional role assignments.

This sub-module is a seam for future Workload Identity / user-assigned-AKS-identity work. The root module does not call it in v0.1.0.

## Usage

\`\`\`hcl
module "kubelet_identity" {
  source              = "../../modules/identity"
  name                = "my-cluster-kubelet"
  location            = "eastus"
  resource_group_name = "my-rg"

  role_assignments = {
    acr_pull = {
      scope                = azurerm_container_registry.this.id
      role_definition_name = "AcrPull"
    }
  }
}
\`\`\`
```

- [ ] **Step 6: Run validate from the sub-module dir**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/modules/identity
tofu init -backend=false
tofu validate
cd ~/gh/terraform-azurerm-aks-cluster
```

Expected: `Success!`

- [ ] **Step 7: Commit**

```bash
git add modules/
git commit -m "feat: add modules/identity seam for future identity work"
```

---

## Phase A2 (continued) — Examples (Tasks 17–18)

### Task 17: Write `examples/complete/`

**Files:**
- Create: `examples/complete/main.tf`
- Create: `examples/complete/providers.tf`
- Create: `examples/complete/versions.tf`
- Create: `examples/complete/variables.tf`
- Create: `examples/complete/outputs.tf`
- Create: `examples/complete/README.md`

- [ ] **Step 1: Write `examples/complete/versions.tf`**

```hcl
terraform {
  required_version = ">= 1.9"
  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0"
    }
  }
}
```

- [ ] **Step 2: Write `examples/complete/providers.tf`**

```hcl
provider "azurerm" {
  features {}
}
```

- [ ] **Step 3: Write `examples/complete/variables.tf`**

```hcl
variable "project_name" {
  type        = string
  description = "Name prefix applied to all resources."
  default     = "nebari-example"
}

variable "location" {
  type        = string
  description = "Azure region."
  default     = "eastus"
}
```

- [ ] **Step 4: Write `examples/complete/main.tf`**

```hcl
module "aks_cluster" {
  source = "../.."

  project_name = var.project_name
  location     = var.location

  kubernetes_version      = "1.34"
  sku_tier                = "Free"
  private_cluster_enabled = false

  vnet_cidr_block        = "10.10.0.0/16"
  node_subnet_cidr_block = "10.10.0.0/22"
  pod_cidr               = "10.244.0.0/16"
  service_cidr           = "10.10.16.0/22"
  dns_service_ip         = "10.10.16.10"

  node_groups = {
    system = {
      vm_size   = "Standard_D2_v3"
      min_count = 1
      max_count = 3
      mode      = "System"
    }
    user = {
      vm_size   = "Standard_D4_v3"
      min_count = 1
      max_count = 5
    }
    worker = {
      vm_size   = "Standard_D4_v3"
      min_count = 0
      max_count = 5
    }
  }

  tags = {
    Environment = "development"
    Project     = "nebari"
  }
}
```

- [ ] **Step 5: Write `examples/complete/outputs.tf`**

```hcl
output "cluster_name" {
  value = module.aks_cluster.cluster_name
}

output "resource_group_name" {
  value = module.aks_cluster.resource_group_name
}

output "kubeconfig_command" {
  value = module.aks_cluster.kubeconfig_command
}

# Re-exported so Terratest can read it via terraform.Output(). Sensitive
# because the module marks it sensitive.
output "kube_admin_config_raw" {
  value     = module.aks_cluster.kube_admin_config_raw
  sensitive = true
}
```

- [ ] **Step 6: Write `examples/complete/README.md`**

```markdown
# Complete example

Provisions an AKS cluster with the module's full default feature set: VNet, system + user + worker node pools, Azure CNI Overlay networking, public API endpoint.

## Usage

\`\`\`bash
export ARM_SUBSCRIPTION_ID=<your-sub-id>
tofu init
tofu apply
\`\`\`

After apply, fetch a kubeconfig with the command in the `kubeconfig_command` output.
```

- [ ] **Step 7: Validate**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/examples/complete
tofu init -backend=false
tofu validate
cd ~/gh/terraform-azurerm-aks-cluster
```

Expected: `Success!`

- [ ] **Step 8: Commit**

```bash
git add examples/complete/
git commit -m "docs: add examples/complete reference deployment"
```

---

### Task 18: Write `examples/existing-resources/`

**Files:**
- Create: `examples/existing-resources/main.tf`
- Create: `examples/existing-resources/providers.tf`
- Create: `examples/existing-resources/versions.tf`
- Create: `examples/existing-resources/variables.tf`
- Create: `examples/existing-resources/outputs.tf`
- Create: `examples/existing-resources/README.md`

- [ ] **Step 1: Write `examples/existing-resources/versions.tf`**

Identical to `examples/complete/versions.tf` — copy it:

```bash
cp examples/complete/versions.tf examples/existing-resources/versions.tf
```

- [ ] **Step 2: Write `examples/existing-resources/providers.tf`**

Identical to `examples/complete/providers.tf` — copy it:

```bash
cp examples/complete/providers.tf examples/existing-resources/providers.tf
```

- [ ] **Step 3: Write `examples/existing-resources/variables.tf`**

```hcl
variable "project_name" {
  type    = string
  default = "nebari-byo-example"
}

variable "location" {
  type    = string
  default = "eastus"
}

variable "existing_resource_group_name" {
  type        = string
  description = "Name of an existing resource group."
}

variable "existing_vnet_id" {
  type        = string
  description = "Full resource ID of an existing VNet."
}

variable "existing_node_subnet_id" {
  type        = string
  description = "Full resource ID of an existing subnet for AKS nodes."
}
```

- [ ] **Step 4: Write `examples/existing-resources/main.tf`**

```hcl
module "aks_cluster" {
  source = "../.."

  project_name = var.project_name
  location     = var.location

  create_resource_group        = false
  existing_resource_group_name = var.existing_resource_group_name

  create_vnet             = false
  existing_vnet_id        = var.existing_vnet_id
  existing_node_subnet_id = var.existing_node_subnet_id

  node_groups = {
    system = {
      vm_size   = "Standard_D2_v3"
      min_count = 1
      max_count = 3
      mode      = "System"
    }
  }

  tags = {
    Environment = "development"
    Project     = "nebari"
  }
}
```

- [ ] **Step 5: Write `examples/existing-resources/outputs.tf`**

```hcl
output "cluster_name" {
  value = module.aks_cluster.cluster_name
}

output "kubeconfig_command" {
  value = module.aks_cluster.kubeconfig_command
}
```

- [ ] **Step 6: Write `examples/existing-resources/README.md`**

```markdown
# Existing-resources example

Provisions an AKS cluster reusing an existing resource group and VNet/subnet. Useful for enterprise scenarios with pre-existing infrastructure governance.

## Usage

\`\`\`bash
export ARM_SUBSCRIPTION_ID=<your-sub-id>
tofu init
tofu apply \\
  -var existing_resource_group_name=my-rg \\
  -var existing_vnet_id=/subscriptions/.../virtualNetworks/my-vnet \\
  -var existing_node_subnet_id=/subscriptions/.../subnets/my-nodes
\`\`\`
```

- [ ] **Step 7: Validate**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/examples/existing-resources
tofu init -backend=false
tofu validate
cd ~/gh/terraform-azurerm-aks-cluster
```

Expected: `Success!`

- [ ] **Step 8: Commit**

```bash
git add examples/existing-resources/
git commit -m "docs: add examples/existing-resources reference deployment"
```

---

## Phase A2 (continued) — Auto-generated docs (Task 19)

### Task 19: Regenerate README via terraform-docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Run terraform-docs**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
make docs
```

Expected: `README.md` is updated between `<!-- BEGIN_TF_DOCS -->` and `<!-- END_TF_DOCS -->` markers with Inputs, Outputs, Resources, etc.

Inspect the result:

```bash
cat README.md
```

If any section looks wrong (missing variable descriptions, etc.), the fix is in the source `.tf` files — not in `README.md`. Update there and re-run `make docs`.

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: regenerate README via terraform-docs"
```

- [ ] **Step 3: Push and verify CI still green**

```bash
git push
gh run watch
```

Expected: all CI jobs pass.

---

## Phase A3 — Tests + release (Tasks 20–26)

### Task 20: Terratest scaffold

**Files:**
- Create: `test/go.mod`
- Create: `test/module_test.go`
- Create: `test/fixtures/disk-csi/storageclass.yaml`
- Create: `test/fixtures/disk-csi/pvc.yaml`
- Create: `test/fixtures/disk-csi/pod.yaml`

- [ ] **Step 1: Init Go module**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/test
go mod init github.com/nebari-dev/terraform-azurerm-aks-cluster/test
```

- [ ] **Step 2: Write `test/module_test.go` (entry point — no real assertions yet)**

```go
package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

const fixtureDir = "../examples/complete"

// TestAKSClusterComplete provisions the examples/complete fixture,
// verifies the cluster is reachable, and exercises the managed-disk CSI driver.
func TestAKSClusterComplete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	projectName := fmt.Sprintf("nebari-test-%s", random.UniqueId())

	terraformOptions := &terraform.Options{
		TerraformDir: fixtureDir,
		Vars: map[string]interface{}{
			"project_name": projectName,
			"location":     getEnvOrDefault("AZURE_TEST_LOCATION", "eastus"),
		},
		EnvVars: map[string]string{
			"ARM_SUBSCRIPTION_ID": os.Getenv("ARM_SUBSCRIPTION_ID"),
		},
		// Write an override file that forces test-friendly settings.
		Reconfigure: true,
	}

	// Override file to keep the cluster small for tests.
	overridePath := filepath.Join(fixtureDir, "test_override.tf.json")
	if err := writeTestOverride(overridePath); err != nil {
		t.Fatalf("failed to write test override: %v", err)
	}
	defer os.Remove(overridePath)

	defer terraform.Destroy(t, terraformOptions)
	terraform.InitAndApply(t, terraformOptions)

	clusterName := terraform.Output(t, terraformOptions, "cluster_name")
	if clusterName == "" {
		t.Fatal("cluster_name output is empty")
	}
	t.Logf("cluster_name=%s", clusterName)

	// TODO: test managed-disk CSI in next task.
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func writeTestOverride(path string) error {
	// Force smallest viable VM size and a single-node system pool for cost control.
	content := `{
  "module": {
    "aks_cluster": {
      "node_groups": {
        "system": {
          "vm_size":   "Standard_B2s",
          "min_count": 1,
          "max_count": 1,
          "mode":      "System"
        }
      }
    }
  }
}`
	return os.WriteFile(path, []byte(content), 0644)
}
```

- [ ] **Step 3: Write `test/fixtures/disk-csi/storageclass.yaml`**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: test-managed-csi
provisioner: disk.csi.azure.com
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
parameters:
  skuName: Standard_LRS
```

- [ ] **Step 4: Write `test/fixtures/disk-csi/pvc.yaml`**

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: test-pvc
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: test-managed-csi
  resources:
    requests:
      storage: 1Gi
```

- [ ] **Step 5: Write `test/fixtures/disk-csi/pod.yaml`**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
    - name: app
      image: busybox:1.36
      command: ["sleep", "3600"]
      volumeMounts:
        - mountPath: /data
          name: vol
  volumes:
    - name: vol
      persistentVolumeClaim:
        claimName: test-pvc
```

- [ ] **Step 6: Add Terratest dependency**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/test
go get github.com/gruntwork-io/terratest/modules/terraform@latest
go get github.com/gruntwork-io/terratest/modules/random@latest
go mod tidy
```

- [ ] **Step 7: Verify compile**

```bash
go vet ./...
go test -run TestAKSClusterComplete -count=0 ./...
```

Expected: both succeed (no test actually runs because `-count=0` means "compile only").

- [ ] **Step 8: Commit**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
git add test/
git commit -m "test: add Terratest scaffold and disk-csi fixtures"
```

---

### Task 21: Implement disk-CSI verification step

**Files:**
- Modify: `test/module_test.go`

- [ ] **Step 1: Add Kubernetes client dependencies**

```bash
cd ~/gh/terraform-azurerm-aks-cluster/test
go get github.com/gruntwork-io/terratest/modules/k8s@latest
go mod tidy
```

- [ ] **Step 2: Replace `// TODO: test managed-disk CSI` with the verification logic**

Replace `module_test.go` with:

```go
package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/random"
	"github.com/gruntwork-io/terratest/modules/terraform"
)

const fixtureDir = "../examples/complete"

func TestAKSClusterComplete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	projectName := fmt.Sprintf("nebari-test-%s", random.UniqueId())

	terraformOptions := &terraform.Options{
		TerraformDir: fixtureDir,
		Vars: map[string]interface{}{
			"project_name": projectName,
			"location":     getEnvOrDefault("AZURE_TEST_LOCATION", "eastus"),
		},
		EnvVars: map[string]string{
			"ARM_SUBSCRIPTION_ID": os.Getenv("ARM_SUBSCRIPTION_ID"),
		},
		Reconfigure: true,
	}

	overridePath := filepath.Join(fixtureDir, "test_override.tf.json")
	if err := writeTestOverride(overridePath); err != nil {
		t.Fatalf("failed to write test override: %v", err)
	}
	defer os.Remove(overridePath)

	defer terraform.Destroy(t, terraformOptions)
	terraform.InitAndApply(t, terraformOptions)

	clusterName := terraform.Output(t, terraformOptions, "cluster_name")
	if clusterName == "" {
		t.Fatal("cluster_name output is empty")
	}
	t.Logf("cluster_name=%s", clusterName)

	kubeconfigPath := writeKubeconfig(t, terraformOptions)
	defer os.Remove(kubeconfigPath)

	testManagedDiskCSI(t, kubeconfigPath)
}

func writeKubeconfig(t *testing.T, opts *terraform.Options) string {
	kubeconfig := terraform.Output(t, opts, "kube_admin_config_raw")
	// kube_admin_config_raw is sensitive; OutputRaw avoids logging it.
	if kubeconfig == "" {
		t.Fatal("kube_admin_config_raw is empty")
	}
	f, err := os.CreateTemp("", "nebari-test-kubeconfig-*.yaml")
	if err != nil {
		t.Fatalf("create temp kubeconfig: %v", err)
	}
	if _, err := f.WriteString(kubeconfig); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	f.Close()
	return f.Name()
}

func testManagedDiskCSI(t *testing.T, kubeconfigPath string) {
	kubectl := k8s.NewKubectlOptions("", kubeconfigPath, "default")

	k8s.KubectlApply(t, kubectl, "fixtures/disk-csi/storageclass.yaml")
	defer k8s.KubectlDelete(t, kubectl, "fixtures/disk-csi/storageclass.yaml")

	k8s.KubectlApply(t, kubectl, "fixtures/disk-csi/pvc.yaml")
	defer k8s.KubectlDelete(t, kubectl, "fixtures/disk-csi/pvc.yaml")

	k8s.KubectlApply(t, kubectl, "fixtures/disk-csi/pod.yaml")
	defer k8s.KubectlDelete(t, kubectl, "fixtures/disk-csi/pod.yaml")

	k8s.WaitUntilPodAvailable(t, kubectl, "test-pod", 30, 10*time.Second)
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func writeTestOverride(path string) error {
	content := `{
  "module": {
    "aks_cluster": {
      "node_groups": {
        "system": {
          "vm_size":   "Standard_B2s",
          "min_count": 1,
          "max_count": 1,
          "mode":      "System"
        }
      }
    }
  }
}`
	return os.WriteFile(path, []byte(content), 0644)
}
```

- [ ] **Step 3: Verify compile**

```bash
go vet ./...
go test -run TestAKSClusterComplete -count=0 ./...
```

Expected: both succeed.

- [ ] **Step 4: Commit**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
git add test/module_test.go test/go.mod test/go.sum
git commit -m "test: verify managed-disk CSI driver works after deploy"
```

---

### Task 22: Add Terratest CI workflow with Azure OIDC

**Files:**
- Create: `.github/workflows/test.yml`

- [ ] **Step 1: Write `.github/workflows/test.yml`**

```yaml
name: Test

on:
  workflow_dispatch:
  pull_request:
    types: [ready_for_review]
  push:
    branches: [main]

permissions:
  id-token: write   # required for Azure OIDC
  contents: read

jobs:
  terratest:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    steps:
      - uses: actions/checkout@v4

      - name: Azure login (OIDC)
        uses: azure/login@v2
        with:
          client-id: ${{ secrets.AZURE_CLIENT_ID }}
          tenant-id: ${{ secrets.AZURE_TENANT_ID }}
          subscription-id: ${{ secrets.AZURE_SUBSCRIPTION_ID }}

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1
        with:
          tofu_version: 1.11.2

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run Terratest
        working-directory: test
        env:
          ARM_SUBSCRIPTION_ID: ${{ secrets.AZURE_SUBSCRIPTION_ID }}
          AZURE_TEST_LOCATION: eastus
        run: |
          go mod tidy
          go test -v -timeout 60m ./...
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "ci: add Terratest workflow with Azure OIDC"
```

- [ ] **Step 3: Document required GitHub secrets**

Edit `README.md` to add (before `<!-- BEGIN_TF_DOCS -->`):

```markdown
## CI/Testing

Terratest runs against a real Azure subscription. The repo needs these GitHub secrets configured:

- `AZURE_CLIENT_ID` — App registration client ID with federated OIDC credentials trusting this repo.
- `AZURE_TENANT_ID` — Azure AD tenant ID.
- `AZURE_SUBSCRIPTION_ID` — Subscription where test resources are created.

The app registration must have `Contributor` on the subscription (or on a scoped test resource group prefix).
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document CI Azure OIDC secret requirements"
```

---

### Task 23: Configure GitHub repo secrets and trigger Terratest run

This is a one-time setup step performed by the repo admin.

- [ ] **Step 1: Create an Azure AD app registration with OIDC federation**

Follow [Azure docs for GitHub Actions OIDC](https://learn.microsoft.com/en-us/azure/developer/github/connect-from-azure?tabs=azure-cli%2Clinux). Outline:

```bash
# Create the app registration
az ad app create --display-name "github-actions-terraform-azurerm-aks-cluster"
APP_ID=$(az ad app list --display-name "github-actions-terraform-azurerm-aks-cluster" --query '[0].appId' -o tsv)

# Create the service principal
az ad sp create --id "$APP_ID"
SP_OBJECT_ID=$(az ad sp show --id "$APP_ID" --query 'id' -o tsv)

# Grant Contributor on the subscription
SUB_ID=$(az account show --query id -o tsv)
az role assignment create --assignee-object-id "$SP_OBJECT_ID" --role Contributor --scope "/subscriptions/$SUB_ID" --assignee-principal-type ServicePrincipal

# Federate to the GitHub repo
cat > /tmp/federation.json <<EOF
{
  "name": "main",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:nebari-dev/terraform-azurerm-aks-cluster:ref:refs/heads/main",
  "audiences": ["api://AzureADTokenExchange"]
}
EOF
az ad app federated-credential create --id "$APP_ID" --parameters @/tmp/federation.json

# Repeat for pull_request:
cat > /tmp/federation-pr.json <<EOF
{
  "name": "pr",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:nebari-dev/terraform-azurerm-aks-cluster:pull_request",
  "audiences": ["api://AzureADTokenExchange"]
}
EOF
az ad app federated-credential create --id "$APP_ID" --parameters @/tmp/federation-pr.json

echo "AZURE_CLIENT_ID=$APP_ID"
echo "AZURE_TENANT_ID=$(az account show --query tenantId -o tsv)"
echo "AZURE_SUBSCRIPTION_ID=$SUB_ID"
```

- [ ] **Step 2: Set the GitHub secrets**

```bash
gh secret set AZURE_CLIENT_ID --repo nebari-dev/terraform-azurerm-aks-cluster --body "$APP_ID"
gh secret set AZURE_TENANT_ID --repo nebari-dev/terraform-azurerm-aks-cluster --body "$(az account show --query tenantId -o tsv)"
gh secret set AZURE_SUBSCRIPTION_ID --repo nebari-dev/terraform-azurerm-aks-cluster --body "$SUB_ID"
```

- [ ] **Step 3: Trigger the Terratest workflow manually**

```bash
gh workflow run "Test" --repo nebari-dev/terraform-azurerm-aks-cluster
gh run watch --repo nebari-dev/terraform-azurerm-aks-cluster
```

Expected: workflow completes successfully in 15–25 minutes; the disk-CSI pod becomes Ready before the destroy phase.

If it fails, inspect logs (`gh run view --log-failed`). Most common failures:
- Quota exhaustion on `Standard_B2s` in the chosen region → switch `AZURE_TEST_LOCATION` to a region with capacity.
- Subscription doesn't have the AKS Resource Provider registered → `az provider register --namespace Microsoft.ContainerService` (one-time, can take ~5 min).

- [ ] **Step 4: No commit needed** — secrets and workflow runs are GitHub-side state.

---

### Task 24: Final lint/docs sweep before tagging

- [ ] **Step 1: Run all checks locally**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
make fmt
make validate
make lint
make docs
```

Expected: all targets pass. If `make docs` produces a README diff, commit it.

- [ ] **Step 2: Commit any pending docs updates**

```bash
git status
# If README.md changed:
git add README.md
git commit -m "docs: refresh terraform-docs output"
```

- [ ] **Step 3: Push and verify CI green on `main`**

```bash
git push
gh run watch
```

Expected: all CI jobs pass.

---

### Task 25: Tag `v0.1.0` and verify Registry publishes

- [ ] **Step 1: Tag the current `main`**

```bash
cd ~/gh/terraform-azurerm-aks-cluster
git tag -a v0.1.0 -m "v0.1.0: initial release"
git push origin v0.1.0
```

- [ ] **Step 2: Connect the repo to Terraform Registry**

Visit https://registry.terraform.io/ and:

1. Sign in with GitHub.
2. Click "Publish" → "Module".
3. Select `nebari-dev/terraform-azurerm-aks-cluster`.
4. Confirm publish.

Registry namespace will be `nebari-dev/aks-cluster/azurerm`. The Registry detects existing tags and publishes `v0.1.0` automatically.

- [ ] **Step 3: Verify Registry listing**

Visit https://registry.terraform.io/modules/nebari-dev/aks-cluster/azurerm/latest

Expected: the page renders with the README, inputs/outputs from terraform-docs, and `v0.1.0` listed.

- [ ] **Step 4: Smoke-test from a fresh dir using Registry source**

```bash
mkdir /tmp/aks-registry-smoke
cd /tmp/aks-registry-smoke
cat > main.tf <<EOF
terraform {
  required_version = ">= 1.9"
  required_providers {
    azurerm = { source = "hashicorp/azurerm", version = ">= 4.0" }
  }
}
provider "azurerm" { features {} }

module "aks_cluster" {
  source  = "nebari-dev/aks-cluster/azurerm"
  version = "0.1.0"

  project_name = "nebari-smoke"
  location     = "eastus"
  node_groups = {
    system = { vm_size = "Standard_D2_v3", min_count = 1, max_count = 1, mode = "System" }
  }
}
EOF
tofu init
tofu validate
```

Expected: `tofu init` downloads the module from the Registry; `tofu validate` succeeds.

```bash
cd ~/gh/terraform-azurerm-aks-cluster
rm -rf /tmp/aks-registry-smoke
```

---

### Task 26: Write release notes

- [ ] **Step 1: Create a GitHub release**

```bash
gh release create v0.1.0 \
  --repo nebari-dev/terraform-azurerm-aks-cluster \
  --title "v0.1.0 — initial release" \
  --notes "$(cat <<'EOF'
Initial release of `terraform-azurerm-aks-cluster`.

## Features
- AKS cluster with system + user node pools
- Azure CNI Overlay networking by default
- Optional BYO resource group and VNet
- User-assigned kubelet identity
- Sensible defaults (Standard_D2_v3 system pool, public API endpoint, SKU=Free)

## Companion module
This module is the Azure counterpart of [terraform-aws-eks-cluster](https://github.com/nebari-dev/terraform-aws-eks-cluster). It is designed to be consumed by [nebari-infrastructure-core](https://github.com/nebari-dev/nebari-infrastructure-core) via its `azure` provider (which lands in a parallel work-stream).

## Known gaps
The following deliberately out-of-scope; will be addressed in subsequent releases:
- Azure Files / `azurefile-csi` shared storage
- Workload Identity beyond the kubelet identity
- Application Gateway Ingress Controller
- Key Vault encryption-at-rest
- Log Analytics Workspace integration
EOF
)"
```

- [ ] **Step 2: No further commit** — release notes are GitHub-side state.

---

## Acceptance checklist

Before considering the plan complete, verify:

- [ ] Repo `github.com/nebari-dev/terraform-azurerm-aks-cluster` exists, public, on `main`.
- [ ] `tofu init -backend=false && tofu validate` passes from the repo root.
- [ ] `make fmt`, `make validate`, `make lint`, `make docs` all pass locally.
- [ ] CI workflows `ci.yml` and `test.yml` both green on `main`.
- [ ] Terratest run (manual `workflow_dispatch`) completed successfully; disk-CSI pod reached `Ready`.
- [ ] Tag `v0.1.0` pushed.
- [ ] Registry listing live at `nebari-dev/aks-cluster/azurerm`, v0.1.0 visible.
- [ ] A smoke-test consumer pinning `source = "nebari-dev/aks-cluster/azurerm"` + `version = "0.1.0"` can `tofu init && tofu validate`.

Once all boxes are checked, the Track B plan (`2026-05-21-nic-azure-provider.md`, when written) can begin flipping NIC's shim `source` to the Registry version.
