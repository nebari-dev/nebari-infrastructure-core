# OpenShift (ROSA HCP) Cluster Provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dual-mode `openshift` cluster provider to NIC that provisions ROSA HCP via OpenTofu (and can also target an existing OpenShift cluster), then deploys Nebari's stack onto it.

**Architecture:** Mirror the `aws` provider's structure (`pkg/providers/cluster/openshift/`), swapping the IaC layer to the `terraform-redhat/rosa-hcp` + `rhcs` Terraform provider while reusing NIC's S3 state-backend and OpenTofu-exec patterns. OpenShift-specific work concentrates in SecurityContextConstraints (SCC) bootstrapping and OpenShift-aware `InfraSettings`. Sequence: prove the ROSA flow manually (Phase A), then encode it into the provider (Phase B).

**Tech Stack:** Go 1.26, OpenTofu/terraform-exec, `rhcs` Terraform provider, `terraform-redhat/rosa-hcp` module, `rosa` + `oc` CLIs, AWS SDK v2, OpenShift `security.openshift.io/v1` API.

## Global Constraints

- Provider name: `openshift` (exact, lowercase). Config key: `cluster.openshift:`.
- Implements `pkg/providers/cluster.Provider` unchanged; CLI/registry are the only touch points outside the package.
- Region default `us-east-1`; single-AZ test cluster; 2× `m5.xlarge` workers.
- Storage default `gp3-csi`; Longhorn opt-in only.
- Required env for provision mode: AWS creds **+ `RHCS_TOKEN`** (OCM offline token).
- Keep Nebari's Envoy Gateway + cert-manager + Keycloak stack (no Routes/OAuth swap).
- Every provisioned cluster has a one-command teardown; always `nic destroy` / `rosa delete` after a session unless explicitly kept.
- Follow existing `_test.go` patterns; Go tests via `go test ./...`.

---

## Phase A — Manual ROSA HCP provision (executable now, drives Phase B)

### Task A1: Install tooling + teardown safety net

**Files:**
- Create: `hack/rosa/teardown.sh` (workspace-level helper, not committed to NIC core unless desired)

- [ ] **Step 1: Install rosa + oc**

Run:
```bash
brew install rosa-cli openshift-cli
rosa version && oc version --client
```
Expected: both print versions.

- [ ] **Step 2: Write teardown script (destroy safety net)**

```bash
#!/usr/bin/env bash
# hack/rosa/teardown.sh — one-command ROSA HCP teardown
set -euo pipefail
CLUSTER="${1:-nebari-ocp-poc}"
echo "Deleting ROSA cluster: $CLUSTER"
rosa delete cluster --cluster "$CLUSTER" --yes --watch
rosa delete operator-roles --cluster "$CLUSTER" --mode auto --yes || true
rosa delete oidc-provider --cluster "$CLUSTER" --mode auto --yes || true
echo "Teardown complete."
```

- [ ] **Step 3: Make executable + verify**

Run: `chmod +x hack/rosa/teardown.sh && bash -n hack/rosa/teardown.sh`
Expected: no syntax errors.

### Task A2: Authenticate + verify ROSA prerequisites

- [ ] **Step 1: Log in to OCM** — `rosa login --token="$RHCS_TOKEN"` then `rosa whoami`. Expected: shows OCM + AWS account.
- [ ] **Step 2: Enable + verify** — `rosa verify quota --region us-east-1` and `rosa verify permissions`. Expected: PASS (enable ROSA in AWS console / `rosa init` if not).
- [ ] **Step 3: Account roles** — `rosa create account-roles --hosted-cp --mode auto --yes`. Expected: account roles created.

### Task A3: Provision the cluster

- [ ] **Step 1: OIDC config** — `rosa create oidc-config --mode auto --yes`. Capture the returned OIDC config ID.
- [ ] **Step 2: Create cluster**

Run:
```bash
rosa create cluster --hosted-cp --sts --mode auto --yes \
  --cluster-name nebari-ocp-poc \
  --region us-east-1 \
  --availability-zones us-east-1a \
  --compute-machine-type m5.xlarge --replicas 2 \
  --oidc-config-id <ID-from-step-1>
```
Expected: cluster enters `installing`.

- [ ] **Step 3: Wait for ready** — `rosa logs install --cluster nebari-ocp-poc --watch` then `rosa describe cluster --cluster nebari-ocp-poc`. Expected: state `ready` (~10–15 min).

### Task A4: Get a kubeconfig NIC can consume

- [ ] **Step 1: Admin** — `rosa create admin --cluster nebari-ocp-poc`. Capture the `oc login` command.
- [ ] **Step 2: Log in + export** — `oc login <api-url> -u cluster-admin -p <pw>` then `KUBECONFIG=./nebari-ocp.kubeconfig oc login ...`. Expected: a standalone kubeconfig file.
- [ ] **Step 3: Verify OpenShift API** — `oc api-resources | grep securitycontextconstraints`. Expected: SCC resource present (confirms it's OpenShift).
- [ ] **Step 4: Record the flow** — append the exact commands + the OIDC/role ARNs to `docs/superpowers/plans/phase-a-runlog.md`. This is the source material for Phase B tofu templates.

### Task A5: Validate Nebari deploys (existing-mode smoke test)

- [ ] **Step 1:** Write `examples/openshift-existing-smoke.yaml` pointing `cluster.existing` at the kubeconfig from A4. (Uses the stock `existing` provider to validate the cluster before the new provider exists.)
- [ ] **Step 2:** `nic validate -f examples/openshift-existing-smoke.yaml`. Expected: passes.
- [ ] **Step 3:** Attempt `nic deploy` and record which foundational pods fail on SCC. **This list defines the SCC bindings Task B5 must create.**

---

## Phase B — `openshift` provider in NIC

### Task B1: Config type + parsing

**Files:**
- Create: `pkg/providers/cluster/openshift/config.go`
- Test: `pkg/providers/cluster/openshift/config_test.go`

**Interfaces:**
- Produces: `type Config struct{...}`; `func (c *Config) Mode() string`; `func (c *Config) StorageClassOrDefault() string`; `func (c *Config) LonghornEnabled() bool`.

- [ ] **Step 1: Write failing test**

```go
package openshift

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestConfigDefaults(t *testing.T) {
	raw := map[string]any{"mode": "provision", "region": "us-east-1"}
	var c Config
	if err := config.UnmarshalProviderConfig(context.Background(), raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Mode() != "provision" {
		t.Errorf("mode = %q, want provision", c.Mode())
	}
	if got := c.StorageClassOrDefault(); got != "gp3-csi" {
		t.Errorf("storage = %q, want gp3-csi", got)
	}
	if c.LonghornEnabled() {
		t.Error("longhorn should default off")
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`Config` undefined). Run: `go test ./pkg/providers/cluster/openshift/ -run TestConfigDefaults`.

- [ ] **Step 3: Implement config.go**

```go
package openshift

const (
	ModeProvision      = "provision"
	ModeExisting       = "existing"
	defaultStorageClass = "gp3-csi"
)

type Compute struct {
	InstanceType string `yaml:"instance_type" json:"instance_type"`
	Replicas     int    `yaml:"replicas" json:"replicas"`
}

type LonghornConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type Config struct {
	ModeField         string         `yaml:"mode" json:"mode"`
	Region            string         `yaml:"region" json:"region"`
	OpenShiftVersion  string         `yaml:"openshift_version" json:"openshift_version"`
	AvailabilityZones []string       `yaml:"availability_zones" json:"availability_zones"`
	Compute           Compute        `yaml:"compute" json:"compute"`
	MachineCIDR       string         `yaml:"machine_cidr" json:"machine_cidr"`
	StateBucket       string         `yaml:"state_bucket" json:"state_bucket"`
	Kubeconfig        string         `yaml:"kubeconfig" json:"kubeconfig"`
	Context           string         `yaml:"context" json:"context"`
	StorageClass      string         `yaml:"storage_class" json:"storage_class"`
	Longhorn          LonghornConfig `yaml:"longhorn" json:"longhorn"`
}

func (c *Config) Mode() string {
	if c.ModeField == "" {
		return ModeProvision
	}
	return c.ModeField
}

func (c *Config) StorageClassOrDefault() string {
	if c.StorageClass == "" {
		return defaultStorageClass
	}
	return c.StorageClass
}

func (c *Config) LonghornEnabled() bool { return c.Longhorn.Enabled }
```

- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `git add pkg/providers/cluster/openshift/config*.go && git commit -m "feat(openshift): config type with mode + storage defaults"`.

### Task B2: Provider skeleton + Name + registry wiring

**Files:**
- Create: `pkg/providers/cluster/openshift/provider.go`
- Modify: `pkg/nic/registry.go` (add import + `Register(ctx, "openshift", openshift.NewProvider())`)
- Test: `pkg/providers/cluster/openshift/provider_test.go`

**Interfaces:**
- Produces: `func NewProvider() *Provider`; `Provider` implements `cluster.Provider`; `const ProviderName = "openshift"`.

- [ ] **Step 1: Test** — assert `NewProvider().Name() == "openshift"` and that `*Provider` satisfies `cluster.Provider` (`var _ cluster.Provider = (*Provider)(nil)`).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** stub `Provider` with all interface methods returning `fmt.Errorf("not implemented")` except `Name()`. Mirror `aws/provider.go` struct shape (kubeconfig cache).
- [ ] **Step 4: Wire registry** — add to `defaultRegistry` in `pkg/nic/registry.go` exactly like the `existing` registration.
- [ ] **Step 5: Run — expect PASS** (`go test ./...` compiles + provider test passes).
- [ ] **Step 6: Commit** — `feat(openshift): provider skeleton + registry wiring`.

### Task B3: InfraSettings

**Files:** Modify `pkg/providers/cluster/openshift/provider.go`; Test `provider_test.go`.

**Interfaces:** Produces `func (p *Provider) InfraSettings(*config.ClusterConfig) cluster.InfraSettings`.

- [ ] **Step 1: Test** — given config with default storage, assert `StorageClass=="gp3-csi"`, `NeedsMetalLB==false`, `SupportsLocalGitOps==false`, and LB annotations map is non-nil.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** — unmarshal config, return `cluster.InfraSettings{StorageClass: cfg.StorageClassOrDefault(), NeedsMetalLB:false, SupportsLocalGitOps:false, LoadBalancerAnnotations: map[string]string{"service.beta.kubernetes.io/aws-load-balancer-type":"external","service.beta.kubernetes.io/aws-load-balancer-nlb-target-type":"ip"}}`.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(openshift): InfraSettings (gp3-csi, AWS NLB annotations)`.

### Task B4: Validate (both modes)

**Files:** Modify `provider.go`; Test `provider_test.go`.

**Interfaces:** Produces `Validate(ctx, projectName, *config.ClusterConfig) error`.

- [ ] **Step 1: Test** — (a) `existing` mode with missing `context` → error; (b) `provision` mode with empty `region` → error; (c) valid provision config (region set) with `RHCS_TOKEN` set in env → nil. Use `t.Setenv("RHCS_TOKEN","x")`.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** — branch on `cfg.Mode()`: existing → reuse `kubeconfig.ValidateContext` logic (copy the small helper from `existing`); provision → require `Region != ""`, require `os.Getenv("RHCS_TOKEN") != ""`, probe AWS creds via `awsconfig.LoadDefaultConfig` like `aws.Validate`.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(openshift): Validate for provision + existing modes`.

### Task B5: SCC bootstrap

**Files:** Create `pkg/providers/cluster/openshift/scc.go`; Test `scc_test.go`.

**Interfaces:** Produces `func applySCCBindings(ctx context.Context, kubeconfig []byte, namespaces []string) error`; `func sccBindingManifests(namespaces []string) []rbacv1.RoleBinding` (pure, unit-testable).

- [ ] **Step 1: Test** — `sccBindingManifests([]string{"nebari"})` returns bindings referencing the SCC ClusterRole (e.g. `system:openshift:scc:nonroot-v2`) for the expected service accounts in namespace `nebari`. Assert names/namespaces. (Exact SA + SCC list comes from Phase A Task A5 Step 3.)
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** — build `RoleBinding` objects binding `ClusterRole/system:openshift:scc:<scc>` to the foundational service accounts; `applySCCBindings` connects with the client pattern from `pkg/storage/longhorn` and applies them idempotently.
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(openshift): SCC RoleBinding bootstrap for foundational namespaces`.

### Task B6: OpenTofu ROSA templates

**Files:** Create `pkg/providers/cluster/openshift/templates/{provider,variables,main,outputs,backend}.tf`; `tofu.go` (`//go:embed templates/*`); Test `tofu_test.go`.

**Interfaces:** Produces `var tofuTemplates embed.FS`; `func (c *Config) toTFVars(projectName string) (map[string]any, error)`.

- [ ] **Step 1: Test** — `toTFVars("nebari-ocp-poc")` yields keys `cluster_name`, `region`, `compute_machine_type`, `replicas`, `availability_zones` with values from config.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement templates** — `main.tf` uses `terraform-redhat/rosa-hcp/rhcs` module with vars; `provider.tf` declares `rhcs` (token from `RHCS_TOKEN`) + `aws`; `backend.tf` S3 (overridden at init like aws); `outputs.tf` exposes `cluster_id`, `api_url`, `console_url`. **Fill the module block from the Phase A runlog (Task A4 Step 4).**
- [ ] **Step 4: Implement toTFVars** in `tofu.go`.
- [ ] **Step 5: Run — expect PASS** (Go test) and `tofu validate` in the templates dir.
- [ ] **Step 6: Commit** — `feat(openshift): OpenTofu ROSA HCP templates + tfvars`.

### Task B7: GetKubeconfig

**Files:** Create `pkg/providers/cluster/openshift/kubeconfig.go`; Test `kubeconfig_test.go`.

**Interfaces:** Produces `GetKubeconfig(ctx, projectName, *config.ClusterConfig) ([]byte, error)`.

- [ ] **Step 1: Test** — existing mode: given a temp kubeconfig + context, returns filtered bytes (reuse `existing` test approach).
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement** — existing mode reuses `kubeconfig.LoadFromPath`/`FilterByContext`/`WriteBytes`; provision mode reads the api_url from tofu outputs and builds a kubeconfig via the rosa/OCM admin credential captured at apply (cache in-memory like aws).
- [ ] **Step 4: Run — expect PASS.**
- [ ] **Step 5: Commit** — `feat(openshift): GetKubeconfig for both modes`.

### Task B8: Deploy + Destroy + Summary

**Files:** Modify `provider.go`; Create `state.go` (reuse/share aws S3 helpers); Test `provider_test.go`.

**Interfaces:** Produces `Deploy(...)`, `Destroy(...)`, `Summary(...)`.

- [ ] **Step 1: Test** — DryRun Deploy in existing mode is a no-op returning nil; Summary returns `Mode` + `Storage Class` keys.
- [ ] **Step 2: Run — expect FAIL.**
- [ ] **Step 3: Implement Deploy** — provision: ensure state bucket → `tofu.Setup`/Init(backend)/Apply → GetKubeconfig → `applySCCBindings` → optional Longhorn. existing: `applySCCBindings` → optional Longhorn. DryRun → tofu plan / log, no SCC writes.
- [ ] **Step 4: Implement Destroy** — provision: tofu destroy → drop state bucket; existing: no-op. Implement Summary.
- [ ] **Step 5: Run — expect PASS.**
- [ ] **Step 6: Commit** — `feat(openshift): Deploy/Destroy/Summary wiring`.

### Task B9: Example config + docs

**Files:** Create `examples/openshift-config.yaml`; Modify `README.md` (provider list) + `docs/cli-reference.md` if it enumerates providers.

- [ ] **Step 1:** Write `examples/openshift-config.yaml` matching the spec's schema (both modes shown, provision active).
- [ ] **Step 2:** Add `openshift` to any provider list in README/docs.
- [ ] **Step 3:** `nic validate -f examples/openshift-config.yaml` (provision mode, with env set). Expected: passes.
- [ ] **Step 4: Commit** — `docs(openshift): example config + provider listing`.

### Task B10: End-to-end against ROSA

- [ ] **Step 1:** `nic deploy -f examples/openshift-config.yaml` (provision mode) — stands up a fresh ROSA HCP cluster and deploys Nebari.
- [ ] **Step 2:** Verify foundational pods healthy (`oc get pods -A`), gateway gets an LB address, Keycloak/landing reachable.
- [ ] **Step 3:** `nic destroy -f examples/openshift-config.yaml` — verify cluster + state bucket gone (`rosa list clusters`). Backstop: `hack/rosa/teardown.sh`.
- [ ] **Step 4: Commit** — `test(openshift): e2e provision+deploy+destroy validated`.

---

## Self-Review

**Spec coverage:** dual-mode ✅ (B1/B4/B8), provision via OpenTofu+ROSA ✅ (B6/B8), existing mode ✅ (B7/B8), keep Nebari stack ✅ (InfraSettings B3, no Routes/OAuth), CSI default + Longhorn opt-in ✅ (B1/B3), SCC bootstrap ✅ (B5), registry/example ✅ (B2/B9), manual-first sequencing ✅ (Phase A → B6/B5 fed by A4/A5), teardown safety ✅ (A1, B10). e2e ✅ (B10).

**Placeholders:** Two intentional, both gated on Phase A outputs and called out explicitly — the ROSA module block (B6 Step 3, from A4 runlog) and the exact SCC/SA list (B5 Step 1, from A5 Step 3). These cannot be written correctly before the manual flow runs; that is the whole reason for the manual-first sequencing.

**Type consistency:** `Config`, `Mode()`, `StorageClassOrDefault()`, `LonghornEnabled()`, `toTFVars`, `applySCCBindings`, `sccBindingManifests` referenced consistently across B1–B8.
