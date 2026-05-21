# NIC Azure Provider Implementation Plan (Track B)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the stub at `pkg/provider/azure/` in `nebari-infrastructure-core` with a fully functional implementation that consumes the `terraform-azurerm-aks-cluster` module (Track A) and exposes `nic deploy --config azure-config.yaml` end-to-end. Mirrors the structure and conventions of `pkg/provider/aws/`.

**Architecture:** The Azure provider parses YAML config → builds a `TFVars` struct → writes `terraform.tfvars.json` next to an embedded OpenTofu shim → invokes `pkg/tofu` to plan/apply against the Track A module. Kubeconfig is fetched via the Azure SDK (`armcontainerservice`), not from Terraform state, so it works even when state is gone. Tag-based discovery (`nic.nebari.dev/cluster-name`, `nic.nebari.dev/managed-by`) handles orphan cleanup after destroy. No NIC-side abstraction changes — the `Provider` interface, registry, and config dispatch already accommodate a third provider.

**Tech Stack:** Go 1.25+, `azidentity` (DefaultAzureCredential chain), `armcontainerservice/v6`, `armresources`, `armsubscription`, OpenTofu via `pkg/tofu`, OpenTelemetry tracing/status.

**Reference spec:** `docs/superpowers/specs/2026-05-21-azure-aks-terraform-module-and-nic-provider-design.md`.

**Reference patterns:** `pkg/provider/aws/` — read `config.go`, `tofu.go`, `provider.go`, `kubeconfig.go`, `state.go`, `cleanup.go`, `templates/*`, `interfaces.go` for the conventions to mirror.

---

## Prerequisites (before Task 1)

The engineer should confirm before starting:

- Working tree: `~/gh/nebari-infrastructure-core` on a feature branch off `main`. The spec and Track A plan already live on `tpotts/azure-aks-design-spec`; you may either continue on that branch or create a fresh one off it (e.g., `tpotts/azure-provider-impl`).
- `make check` passes on the branch (formats, vets, lints, tests cleanly).
- An Azure subscription accessible via `az login` (required for B3 and C2 only).
- The Track A module repo exists at `github.com/nebari-dev/terraform-azurerm-aks-cluster` with at least `main` branch reachable. Full Track A v0.1.0 release is required for **Phase C1** (Registry pin flip). Phases B1–B3 can run against `?ref=main` of the in-progress Track A repo.

`cd` into the repo for all tasks:

```bash
cd ~/gh/nebari-infrastructure-core
git status                  # must be clean before starting
```

---

## File Structure

Final layout of `pkg/provider/azure/` (mirrors `pkg/provider/aws/`):

| Path | Responsibility |
|---|---|
| `config.go` | Full `Config`, `NetworkConfig`, `NodeGroup` structs, YAML tags, `Validate()` method |
| `config_test.go` | Table-driven validation tests, no cloud calls |
| `provider.go` | `Provider` struct, `NewProvider()`, full interface impl |
| `provider_test.go` | Top-level contract tests |
| `tofu.go` | `TFVars` struct, `embed.FS` for `templates/`, `(*Config).toTFVars()` |
| `tofu_test.go` | `toTFVars()` correctness tests |
| `kubeconfig.go` | Pull kubeconfig via `armcontainerservice.ManagedClustersClient.ListClusterAdminCredentials` |
| `kubeconfig_test.go` | Mock-backed test |
| `state.go` | Tag-based resource discovery for cleanup |
| `state_test.go` | Tag-filter query construction, classification tests |
| `cleanup.go` | Orphan-resource cleanup (MC_* RG, leaked LBs/disks) |
| `cleanup_test.go` | Ordering + idempotency tests |
| `version.go` | AKS supported-versions helper |
| `version_test.go` | Negotiation tests |
| `interfaces.go` | Mock-friendly SDK client interfaces |
| `templates/main.tf` | OpenTofu shim — `module "aks_cluster"` calling Track A |
| `templates/variables.tf` | Re-declares all module inputs |
| `templates/outputs.tf` | Re-exposes module outputs |
| `templates/provider.tf` | `provider "azurerm" { features {} }` |
| `templates/backend.tf` | `backend "local" {}` (matches AWS for MVP) |

Other files touched:

| Path | Responsibility |
|---|---|
| `go.mod` / `go.sum` | Add `azidentity`, `armcontainerservice/v6`, `armresources`, `armsubscription` |
| `examples/azure-config.yaml` | Final schema reflecting `Config` |
| `ARCHITECTURE.md`, `WALKTHROUGH.md`, `CLAUDE.md` | Drop "Azure stub" language (Phase C2) |

---

## Phase B1 — Scaffolding + Config (Tasks 1–10)

### Task 1: Create feature branch and verify clean baseline

- [ ] **Step 1: Create branch**

```bash
cd ~/gh/nebari-infrastructure-core
git checkout tpotts/azure-aks-design-spec   # already exists with spec + plans
git pull --ff-only origin main || true        # incorporate latest main if behind
git checkout -b tpotts/azure-provider-impl
```

- [ ] **Step 2: Verify baseline checks pass**

```bash
make check
```

Expected: format, vet, lint, and tests all pass. If anything fails, fix or stash before proceeding.

- [ ] **Step 3: No commit needed** — branch creation only. Start implementation in Task 2.

---

### Task 2: Expand `pkg/provider/azure/config.go` with full struct (no `Validate()` yet)

**Files:**
- Modify: `pkg/provider/azure/config.go`

- [ ] **Step 1: Read the existing stub for context**

```bash
cat pkg/provider/azure/config.go
```

The stub has a `Config` struct with a few fields. You're replacing it.

- [ ] **Step 2: Replace `pkg/provider/azure/config.go` with the full struct**

```go
package azure

// Config is the user-facing Azure cluster configuration as parsed from the
// `cluster.azure:` block of NIC YAML.
type Config struct {
	Region                string               `yaml:"region"`
	ResourceGroupName     string               `yaml:"resource_group_name,omitempty"`
	CreateResourceGroup   *bool                `yaml:"create_resource_group,omitempty"`
	KubernetesVersion     string               `yaml:"kubernetes_version,omitempty"`
	SKUTier               string               `yaml:"sku_tier,omitempty"`
	PrivateClusterEnabled bool                 `yaml:"private_cluster_enabled,omitempty"`
	AuthorizedIPRanges    []string             `yaml:"authorized_ip_ranges,omitempty"`
	Network               *NetworkConfig       `yaml:"network,omitempty"`
	NodeGroups            map[string]NodeGroup `yaml:"node_groups"`
	Tags                  map[string]string    `yaml:"tags,omitempty"`
}

// NetworkConfig groups all VNet/subnet/CIDR knobs.
type NetworkConfig struct {
	VNetCIDRBlock        string `yaml:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock  string `yaml:"node_subnet_cidr_block,omitempty"`
	PodCIDR              string `yaml:"pod_cidr,omitempty"`
	ServiceCIDR          string `yaml:"service_cidr,omitempty"`
	DNSServiceIP         string `yaml:"dns_service_ip,omitempty"`
	ExistingVNetID       string `yaml:"existing_vnet_id,omitempty"`
	ExistingNodeSubnetID string `yaml:"existing_node_subnet_id,omitempty"`
}

// NodeGroup describes one AKS node pool.
type NodeGroup struct {
	Instance     string            `yaml:"instance"`
	MinNodes     int               `yaml:"min_nodes"`
	MaxNodes     int               `yaml:"max_nodes"`
	Mode         string            `yaml:"mode,omitempty"` // "System" | "User"; defaults to "User"
	OSDiskSizeGB int               `yaml:"os_disk_size_gb,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty"`
	Taints       []string          `yaml:"taints,omitempty"`
	Zones        []string          `yaml:"zones,omitempty"`
}
```

- [ ] **Step 3: Run formatters and vet**

```bash
make fmt
make vet
```

Expected: no diffs after fmt; vet clean.

- [ ] **Step 4: Run existing Azure tests (the stub `provider_test.go` will likely break — that's expected)**

```bash
go test ./pkg/provider/azure/...
```

If existing tests fail because they referenced removed fields, that's expected — the next task replaces those tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/provider/azure/config.go
git commit -m "feat(azure): expand Config struct with full schema"
```

---

### Task 3: Add `(*Config).Validate()` with tests (TDD)

**Files:**
- Create: `pkg/provider/azure/config_test.go` (or replace existing)
- Modify: `pkg/provider/azure/config.go`

- [ ] **Step 1: Write the failing tests first**

Replace `pkg/provider/azure/config_test.go` with:

```go
package azure

import (
	"strings"
	"testing"
)

func ptrBool(b bool) *bool { return &b }

func TestConfigValidate(t *testing.T) {
	validNodeGroup := NodeGroup{Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 3, Mode: "System"}

	cases := []struct {
		name      string
		cfg       Config
		wantErr   bool
		wantInErr string
	}{
		{
			name: "minimal valid",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
			},
			wantErr: false,
		},
		{
			name:      "missing region",
			cfg:       Config{NodeGroups: map[string]NodeGroup{"system": validNodeGroup}},
			wantErr:   true,
			wantInErr: "region",
		},
		{
			name:      "no node groups",
			cfg:       Config{Region: "eastus"},
			wantErr:   true,
			wantInErr: "node_groups",
		},
		{
			name: "two system pools",
			cfg: Config{
				Region: "eastus",
				NodeGroups: map[string]NodeGroup{
					"a": {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: "System"},
					"b": {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: "System"},
				},
			},
			wantErr:   true,
			wantInErr: "System",
		},
		{
			name: "BYO vnet missing subnet",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{ExistingVNetID: "/subscriptions/.../vn1"},
			},
			wantErr:   true,
			wantInErr: "existing_node_subnet_id",
		},
		{
			name: "BYO subnet missing vnet",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{ExistingNodeSubnetID: "/subscriptions/.../sub1"},
			},
			wantErr:   true,
			wantInErr: "existing_vnet_id",
		},
		{
			name: "bad CIDR",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{VNetCIDRBlock: "not-a-cidr"},
			},
			wantErr:   true,
			wantInErr: "vnet_cidr_block",
		},
		{
			name: "create_resource_group false without existing name",
			cfg: Config{
				Region:              "eastus",
				NodeGroups:          map[string]NodeGroup{"system": validNodeGroup},
				CreateResourceGroup: ptrBool(false),
			},
			wantErr:   true,
			wantInErr: "resource_group_name",
		},
		{
			name: "bad kubernetes version",
			cfg: Config{
				Region:            "eastus",
				NodeGroups:        map[string]NodeGroup{"system": validNodeGroup},
				KubernetesVersion: "latest",
			},
			wantErr:   true,
			wantInErr: "kubernetes_version",
		},
		{
			name: "valid kubernetes version",
			cfg: Config{
				Region:            "eastus",
				NodeGroups:        map[string]NodeGroup{"system": validNodeGroup},
				KubernetesVersion: "1.34",
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantInErr)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if tc.wantErr && tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("error %q does not contain expected substring %q", err.Error(), tc.wantInErr)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```bash
go test ./pkg/provider/azure/ -run TestConfigValidate -v
```

Expected: compile error or failure (`Validate` doesn't exist yet on `Config`).

- [ ] **Step 3: Implement `Validate()` in `pkg/provider/azure/config.go`**

Append to `config.go`:

```go
import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var kubernetesVersionRE = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

// Validate checks that the Config is internally consistent and that all
// references between fields are coherent. It does NOT make any cloud calls.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Region) == "" {
		return fmt.Errorf("cluster.azure.region is required")
	}

	if len(c.NodeGroups) == 0 {
		return fmt.Errorf("cluster.azure.node_groups must contain at least one entry")
	}

	systemCount := 0
	for _, ng := range c.NodeGroups {
		if ng.Mode == "System" {
			systemCount++
		}
	}
	if systemCount > 1 {
		return fmt.Errorf("at most one node group may have mode=\"System\" (got %d)", systemCount)
	}

	if c.CreateResourceGroup != nil && !*c.CreateResourceGroup && strings.TrimSpace(c.ResourceGroupName) == "" {
		return fmt.Errorf("cluster.azure.resource_group_name is required when create_resource_group=false")
	}

	if c.KubernetesVersion != "" && !kubernetesVersionRE.MatchString(c.KubernetesVersion) {
		return fmt.Errorf("cluster.azure.kubernetes_version %q is not a valid semver-ish version (expected e.g. \"1.34\" or \"1.34.0\")", c.KubernetesVersion)
	}

	if c.Network != nil {
		if err := c.Network.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (n *NetworkConfig) validate() error {
	// BYO networking: both ID fields must be set together.
	if (n.ExistingVNetID != "") != (n.ExistingNodeSubnetID != "") {
		if n.ExistingVNetID == "" {
			return fmt.Errorf("cluster.azure.network.existing_vnet_id is required when existing_node_subnet_id is set")
		}
		return fmt.Errorf("cluster.azure.network.existing_node_subnet_id is required when existing_vnet_id is set")
	}

	for label, cidr := range map[string]string{
		"vnet_cidr_block":        n.VNetCIDRBlock,
		"node_subnet_cidr_block": n.NodeSubnetCIDRBlock,
		"pod_cidr":               n.PodCIDR,
		"service_cidr":           n.ServiceCIDR,
	} {
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("cluster.azure.network.%s: %w", label, err)
		}
	}

	if n.DNSServiceIP != "" && net.ParseIP(n.DNSServiceIP) == nil {
		return fmt.Errorf("cluster.azure.network.dns_service_ip: %q is not a valid IP address", n.DNSServiceIP)
	}

	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/provider/azure/ -run TestConfigValidate -v
```

Expected: all subtests PASS.

- [ ] **Step 5: Lint**

```bash
make lint
```

Expected: no new issues.

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/config.go pkg/provider/azure/config_test.go
git commit -m "feat(azure): add Config.Validate() with table-driven tests"
```

---

### Task 4: Create `pkg/provider/azure/templates/main.tf` (shim, git-ref source)

**Files:**
- Create: `pkg/provider/azure/templates/main.tf`

The shim points at Track A's repo via git ref during dev. Phase C1 flips this to the Registry-versioned source after Track A's v0.1.0 is published.

- [ ] **Step 1: Write `pkg/provider/azure/templates/main.tf`**

```hcl
module "aks_cluster" {
  source = "git::https://github.com/nebari-dev/terraform-azurerm-aks-cluster.git?ref=main"

  project_name                 = var.project_name
  location                     = var.location
  tags                         = var.tags
  create_resource_group        = var.create_resource_group
  existing_resource_group_name = var.existing_resource_group_name
  create_vnet                  = var.create_vnet
  vnet_cidr_block              = var.vnet_cidr_block
  node_subnet_cidr_block       = var.node_subnet_cidr_block
  existing_vnet_id             = var.existing_vnet_id
  existing_node_subnet_id      = var.existing_node_subnet_id
  network_plugin               = var.network_plugin
  network_plugin_mode          = var.network_plugin_mode
  pod_cidr                     = var.pod_cidr
  service_cidr                 = var.service_cidr
  dns_service_ip               = var.dns_service_ip
  kubernetes_version           = var.kubernetes_version
  private_cluster_enabled      = var.private_cluster_enabled
  authorized_ip_ranges         = var.authorized_ip_ranges
  sku_tier                     = var.sku_tier
  identity_type                = var.identity_type
  node_groups                  = var.node_groups
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/provider/azure/templates/main.tf
git commit -m "feat(azure): add shim main.tf wired to Track A module via git ref"
```

---

### Task 5: Create `templates/variables.tf` (re-declares module inputs)

**Files:**
- Create: `pkg/provider/azure/templates/variables.tf`

- [ ] **Step 1: Write `pkg/provider/azure/templates/variables.tf`**

```hcl
variable "project_name" {
  type = string
}

variable "location" {
  type = string
}

variable "tags" {
  type    = map(string)
  default = {}
}

variable "create_resource_group" {
  type    = bool
  default = true
}

variable "existing_resource_group_name" {
  type    = string
  default = null
}

variable "create_vnet" {
  type    = bool
  default = true
}

variable "vnet_cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

variable "node_subnet_cidr_block" {
  type    = string
  default = "10.0.0.0/22"
}

variable "existing_vnet_id" {
  type    = string
  default = null
}

variable "existing_node_subnet_id" {
  type    = string
  default = null
}

variable "network_plugin" {
  type    = string
  default = "azure"
}

variable "network_plugin_mode" {
  type    = string
  default = "overlay"
}

variable "pod_cidr" {
  type    = string
  default = "10.244.0.0/16"
}

variable "service_cidr" {
  type    = string
  default = "10.0.16.0/22"
}

variable "dns_service_ip" {
  type    = string
  default = "10.0.16.10"
}

variable "kubernetes_version" {
  type    = string
  default = null
}

variable "private_cluster_enabled" {
  type    = bool
  default = false
}

variable "authorized_ip_ranges" {
  type    = list(string)
  default = []
}

variable "sku_tier" {
  type    = string
  default = "Free"
}

variable "identity_type" {
  type    = string
  default = "SystemAssigned"
}

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
}
```

- [ ] **Step 2: Commit**

```bash
git add pkg/provider/azure/templates/variables.tf
git commit -m "feat(azure): declare shim variables matching Track A module"
```

---

### Task 6: Create `templates/outputs.tf`, `templates/provider.tf`, `templates/backend.tf`

**Files:**
- Create: `pkg/provider/azure/templates/outputs.tf`
- Create: `pkg/provider/azure/templates/provider.tf`
- Create: `pkg/provider/azure/templates/backend.tf`

- [ ] **Step 1: Write `pkg/provider/azure/templates/outputs.tf`**

```hcl
output "cluster_id" {
  value = module.aks_cluster.cluster_id
}

output "cluster_name" {
  value = module.aks_cluster.cluster_name
}

output "cluster_fqdn" {
  value = module.aks_cluster.cluster_fqdn
}

output "host" {
  value     = module.aks_cluster.host
  sensitive = true
}

output "kube_admin_config_raw" {
  value     = module.aks_cluster.kube_admin_config_raw
  sensitive = true
}

output "cluster_ca_certificate" {
  value     = module.aks_cluster.cluster_ca_certificate
  sensitive = true
}

output "oidc_issuer_url" {
  value = module.aks_cluster.oidc_issuer_url
}

output "kubelet_identity_object_id" {
  value = module.aks_cluster.kubelet_identity_object_id
}

output "kubelet_identity_client_id" {
  value = module.aks_cluster.kubelet_identity_client_id
}

output "node_resource_group" {
  value = module.aks_cluster.node_resource_group
}

output "resource_group_name" {
  value = module.aks_cluster.resource_group_name
}

output "vnet_id" {
  value = module.aks_cluster.vnet_id
}

output "node_subnet_id" {
  value = module.aks_cluster.node_subnet_id
}
```

- [ ] **Step 2: Write `pkg/provider/azure/templates/provider.tf`**

```hcl
provider "azurerm" {
  features {}
  # subscription_id picked up from ARM_SUBSCRIPTION_ID env (exported by NIC from
  # the user's AZURE_SUBSCRIPTION_ID).
}
```

- [ ] **Step 3: Write `pkg/provider/azure/templates/backend.tf`**

```hcl
terraform {
  backend "local" {}
}
```

Matches the AWS provider's MVP backend choice (local state in the working directory).

- [ ] **Step 4: Commit**

```bash
git add pkg/provider/azure/templates/outputs.tf \
        pkg/provider/azure/templates/provider.tf \
        pkg/provider/azure/templates/backend.tf
git commit -m "feat(azure): add shim outputs/provider/backend"
```

---

### Task 7: Create `tofu.go` with TFVars + embed (no toTFVars yet)

**Files:**
- Create: `pkg/provider/azure/tofu.go`

- [ ] **Step 1: Write `pkg/provider/azure/tofu.go`**

```go
package azure

import "embed"

// tofuTemplates contains the OpenTofu shim that calls the Track A module.
// The embedded files are extracted to the working directory at deploy time
// alongside a generated terraform.tfvars.json.
//
//go:embed all:templates
var tofuTemplates embed.FS

// TFVars is the JSON marshalling layer between the parsed Config and OpenTofu.
// Field names use snake_case to match the Terraform module variables. Pointer
// types + `omitempty` let us pass null-as-omitted so the module's defaults win
// when a user doesn't set a value.
type TFVars struct {
	ProjectName               string               `json:"project_name"`
	Location                  string               `json:"location"`
	Tags                      map[string]string    `json:"tags,omitempty"`
	CreateResourceGroup       bool                 `json:"create_resource_group"`
	ExistingResourceGroupName *string              `json:"existing_resource_group_name,omitempty"`
	CreateVNet                bool                 `json:"create_vnet"`
	VNetCIDRBlock             string               `json:"vnet_cidr_block,omitempty"`
	NodeSubnetCIDRBlock       string               `json:"node_subnet_cidr_block,omitempty"`
	ExistingVNetID            *string              `json:"existing_vnet_id,omitempty"`
	ExistingNodeSubnetID      *string              `json:"existing_node_subnet_id,omitempty"`
	NetworkPlugin             string               `json:"network_plugin"`
	NetworkPluginMode         string               `json:"network_plugin_mode"`
	PodCIDR                   string               `json:"pod_cidr,omitempty"`
	ServiceCIDR               string               `json:"service_cidr,omitempty"`
	DNSServiceIP              string               `json:"dns_service_ip,omitempty"`
	KubernetesVersion         *string              `json:"kubernetes_version,omitempty"`
	PrivateClusterEnabled     bool                 `json:"private_cluster_enabled"`
	AuthorizedIPRanges        []string             `json:"authorized_ip_ranges,omitempty"`
	SKUTier                   string               `json:"sku_tier"`
	IdentityType              string               `json:"identity_type"`
	NodeGroups                map[string]TFNodeGroup `json:"node_groups"`
}

// TFNodeGroup is the JSON shape the Terraform module expects for each node
// group. Differs from the YAML-facing NodeGroup by field naming and the
// `mode` defaulting (handled in toTFVars).
type TFNodeGroup struct {
	VMSize       string            `json:"vm_size"`
	MinCount     int               `json:"min_count"`
	MaxCount     int               `json:"max_count"`
	Mode         string            `json:"mode"`
	OSDiskSizeGB int               `json:"os_disk_size_gb,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Taints       []string          `json:"taints,omitempty"`
	Zones        []string          `json:"zones,omitempty"`
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./pkg/provider/azure/...
```

Expected: clean compile.

- [ ] **Step 3: Verify embed picks up templates**

```bash
ls pkg/provider/azure/templates/
```

Expected: `main.tf`, `variables.tf`, `outputs.tf`, `provider.tf`, `backend.tf`.

- [ ] **Step 4: Commit**

```bash
git add pkg/provider/azure/tofu.go
git commit -m "feat(azure): add TFVars struct and embed templates"
```

---

### Task 8: Implement `(*Config).toTFVars()` with tests (TDD)

**Files:**
- Create: `pkg/provider/azure/tofu_test.go`
- Modify: `pkg/provider/azure/tofu.go`

- [ ] **Step 1: Write failing tests**

Create `pkg/provider/azure/tofu_test.go`:

```go
package azure

import (
	"encoding/json"
	"testing"
)

func TestToTFVarsSystemPoolResolution(t *testing.T) {
	cfg := Config{
		Region: "eastus",
		NodeGroups: map[string]NodeGroup{
			"sys":  {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: "System"},
			"user": {Instance: "Standard_D4_v3", MinNodes: 0, MaxNodes: 5},
		},
	}
	vars := cfg.toTFVars("myproj")

	if got := vars.NodeGroups["sys"].Mode; got != "System" {
		t.Errorf("sys.mode = %q, want System", got)
	}
	if got := vars.NodeGroups["user"].Mode; got != "User" {
		t.Errorf("user.mode defaulted = %q, want User", got)
	}
}

func TestToTFVarsCreateFlags(t *testing.T) {
	t.Run("create RG by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
		vars := cfg.toTFVars("p")
		if !vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should default to true")
		}
	})
	t.Run("BYO RG via explicit name", func(t *testing.T) {
		cfg := Config{
			Region:            "eastus",
			ResourceGroupName: "my-rg",
			NodeGroups:        map[string]NodeGroup{"s": {Mode: "System"}},
		}
		vars := cfg.toTFVars("p")
		if vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should be false when ResourceGroupName is set")
		}
		if vars.ExistingResourceGroupName == nil || *vars.ExistingResourceGroupName != "my-rg" {
			t.Errorf("ExistingResourceGroupName not propagated")
		}
	})
	t.Run("create VNet by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
		vars := cfg.toTFVars("p")
		if !vars.CreateVNet {
			t.Error("CreateVNet should default to true")
		}
	})
	t.Run("BYO VNet flips flag", func(t *testing.T) {
		cfg := Config{
			Region:     "eastus",
			NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}},
			Network:    &NetworkConfig{ExistingVNetID: "/subs/.../vn1", ExistingNodeSubnetID: "/subs/.../sub1"},
		}
		vars := cfg.toTFVars("p")
		if vars.CreateVNet {
			t.Error("CreateVNet should be false when ExistingVNetID is set")
		}
	})
}

func TestToTFVarsNICTagsInjected(t *testing.T) {
	cfg := Config{
		Region:     "eastus",
		NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}},
		Tags:       map[string]string{"Env": "dev"},
	}
	vars := cfg.toTFVars("nebari-x")

	if got := vars.Tags["nic.nebari.dev/cluster-name"]; got != "nebari-x" {
		t.Errorf("cluster-name tag = %q, want nebari-x", got)
	}
	if got := vars.Tags["nic.nebari.dev/managed-by"]; got != "nic" {
		t.Errorf("managed-by tag = %q, want nic", got)
	}
	if got := vars.Tags["Env"]; got != "dev" {
		t.Errorf("user tag dropped: %q", got)
	}
}

func TestToTFVarsOmitsEmptyPointers(t *testing.T) {
	cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
	vars := cfg.toTFVars("p")

	b, err := json.Marshal(vars)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{
		"existing_resource_group_name",
		"existing_vnet_id",
		"existing_node_subnet_id",
		"kubernetes_version",
	} {
		if contains(s, key) {
			t.Errorf("expected %q to be omitted from JSON, got: %s", key, s)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./pkg/provider/azure/ -run TestToTFVars -v
```

Expected: compile error — `toTFVars` doesn't exist yet.

- [ ] **Step 3: Implement `toTFVars()` in `tofu.go`**

Append to `pkg/provider/azure/tofu.go`:

```go
const (
	tagClusterName = "nic.nebari.dev/cluster-name"
	tagManagedBy   = "nic.nebari.dev/managed-by"
)

// toTFVars converts a parsed Config into the JSON-friendly TFVars accepted by
// the embedded Terraform shim. Performs three transforms:
//  1. Default each node group's Mode to "User" if empty.
//  2. Resolve create_resource_group / create_vnet flags from BYO presence.
//  3. Inject NIC-required tags for tag-based discovery.
func (c *Config) toTFVars(projectName string) TFVars {
	vars := TFVars{
		ProjectName:           projectName,
		Location:              c.Region,
		Tags:                  mergeTags(c.Tags, projectName),
		CreateResourceGroup:   c.CreateResourceGroup == nil && c.ResourceGroupName == "" || (c.CreateResourceGroup != nil && *c.CreateResourceGroup),
		CreateVNet:            c.Network == nil || c.Network.ExistingVNetID == "",
		NetworkPlugin:         "azure",
		NetworkPluginMode:     "overlay",
		PrivateClusterEnabled: c.PrivateClusterEnabled,
		AuthorizedIPRanges:    c.AuthorizedIPRanges,
		SKUTier:               defaultIfEmpty(c.SKUTier, "Free"),
		IdentityType:          "SystemAssigned",
		NodeGroups:            convertNodeGroups(c.NodeGroups),
	}

	if c.ResourceGroupName != "" {
		vars.CreateResourceGroup = false
		vars.ExistingResourceGroupName = &c.ResourceGroupName
	}

	if c.KubernetesVersion != "" {
		vars.KubernetesVersion = &c.KubernetesVersion
	}

	if c.Network != nil {
		vars.VNetCIDRBlock = c.Network.VNetCIDRBlock
		vars.NodeSubnetCIDRBlock = c.Network.NodeSubnetCIDRBlock
		vars.PodCIDR = c.Network.PodCIDR
		vars.ServiceCIDR = c.Network.ServiceCIDR
		vars.DNSServiceIP = c.Network.DNSServiceIP
		if c.Network.ExistingVNetID != "" {
			vars.ExistingVNetID = &c.Network.ExistingVNetID
		}
		if c.Network.ExistingNodeSubnetID != "" {
			vars.ExistingNodeSubnetID = &c.Network.ExistingNodeSubnetID
		}
	}

	return vars
}

func convertNodeGroups(in map[string]NodeGroup) map[string]TFNodeGroup {
	out := make(map[string]TFNodeGroup, len(in))
	for name, ng := range in {
		mode := ng.Mode
		if mode == "" {
			mode = "User"
		}
		out[name] = TFNodeGroup{
			VMSize:       ng.Instance,
			MinCount:     ng.MinNodes,
			MaxCount:     ng.MaxNodes,
			Mode:         mode,
			OSDiskSizeGB: ng.OSDiskSizeGB,
			Labels:       ng.Labels,
			Taints:       ng.Taints,
			Zones:        ng.Zones,
		}
	}
	return out
}

func mergeTags(user map[string]string, projectName string) map[string]string {
	out := make(map[string]string, len(user)+2)
	for k, v := range user {
		out[k] = v
	}
	out[tagClusterName] = projectName
	out[tagManagedBy] = "nic"
	return out
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./pkg/provider/azure/ -run TestToTFVars -v
```

Expected: all subtests PASS.

- [ ] **Step 5: Run all azure package tests + vet + lint**

```bash
go test ./pkg/provider/azure/...
make vet
make lint
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/tofu.go pkg/provider/azure/tofu_test.go
git commit -m "feat(azure): implement (*Config).toTFVars with TDD tests"
```

---

### Task 9: Update `examples/azure-config.yaml` to match final schema

**Files:**
- Modify: `examples/azure-config.yaml`

- [ ] **Step 1: Replace `examples/azure-config.yaml`**

```yaml
project_name: my-nebari-azure
domain: nebari.example.com

# TLS certificate configuration
certificate:
  type: letsencrypt
  acme:
    email: admin@example.com

# GitOps repository configuration (required for foundational services)
git_repository:
  url: "git@github.com:my-org/my-gitops-repo.git"
  branch: main
  path: "clusters/my-nebari-azure"
  auth:
    ssh_key_env: GIT_SSH_PRIVATE_KEY

cluster:
  azure:
    region: eastus

    # Optional: omit to let NIC create "<project_name>-rg"
    # resource_group_name: my-rg

    kubernetes_version: "1.34"
    sku_tier: Free
    private_cluster_enabled: false

    # Restrict API server access to specific CIDRs. [] means open.
    # authorized_ip_ranges:
    #   - 203.0.113.0/24

    network:
      vnet_cidr_block: "10.0.0.0/16"
      node_subnet_cidr_block: "10.0.0.0/22"
      pod_cidr: "10.244.0.0/16"
      service_cidr: "10.0.16.0/22"
      dns_service_ip: "10.0.16.10"
      # BYO networking:
      # existing_vnet_id: /subscriptions/.../virtualNetworks/foo
      # existing_node_subnet_id: /subscriptions/.../subnets/foo

    node_groups:
      # Exactly one node group must have mode=System (or omit mode entirely
      # and the first entry will be defaulted to System).
      system:
        instance: Standard_D4_v3
        min_nodes: 1
        max_nodes: 3
        mode: System

      user:
        instance: Standard_D8_v3
        min_nodes: 1
        max_nodes: 5

      worker:
        instance: Standard_D4_v3
        min_nodes: 0
        max_nodes: 5

    tags:
      Environment: development
      Project: nebari

# Required environment variables:
#   AZURE_SUBSCRIPTION_ID  (will be exported as ARM_SUBSCRIPTION_ID for tofu)
#
# Auth picked up by azidentity.DefaultAzureCredential, in order:
#   1. Env vars (AZURE_CLIENT_ID + AZURE_TENANT_ID + AZURE_CLIENT_SECRET)
#   2. Workload identity (in-cluster)
#   3. Managed identity
#   4. Azure CLI (az login)
```

- [ ] **Step 2: Commit**

```bash
git add examples/azure-config.yaml
git commit -m "docs(azure): align example config with final schema"
```

---

### Task 10: Run full check suite after Phase B1

- [ ] **Step 1: Verify everything still passes**

```bash
make check
```

Expected: format, vet, lint, tests all green. The azure package now has real Config + Validate + toTFVars with tests, but no Provider impl yet — that's Phase B2.

- [ ] **Step 2: No additional commit** — verification only. Phase B1 complete.

---

## Phase B2 — Deploy / Destroy (Tasks 11–16)

### Task 11: Create `interfaces.go` with mock-friendly SDK interfaces

**Files:**
- Create: `pkg/provider/azure/interfaces.go`

- [ ] **Step 1: Add Azure SDK dependencies to `go.mod`**

```bash
cd ~/gh/nebari-infrastructure-core
go get github.com/Azure/azure-sdk-for-go/sdk/azidentity
go get github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6
go get github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources
go get github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/subscription/armsubscription
go mod tidy
```

- [ ] **Step 2: Write `pkg/provider/azure/interfaces.go`**

```go
package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// managedClustersAPI is the subset of armcontainerservice.ManagedClustersClient
// that the Azure provider uses. Tests inject a fake; production wires the real
// client in NewProvider.
type managedClustersAPI interface {
	ListClusterAdminCredentials(
		ctx context.Context,
		resourceGroupName, resourceName string,
		options *armcontainerservice.ManagedClustersClientListClusterAdminCredentialsOptions,
	) (armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse, error)

	Get(
		ctx context.Context,
		resourceGroupName, resourceName string,
		options *armcontainerservice.ManagedClustersClientGetOptions,
	) (armcontainerservice.ManagedClustersClientGetResponse, error)
}

// managedClusterVersionsAPI exposes the AKS-supported-versions lookup.
type managedClusterVersionsAPI interface {
	ListKubernetesVersions(
		ctx context.Context,
		location string,
		options *armcontainerservice.ManagedClustersClientListKubernetesVersionsOptions,
	) (armcontainerservice.ManagedClustersClientListKubernetesVersionsResponse, error)
}

// resourcesAPI is the subset of armresources.Client used by state.go / cleanup.go.
type resourcesAPI interface {
	NewListPager(
		options *armresources.ClientListOptions,
	) *runtime.Pager[armresources.ClientListResponse]
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./pkg/provider/azure/...
```

Expected: clean compile.

- [ ] **Step 4: Commit**

```bash
git add pkg/provider/azure/interfaces.go go.mod go.sum
git commit -m "feat(azure): add SDK client interfaces for testability"
```

---

### Task 12: Rewrite `provider.go` skeleton with real Validate (TDD)

**Files:**
- Create: `pkg/provider/azure/provider_test.go` (replace stub)
- Modify: `pkg/provider/azure/provider.go`

- [ ] **Step 1: Read AWS provider's `provider.go` for pattern reference**

```bash
sed -n '1,80p' pkg/provider/aws/provider.go
```

Note the OTel span pattern, `parseConfig` helper, status messaging.

- [ ] **Step 2: Write failing tests in `pkg/provider/azure/provider_test.go`**

```go
package azure

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestProviderName(t *testing.T) {
	p := NewProvider()
	if got := p.Name(); got != "azure" {
		t.Errorf("Name() = %q, want \"azure\"", got)
	}
}

func TestProviderValidateRejectsBadConfig(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			"azure": map[string]any{
				// missing region — must fail
				"node_groups": map[string]any{
					"system": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      "System",
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "00000000-0000-0000-0000-000000000000")
	err := p.Validate(context.Background(), "myproj", cc)
	if err == nil {
		t.Fatal("expected validation error for missing region")
	}
}

func TestProviderValidateRequiresSubscriptionID(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			"azure": map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"system": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      "System",
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Validate(context.Background(), "myproj", cc)
	if err == nil {
		t.Fatal("expected error when AZURE_SUBSCRIPTION_ID is unset")
	}
}

func TestProviderInfraSettings(t *testing.T) {
	p := NewProvider()
	settings := p.InfraSettings(nil)
	if settings.StorageClass != "managed-csi" {
		t.Errorf("StorageClass = %q, want managed-csi", settings.StorageClass)
	}
	if settings.NeedsMetalLB {
		t.Error("NeedsMetalLB = true, want false")
	}
}

func TestProviderSummaryWithConfig(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			"azure": map[string]any{
				"region":              "eastus",
				"resource_group_name": "rg-1",
				"node_groups": map[string]any{
					"a": map[string]any{},
					"b": map[string]any{},
				},
			},
		},
	}
	s := p.Summary(cc)
	if s["Region"] != "eastus" {
		t.Errorf("Region = %q", s["Region"])
	}
	if s["ResourceGroup"] != "rg-1" {
		t.Errorf("ResourceGroup = %q", s["ResourceGroup"])
	}
	if s["NodeGroupCount"] != "2" {
		t.Errorf("NodeGroupCount = %q, want 2", s["NodeGroupCount"])
	}
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```bash
go test ./pkg/provider/azure/ -run TestProvider -v
```

Expected: some pass (the stub returns the right Name() and InfraSettings()), others fail (Validate doesn't reject bad config; Summary doesn't return useful info).

- [ ] **Step 4: Replace `pkg/provider/azure/provider.go` with the new skeleton**

```go
package azure

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

const subscriptionIDEnv = "AZURE_SUBSCRIPTION_ID"

// Provider implements the Azure cloud provider for NIC.
type Provider struct{}

// NewProvider returns a fresh Azure provider. Registered in cmd/nic/main.go.
func NewProvider() *Provider {
	return &Provider{}
}

// Name returns the provider name used in cluster.azure: dispatch.
func (p *Provider) Name() string { return "azure" }

func (p *Provider) parseConfig(ctx context.Context, clusterConfig *config.ClusterConfig) (*Config, error) {
	raw := clusterConfig.ProviderConfig()
	if raw == nil {
		return nil, fmt.Errorf("cluster.azure block is missing")
	}
	var cfg Config
	if err := config.UnmarshalProviderConfig(ctx, raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse azure config: %w", err)
	}
	return &cfg, nil
}

// Validate checks config integrity and probes Azure auth via env vars.
func (p *Provider) Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Validate")
	defer span.End()
	span.SetAttributes(
		attribute.String("provider", "azure"),
		attribute.String("project_name", projectName),
	)

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := cfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	if os.Getenv(subscriptionIDEnv) == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Azure configuration validated").
		WithResource("provider").
		WithAction("validate").
		WithMetadata("cluster_name", projectName))
	return nil
}

// Deploy is implemented in Task 13.
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DeployOptions) error {
	return fmt.Errorf("azure.Deploy: not implemented in this commit")
}

// Destroy is implemented in Task 14.
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DestroyOptions) error {
	return fmt.Errorf("azure.Destroy: not implemented in this commit")
}

// GetKubeconfig is implemented in Task 17.
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	return nil, fmt.Errorf("azure.GetKubeconfig: not implemented in this commit")
}

// Summary returns display-only metadata about the cluster from config.
func (p *Provider) Summary(clusterConfig *config.ClusterConfig) map[string]string {
	out := make(map[string]string)
	cfg, err := p.parseConfig(context.Background(), clusterConfig)
	if err != nil {
		return out
	}
	out["Region"] = cfg.Region
	if cfg.ResourceGroupName != "" {
		out["ResourceGroup"] = cfg.ResourceGroupName
	}
	out["NodeGroupCount"] = strconv.Itoa(len(cfg.NodeGroups))
	return out
}

// InfraSettings returns Azure-specific Kubernetes infra settings.
func (p *Provider) InfraSettings(_ *config.ClusterConfig) provider.InfraSettings {
	return provider.InfraSettings{
		StorageClass: "managed-csi",
		NeedsMetalLB: false,
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./pkg/provider/azure/ -v
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/provider.go pkg/provider/azure/provider_test.go
git commit -m "feat(azure): real Validate + Summary + InfraSettings"
```

---

### Task 13: Implement `Deploy()` via `pkg/tofu`

**Files:**
- Modify: `pkg/provider/azure/provider.go`
- Modify: `pkg/provider/azure/tofu.go` (add helper)

- [ ] **Step 1: Read the AWS provider's Deploy for the pattern**

```bash
grep -n "func (p \*Provider) Deploy" pkg/provider/aws/provider.go
```

Then read the surrounding ~80 lines. Note how it: parses config, builds TFVars, calls `tofu.Run*` via `pkg/tofu`, streams status updates, returns errors.

- [ ] **Step 2: Add a helper in `tofu.go` to extract templates + write tfvars**

Append to `pkg/provider/azure/tofu.go`:

```go
import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// prepareWorkingDir extracts the embedded shim templates into dir and writes
// terraform.tfvars.json from the given TFVars. Existing files are overwritten.
func prepareWorkingDir(dir string, vars TFVars) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create working dir: %w", err)
	}

	entries, err := fs.ReadDir(tofuTemplates, "templates")
	if err != nil {
		return fmt.Errorf("read embedded templates: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src, err := fs.ReadFile(tofuTemplates, filepath.Join("templates", e.Name()))
		if err != nil {
			return fmt.Errorf("read template %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), src, 0o644); err != nil {
			return fmt.Errorf("write template %s: %w", e.Name(), err)
		}
	}

	b, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tfvars: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars.json"), b, 0o644); err != nil {
		return fmt.Errorf("write tfvars: %w", err)
	}
	return nil
}

// workingDirForCluster returns a stable working dir path under the user's
// XDG state dir so the local Terraform state persists across nic invocations.
func workingDirForCluster(projectName string) (string, error) {
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if stateRoot == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateRoot = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateRoot, "nic", "azure", projectName), nil
}
```

- [ ] **Step 3: Replace the stub Deploy in `provider.go`**

Replace the body of `(*Provider).Deploy()`:

```go
func (p *Provider) Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DeployOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Deploy")
	defer span.End()
	span.SetAttributes(attribute.String("provider", "azure"), attribute.String("project_name", projectName))

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := cfg.Validate(); err != nil {
		span.RecordError(err)
		return err
	}

	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	workDir, err := workingDirForCluster(projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := prepareWorkingDir(workDir, cfg.toTFVars(projectName)); err != nil {
		span.RecordError(err)
		return err
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Initializing OpenTofu working directory").
		WithResource("tofu").WithAction("init").WithMetadata("dir", workDir))

	// pkg/tofu wires up OpenTofu binary discovery + tfexec.
	runner, err := tofu.NewRunner(ctx, workDir, tofu.WithEnv(map[string]string{
		"ARM_SUBSCRIPTION_ID": subID,
	}))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("create tofu runner: %w", err)
	}

	if err := runner.Init(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu init: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Applying Terraform plan").
		WithResource("tofu").WithAction("apply").WithMetadata("dir", workDir))
	if err := runner.Apply(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu apply: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Azure cluster deployed").
		WithResource("cluster").WithAction("deploy").WithMetadata("cluster_name", projectName))
	return nil
}
```

Add the import for `pkg/tofu` at the top:

```go
import (
	// ... existing imports ...
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)
```

**Note:** If `pkg/tofu` doesn't yet have `NewRunner`/`WithEnv`/`Init`/`Apply` in this exact shape, inspect its actual API and adjust — the AWS provider uses it in the same way, so look at how AWS calls it (`grep -n "pkg/tofu" pkg/provider/aws/*.go`) and mirror that.

- [ ] **Step 4: Add a unit test that asserts Deploy fails fast on missing subscription ID**

Append to `provider_test.go`:

```go
func TestProviderDeployFailsWithoutSubscription(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			"azure": map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"s": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      "System",
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Deploy(context.Background(), "p", cc, provider.DeployOptions{})
	if err == nil {
		t.Fatal("expected Deploy to fail without subscription ID")
	}
}
```

- [ ] **Step 5: Run tests + vet + lint**

```bash
go test ./pkg/provider/azure/...
make vet
make lint
```

Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/provider.go pkg/provider/azure/tofu.go pkg/provider/azure/provider_test.go
git commit -m "feat(azure): implement Deploy via pkg/tofu"
```

---

### Task 14: Implement `Destroy()` via `pkg/tofu`

**Files:**
- Modify: `pkg/provider/azure/provider.go`

- [ ] **Step 1: Replace the stub Destroy**

```go
func (p *Provider) Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, _ provider.DestroyOptions) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.Destroy")
	defer span.End()
	span.SetAttributes(attribute.String("provider", "azure"), attribute.String("project_name", projectName))

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return err
	}

	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		err := fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
		span.RecordError(err)
		return err
	}

	workDir, err := workingDirForCluster(projectName)
	if err != nil {
		span.RecordError(err)
		return err
	}
	if err := prepareWorkingDir(workDir, cfg.toTFVars(projectName)); err != nil {
		span.RecordError(err)
		return err
	}

	runner, err := tofu.NewRunner(ctx, workDir, tofu.WithEnv(map[string]string{
		"ARM_SUBSCRIPTION_ID": subID,
	}))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("create tofu runner: %w", err)
	}
	if err := runner.Init(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu init: %w", err)
	}

	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Destroying Azure cluster").
		WithResource("tofu").WithAction("destroy").WithMetadata("dir", workDir))
	if err := runner.Destroy(ctx); err != nil {
		span.RecordError(err)
		return fmt.Errorf("tofu destroy: %w", err)
	}

	// Tag-based orphan cleanup runs after tofu destroy succeeds.
	if err := cleanupOrphans(ctx, subID, projectName); err != nil {
		// Non-fatal: log but don't fail destroy — user will retry.
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Orphan cleanup encountered issues").
			WithResource("cleanup").WithAction("destroy").WithMetadata("error", err.Error()))
	}

	status.Send(ctx, status.NewUpdate(status.LevelSuccess, "Azure cluster destroyed").
		WithResource("cluster").WithAction("destroy").WithMetadata("cluster_name", projectName))
	return nil
}
```

`cleanupOrphans` is defined in Task 16.

- [ ] **Step 2: Add a unit test that asserts Destroy fails fast on missing subscription ID**

Append to `provider_test.go`:

```go
func TestProviderDestroyFailsWithoutSubscription(t *testing.T) {
	p := NewProvider()
	cc := &config.ClusterConfig{
		Providers: map[string]any{
			"azure": map[string]any{
				"region": "eastus",
				"node_groups": map[string]any{
					"s": map[string]any{
						"instance":  "Standard_D2_v3",
						"min_nodes": 1,
						"max_nodes": 1,
						"mode":      "System",
					},
				},
			},
		},
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	err := p.Destroy(context.Background(), "p", cc, provider.DestroyOptions{})
	if err == nil {
		t.Fatal("expected Destroy to fail without subscription ID")
	}
}
```

- [ ] **Step 3: Test, vet, lint (Destroy references undefined `cleanupOrphans` — compile may fail)**

If compile fails on `cleanupOrphans`, comment out that block temporarily, run tests, and re-enable in Task 16 commit. Mark this task DONE_WITH_CONCERNS in that case.

- [ ] **Step 4: Commit**

```bash
git add pkg/provider/azure/provider.go pkg/provider/azure/provider_test.go
git commit -m "feat(azure): implement Destroy via pkg/tofu"
```

---

### Task 15: Implement `state.go` for tag-based discovery (TDD)

**Files:**
- Create: `pkg/provider/azure/state.go`
- Create: `pkg/provider/azure/state_test.go`

- [ ] **Step 1: Write the failing test**

```go
package azure

import (
	"strings"
	"testing"
)

func TestBuildTagFilter(t *testing.T) {
	got := buildTagFilter("my-cluster")
	if !strings.Contains(got, "nic.nebari.dev/cluster-name") {
		t.Errorf("filter missing cluster-name tag: %s", got)
	}
	if !strings.Contains(got, "my-cluster") {
		t.Errorf("filter missing project name: %s", got)
	}
	if !strings.Contains(got, "nic.nebari.dev/managed-by") {
		t.Errorf("filter missing managed-by tag: %s", got)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./pkg/provider/azure/ -run TestBuildTagFilter -v
```

Expected: compile error.

- [ ] **Step 3: Write `state.go`**

```go
package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// buildTagFilter returns the Azure Resource Graph `$filter` expression that
// matches every resource NIC tagged for this cluster.
func buildTagFilter(projectName string) string {
	return fmt.Sprintf(
		"tagName eq '%s' and tagValue eq '%s'",
		tagClusterName, projectName,
	)
}

// listTaggedResources enumerates resources matching the NIC cluster tags.
// Returns IDs suitable for cleanup.
func listTaggedResources(ctx context.Context, client resourcesAPI, projectName string) ([]string, error) {
	pager := client.NewListPager(&armresources.ClientListOptions{
		Filter: to.Ptr(buildTagFilter(projectName)),
	})

	var ids []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tagged resources: %w", err)
		}
		for _, r := range page.Value {
			if r.ID != nil {
				ids = append(ids, *r.ID)
			}
		}
	}
	return ids, nil
}
```

- [ ] **Step 4: Verify test passes + vet/lint**

```bash
go test ./pkg/provider/azure/ -run TestBuildTagFilter -v
make vet
make lint
```

- [ ] **Step 5: Commit**

```bash
git add pkg/provider/azure/state.go pkg/provider/azure/state_test.go
git commit -m "feat(azure): tag-based resource discovery for cleanup"
```

---

### Task 16: Implement `cleanup.go` for orphan-resource cleanup

**Files:**
- Create: `pkg/provider/azure/cleanup.go`
- Create: `pkg/provider/azure/cleanup_test.go`

- [ ] **Step 1: Write failing tests**

```go
package azure

import (
	"context"
	"testing"
)

// fakeResources implements resourcesAPI for tests. Returns a fixed set of IDs.
type fakeResources struct {
	ids []string
}

// classify divides IDs into MC_* resource groups vs other resources for ordering.
func TestClassifyMCResourceGroups(t *testing.T) {
	ids := []string{
		"/subscriptions/x/resourceGroups/my-cluster-rg",
		"/subscriptions/x/resourceGroups/MC_my-cluster-rg_my-cluster-aks_eastus",
		"/subscriptions/x/resourceGroups/MC_my-cluster-rg_my-cluster-aks_eastus/providers/Microsoft.Network/loadBalancers/foo",
	}
	mc, others := classifyMC(ids)
	if len(mc) != 2 {
		t.Errorf("expected 2 MC items (RG + resource inside), got %d: %v", len(mc), mc)
	}
	if len(others) != 1 {
		t.Errorf("expected 1 non-MC item, got %d: %v", len(others), others)
	}
}

func TestCleanupOrphansNoopWhenNothing(t *testing.T) {
	if err := cleanupOrphansWithClient(context.Background(), &noopResources{}, "p"); err != nil {
		t.Errorf("expected nil error on empty list, got: %v", err)
	}
}

type noopResources struct{}

func (n *noopResources) NewListPager(_ *armresources.ClientListOptions) *runtime.Pager[armresources.ClientListResponse] {
	// Real implementation returns a finished pager. Skip the test if this can't
	// be constructed easily in unit tests — alternative: refactor signature to
	// return a slice directly. See task note below.
	return nil
}
```

**Task note:** Building a fake `runtime.Pager[T]` in tests is awkward. A pragmatic alternative is to make `listTaggedResources` take an inner "list once" function and have `cleanupOrphans` call it through a higher-level seam. If that's too much refactor pressure for this task, write `listTaggedResourcesFunc` as a function-type indirection and inject a slice in tests. Either approach is acceptable; pick whichever keeps `state.go` simple.

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./pkg/provider/azure/ -run TestCleanup -v
```

- [ ] **Step 3: Write `cleanup.go`**

```go
package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// classifyMC splits resource IDs into those inside an AKS-managed MC_* group
// and everything else. MC_ items are deleted last because their parent
// resource group will cascade them.
func classifyMC(ids []string) (mc, others []string) {
	for _, id := range ids {
		if strings.Contains(id, "/resourceGroups/MC_") {
			mc = append(mc, id)
		} else {
			others = append(others, id)
		}
	}
	return mc, others
}

// cleanupOrphans is the entry point called by Destroy. It enumerates resources
// tagged for this cluster, identifies what Terraform missed, and deletes them
// in a safe order (LBs/disks → MC_ RG → main RG).
func cleanupOrphans(ctx context.Context, subscriptionID, projectName string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure auth: %w", err)
	}
	factory, err := armresources.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armresources client: %w", err)
	}
	return cleanupOrphansWithClient(ctx, factory.NewClient(), projectName)
}

// cleanupOrphansWithClient is the unit-testable inner that takes the
// resourcesAPI seam.
func cleanupOrphansWithClient(ctx context.Context, client resourcesAPI, projectName string) error {
	ids, err := listTaggedResources(ctx, client, projectName)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil // happy path: tofu destroy was thorough.
	}
	// In MVP we report the orphans rather than auto-delete. Auto-delete will
	// land in a follow-up once the integration test confirms what tofu leaves
	// behind in practice.
	mc, others := classifyMC(ids)
	return fmt.Errorf("found %d orphaned resources (MC: %d, other: %d); run `az resource delete --ids %s` to clean up",
		len(ids), len(mc), len(others), strings.Join(ids, " "))
}
```

**Scope note:** Auto-delete is deferred to a follow-up. The MVP just *reports* orphans, which is enough to keep users from being silently billed. If you want to ship auto-delete in this task, replace the final `return fmt.Errorf(...)` with calls to `armresources.NewClient().BeginDeleteByID()` for each ID — careful to delete non-MC items first, then MC RG, then main RG.

- [ ] **Step 4: Verify tests pass + vet/lint**

```bash
go test ./pkg/provider/azure/...
make vet
make lint
```

- [ ] **Step 5: Re-verify Destroy compiles (uncomment the cleanup call from Task 14 if you commented it out)**

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/cleanup.go pkg/provider/azure/cleanup_test.go pkg/provider/azure/provider.go
git commit -m "feat(azure): orphan-resource detection and reporting on destroy"
```

---

## Phase B3 — Kubeconfig + Polish (Tasks 17–19)

### Task 17: Implement `kubeconfig.go` via armcontainerservice SDK (TDD)

**Files:**
- Create: `pkg/provider/azure/kubeconfig.go`
- Create: `pkg/provider/azure/kubeconfig_test.go`
- Modify: `pkg/provider/azure/provider.go` (wire `GetKubeconfig`)

- [ ] **Step 1: Write failing test**

```go
package azure

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

type fakeManagedClusters struct {
	credentials string
	getResult   *armcontainerservice.ManagedCluster
}

func (f *fakeManagedClusters) ListClusterAdminCredentials(_ context.Context, _, _ string, _ *armcontainerservice.ManagedClustersClientListClusterAdminCredentialsOptions) (armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse, error) {
	return armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse{
		CredentialResults: armcontainerservice.CredentialResults{
			Kubeconfigs: []*armcontainerservice.CredentialResult{
				{Name: to.Ptr("clusterAdmin"), Value: []byte(f.credentials)},
			},
		},
	}, nil
}

func (f *fakeManagedClusters) Get(_ context.Context, _, _ string, _ *armcontainerservice.ManagedClustersClientGetOptions) (armcontainerservice.ManagedClustersClientGetResponse, error) {
	return armcontainerservice.ManagedClustersClientGetResponse{ManagedCluster: *f.getResult}, nil
}

func TestFetchAdminKubeconfig(t *testing.T) {
	api := &fakeManagedClusters{credentials: "apiVersion: v1\nkind: Config\n# fake"}
	got, err := fetchAdminKubeconfig(context.Background(), api, "rg-1", "my-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "" {
		t.Error("empty kubeconfig")
	}
	if !contains(string(got), "kind: Config") {
		t.Errorf("unexpected kubeconfig payload: %s", got)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./pkg/provider/azure/ -run TestFetchAdminKubeconfig -v
```

- [ ] **Step 3: Write `kubeconfig.go`**

```go
package azure

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

// fetchAdminKubeconfig pulls the admin kubeconfig via the AKS data plane API.
// Inner function: takes a managedClustersAPI interface so tests can fake it.
func fetchAdminKubeconfig(ctx context.Context, api managedClustersAPI, resourceGroup, clusterName string) ([]byte, error) {
	resp, err := api.ListClusterAdminCredentials(ctx, resourceGroup, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("list admin credentials: %w", err)
	}
	for _, kc := range resp.Kubeconfigs {
		if kc != nil && len(kc.Value) > 0 {
			return kc.Value, nil
		}
	}
	return nil, fmt.Errorf("no kubeconfig returned for cluster %q", clusterName)
}

// newManagedClustersClient wires the real SDK client. Production path.
func newManagedClustersClient(subscriptionID string) (managedClustersAPI, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure auth: %w", err)
	}
	factory, err := armcontainerservice.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armcontainerservice factory: %w", err)
	}
	return factory.NewManagedClustersClient(), nil
}

// resolveResourceGroup returns either the user-supplied RG or the convention
// "<project_name>-rg" used by the Terraform module when create_resource_group=true.
func resolveResourceGroup(cfg *Config, projectName string) string {
	if cfg.ResourceGroupName != "" {
		return cfg.ResourceGroupName
	}
	return projectName + "-rg"
}

// resolveClusterName mirrors the Terraform module's "${var.project_name}-aks".
func resolveClusterName(projectName string) string {
	return projectName + "-aks"
}

// fetchKubeconfigForCluster is the high-level wrapper called by Provider.GetKubeconfig.
func fetchKubeconfigForCluster(ctx context.Context, cfg *Config, projectName string) ([]byte, error) {
	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		return nil, fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
	}
	api, err := newManagedClustersClient(subID)
	if err != nil {
		return nil, err
	}
	return fetchAdminKubeconfig(ctx, api, resolveResourceGroup(cfg, projectName), resolveClusterName(projectName))
}
```

- [ ] **Step 4: Wire `Provider.GetKubeconfig` to call it**

Replace the `GetKubeconfig` stub in `provider.go`:

```go
func (p *Provider) GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.GetKubeconfig")
	defer span.End()
	span.SetAttributes(attribute.String("provider", "azure"), attribute.String("project_name", projectName))

	cfg, err := p.parseConfig(ctx, clusterConfig)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	kc, err := fetchKubeconfigForCluster(ctx, cfg, projectName)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	status.Send(ctx, status.NewUpdate(status.LevelInfo, "Kubeconfig fetched from AKS API").
		WithResource("cluster").WithAction("get-kubeconfig").WithMetadata("cluster_name", projectName))
	return kc, nil
}
```

- [ ] **Step 5: Tests + vet + lint**

```bash
go test ./pkg/provider/azure/...
make vet
make lint
```

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/azure/kubeconfig.go pkg/provider/azure/kubeconfig_test.go pkg/provider/azure/provider.go
git commit -m "feat(azure): fetch admin kubeconfig via armcontainerservice SDK"
```

---

### Task 18: Implement `version.go` — AKS supported-version negotiation

**Files:**
- Create: `pkg/provider/azure/version.go`
- Create: `pkg/provider/azure/version_test.go`

This helper is used by future work (`Validate` could reject user-requested versions that AKS doesn't support in a given region). For MVP it's a small, well-tested utility that future code can plug into.

- [ ] **Step 1: Write failing test**

```go
package azure

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

type fakeVersionsAPI struct {
	versions []string
}

func (f *fakeVersionsAPI) ListKubernetesVersions(_ context.Context, _ string, _ *armcontainerservice.ManagedClustersClientListKubernetesVersionsOptions) (armcontainerservice.ManagedClustersClientListKubernetesVersionsResponse, error) {
	patches := make([]*armcontainerservice.KubernetesVersion, 0, len(f.versions))
	for _, v := range f.versions {
		patches = append(patches, &armcontainerservice.KubernetesVersion{Version: to.Ptr(v)})
	}
	return armcontainerservice.ManagedClustersClientListKubernetesVersionsResponse{
		KubernetesVersionListResult: armcontainerservice.KubernetesVersionListResult{Values: patches},
	}, nil
}

func TestListSupportedVersions(t *testing.T) {
	api := &fakeVersionsAPI{versions: []string{"1.32", "1.33", "1.34"}}
	got, err := listSupportedVersions(context.Background(), api, "eastus")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if got[0] != "1.32" {
		t.Errorf("first = %q, want 1.32", got[0])
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./pkg/provider/azure/ -run TestListSupportedVersions -v
```

- [ ] **Step 3: Write `version.go`**

```go
package azure

import (
	"context"
	"fmt"
)

func listSupportedVersions(ctx context.Context, api managedClusterVersionsAPI, location string) ([]string, error) {
	resp, err := api.ListKubernetesVersions(ctx, location, nil)
	if err != nil {
		return nil, fmt.Errorf("list AKS versions in %s: %w", location, err)
	}
	out := make([]string, 0, len(resp.Values))
	for _, v := range resp.Values {
		if v != nil && v.Version != nil {
			out = append(out, *v.Version)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Tests + vet + lint**

```bash
go test ./pkg/provider/azure/...
make vet
make lint
```

- [ ] **Step 5: Commit**

```bash
git add pkg/provider/azure/version.go pkg/provider/azure/version_test.go
git commit -m "feat(azure): AKS supported-version listing helper"
```

---

### Task 19: Final Phase B3 check + integration test stub

**Files:**
- Create: `pkg/provider/azure/INTEGRATION_TESTING.md`

- [ ] **Step 1: Run the full check suite**

```bash
make check
```

Expected: format, vet, lint, tests all green.

- [ ] **Step 2: Write `pkg/provider/azure/INTEGRATION_TESTING.md` mirroring the AWS sibling**

```markdown
# Azure Provider Integration Testing

Integration tests for the Azure provider run against a real Azure subscription.
There is no LocalStack-equivalent emulator for AKS/ARM.

## Prerequisites

- `AZURE_SUBSCRIPTION_ID` set to a sub where you can create AKS clusters.
- One of:
  - `az login` completed in the shell
  - Service principal env vars: `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`

## Running

\`\`\`bash
go test -v -tags=integration -timeout 60m ./pkg/provider/azure/...
\`\`\`

Tests gated behind the `integration` build tag are not run by default `make test`.

## Cost

Each test cycle provisions a minimal AKS cluster (Standard_B2s, single-node
system pool, single user pool) and tears it down. Expect a few cents to a
couple of dollars per run depending on region and how long it stays up.
```

- [ ] **Step 3: Commit**

```bash
git add pkg/provider/azure/INTEGRATION_TESTING.md
git commit -m "docs(azure): document integration testing prereqs"
```

---

## Phase C1 — Convergence: Registry pin flip (Task 20)

### Task 20: Flip shim source from git-ref to Registry version

**Files:**
- Modify: `pkg/provider/azure/templates/main.tf`

**Gating:** Do not start this task until Track A has tagged `v0.1.0` and the Registry shows the module at `nebari-dev/aks-cluster/azurerm`. Verify by visiting https://registry.terraform.io/modules/nebari-dev/aks-cluster/azurerm/latest.

- [ ] **Step 1: Update the shim source**

In `pkg/provider/azure/templates/main.tf`, replace:

```hcl
  source = "git::https://github.com/nebari-dev/terraform-azurerm-aks-cluster.git?ref=main"
```

with:

```hcl
  source  = "nebari-dev/aks-cluster/azurerm"
  version = "0.1.0"
```

- [ ] **Step 2: Verify `pkg/tofu` can resolve the Registry source**

Run a deploy dry-run that goes far enough to test `tofu init`:

```bash
cd /tmp
mkdir azure-init-smoke && cd azure-init-smoke
# Copy embedded templates here. Easiest: run `go run` of a small helper, or
# manually copy pkg/provider/azure/templates/*.tf into this dir and run:
tofu init -backend=false
```

Expected: tofu fetches the module from the Registry; init succeeds.

- [ ] **Step 3: Commit**

```bash
cd ~/gh/nebari-infrastructure-core
git add pkg/provider/azure/templates/main.tf
git commit -m "feat(azure): pin shim to nebari-dev/aks-cluster/azurerm v0.1.0"
```

---

## Phase C2 — Real-subscription end-to-end + docs (Tasks 21–22)

### Task 21: End-to-end deploy + destroy against real subscription

This is a manual verification task — **do not run autonomously**. Surface the steps to the user and confirm they succeed.

- [ ] **Step 1: Export subscription ID and credentials**

```bash
export AZURE_SUBSCRIPTION_ID=<your-sub>
az login
```

- [ ] **Step 2: Build NIC**

```bash
cd ~/gh/nebari-infrastructure-core
make build
```

- [ ] **Step 3: Deploy**

```bash
./nic deploy --config examples/azure-config.yaml
```

Expected: progress messages stream through; tofu apply completes in ~15-25 min; final success message.

- [ ] **Step 4: Fetch kubeconfig and verify cluster is reachable**

```bash
./nic kubeconfig --config examples/azure-config.yaml > /tmp/azkc.yaml
KUBECONFIG=/tmp/azkc.yaml kubectl get nodes
```

Expected: nodes Ready.

- [ ] **Step 5: Destroy**

```bash
./nic destroy --config examples/azure-config.yaml
```

Expected: tofu destroy completes; cleanup reports either "no orphans" or a list of orphan IDs.

- [ ] **Step 6: Manually verify no leaked resources in the subscription**

```bash
az resource list --tag nic.nebari.dev/cluster-name=my-nebari-azure --output table
```

Expected: empty result.

- [ ] **Step 7: No code commit unless something needed fixing during the run**

If issues surfaced (e.g., a missing field in tofu.go, a kubeconfig path bug), fix them with a follow-up task per issue, run review, and merge before tagging.

---

### Task 22: Update top-level docs to drop "Azure stub" language

**Files:**
- Modify: `ARCHITECTURE.md`
- Modify: `WALKTHROUGH.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Grep for stub mentions**

```bash
cd ~/gh/nebari-infrastructure-core
grep -n -i "azure" ARCHITECTURE.md WALKTHROUGH.md CLAUDE.md | grep -i -E "(stub|not yet|todo|future)"
```

- [ ] **Step 2: Edit each hit**

- `ARCHITECTURE.md`: change "Azure stub provider" / "Azure (stub)" to descriptions matching AWS's wording. Add a one-paragraph section describing Azure's deploy flow.
- `WALKTHROUGH.md`: update the "Adding a Provider" section if it references Azure as the stub-followup example.
- `CLAUDE.md`: change `**`pkg/provider/{gcp,azure,local}/`** — Stub providers (not yet implemented)` to `**`pkg/provider/{gcp,local}/`** — Stub providers (not yet implemented)` and add Azure to the "fully implemented" list near AWS.

- [ ] **Step 3: Run `make check` one more time**

```bash
make check
```

- [ ] **Step 4: Commit**

```bash
git add ARCHITECTURE.md WALKTHROUGH.md CLAUDE.md
git commit -m "docs: Azure provider is fully implemented (drop stub language)"
```

---

## Acceptance checklist

Before considering Track B complete:

- [ ] `make check` passes on the branch with all new tests.
- [ ] `pkg/provider/azure/` contains all files from the File Structure table.
- [ ] `nic deploy --config examples/azure-config.yaml` deploys AKS end-to-end against a real subscription.
- [ ] `nic kubeconfig` writes a working kubeconfig and `kubectl get nodes` returns Ready nodes.
- [ ] `nic destroy` tears down all NIC-tagged resources, including the auto-created `MC_*` RG.
- [ ] Registry-pinned shim resolves: `tofu init` in a clean directory copying the templates pulls `nebari-dev/aks-cluster/azurerm` v0.1.0.
- [ ] `ARCHITECTURE.md`, `WALKTHROUGH.md`, `CLAUDE.md` no longer describe Azure as a stub.
- [ ] Branch ready for PR; CI green.
