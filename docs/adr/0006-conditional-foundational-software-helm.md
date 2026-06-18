# ADR-0006: Conditional Foundational Software via Provider-Driven Helm Installs

## Status

Proposed (2026-06-03)

Refines the "GitOps for software" principle in [ADR-0001](0001-git-provider-for-gitops-bootstrap.md), `AGENTS.md` §Core Architecture Principles #5, and the design doc §4.5 for the specific case of *conditional, provider/cluster-dependent* foundational software. The implementation of the shared interface described here is tracked in #349; this ADR records the decision and the target design.

## Date

2026-06-03

## Context

NIC bootstraps two kinds of foundational software onto a freshly-provisioned cluster:

- **Unconditional software** — installed on every cluster regardless of provider or cluster shape: cert-manager, Keycloak, Envoy Gateway, PostgreSQL, the Nebari operator, the landing page. These are rendered as ArgoCD `Application` templates into the GitOps repo by `pkg/argocd/writer.go` and reconciled by the root App-of-Apps. This is the documented "GitOps for software" path and it works well.

- **Conditional software** — installed only when a provider- or cluster-specific condition holds: MetalLB (only when the provider has no native load balancer), Longhorn (only without managed block storage), the AWS Load Balancer Controller (AWS only), the cluster-autoscaler (#352), EFS StorageClass (AWS, EFS enabled), and now the NVIDIA GPU Operator (only when the cluster has GPU nodes — #348).

The conditional set is installed **three inconsistent ways** today:

1. **ArgoCD app + a skip predicate in the writer** — MetalLB keys off `InfraSettings.NeedsMetalLB`; `writer.go` renders or skips `apps/metallb.yaml` accordingly.
2. **Imperative Helm in the provider's `Deploy()`** — Longhorn ([ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md)), the AWS Load Balancer Controller, the cluster-autoscaler (#352), each hand-rolled with its own history-check → install/upgrade logic, plus best-effort uninstall in `Destroy()`.
3. **Tofu / node-group level** — not foundational *software* but adjacent (taints, labels).

There is no rule for which mechanism a new conditional component should use, so each addition re-litigates it. The GPU operator (#348) surfaced this directly: it was first implemented as an ArgoCD app gated through the `NeedsMetalLB`-style writer mechanism, then moved to imperative Helm — and a documented-architecture review flagged the move as violating the blanket "GitOps for software" rule, because that rule does not distinguish unconditional from conditional software.

The GitOps-app-with-`InfraSettings`-flag approach does not scale for conditional software. Each new conditional app requires a new `InfraSettings` field *and* a new skip predicate in `pkg/argocd/writer.go`, which leaks provider- and cluster-specific knowledge into the provider-agnostic GitOps render layer. The decision to install — and the values to install with — frequently depend on state the provider computes (GPU nodes present, existing-vs-cloud, region, VPC ID, node-group-derived toggles, chart versions), which is awkward to thread through templates and natural to express in Go.

## Decision Drivers

- The install/remove decision for conditional software depends on provider + live-cluster state the provider already computes; it should not leak into `pkg/argocd` or `InfraSettings`.
- Several conditional components need ordered, idempotent lifecycle relative to infrastructure (e.g. Longhorn must uninstall before `tofu destroy` to avoid CSI finalizers blocking node-group deletion — ADR-0002 §Destroy Flow).
- The imperative-Helm logic is already duplicated across three components and growing; it wants one home.
- The CLI and GitOps layers must stay free of provider-name branching (`AGENTS.md` §Abstraction Boundaries).
- Unconditional foundational software is well served by GitOps and should not change.

## Considered Options

1. **GitOps ArgoCD app gated by an `InfraSettings` flag** (status quo for MetalLB) — extend the `NeedsMetalLB` pattern to every conditional app.
2. **Per-component hand-rolled imperative Helm** (status quo for Longhorn / LBC / autoscaler) — keep adding bespoke install/uninstall files per component.
3. **A shared Helm install interface, driven by per-provider conditional-software listing** — providers enumerate the conditional charts they want for a given cluster and a shared installer reconciles them.

## Decision Outcome

Chosen option: **Option 3** — provider/cluster-conditional foundational software is installed imperatively via Helm, driven by the provider, behind a shared interface. Unconditional foundational software stays in GitOps/ArgoCD unchanged.

The line is: **conditional on provider or cluster state → provider-driven Helm; installed on every cluster → GitOps.** MetalLB is currently on the wrong side of this line and is expected to migrate (it is not correctly shaped today). The GPU operator (#348) and the cluster-autoscaler (#352) are correct instances of this decision; their hand-rolled implementations are folded into the shared interface as part of #349.

### Consequences

**Good:**
- One home for the Helm history-check/install/upgrade/uninstall logic currently duplicated in `cluster_autoscaler.go`, `aws_load_balancer_controller.go`, and `gpu_operator.go`.
- Each provider owns its own conditional set and the values it installs with; no provider knowledge leaks into `pkg/argocd` or `InfraSettings`.
- Standardized, ordered `Deploy`/`Destroy` (install/uninstall) lifecycle — including the before-`tofu destroy` ordering Longhorn needs.
- The stable, unconditional foundational core stays declarative in GitOps.

**Bad:**
- Two mechanisms for "foundational software" (GitOps for unconditional, Helm for conditional) that contributors must learn and keep on the right side of the line.
- Conditional apps are no longer visible as ArgoCD `Application`s in the GitOps repo / ArgoCD UI; their state lives in Helm releases and NIC logs.
- Reconcile and prune-on-disable correctness is on NIC, not on ArgoCD's automated prune + finalizers.

## Options Detail

### Option 1: GitOps ArgoCD app + `InfraSettings` flag

Render a per-app template, gate it with a boolean on `InfraSettings` and a skip predicate in `writer.go`.

**Pros:**
- Declarative; visible in the GitOps repo and ArgoCD UI; ArgoCD owns prune/self-heal.
- One mechanism for all foundational software.

**Cons:**
- Every conditional app adds an `InfraSettings` field + a `writer.go` predicate; provider/cluster knowledge accretes in the provider-agnostic render layer.
- Values that depend on computed provider/cluster state are awkward to thread through templates.
- No clean hook for teardown ordered relative to `tofu destroy`.

### Option 2: Per-component hand-rolled imperative Helm

Keep writing a bespoke `install<Component>` / `uninstall<Component>` file per component.

**Pros:**
- Full control per component; matches what Longhorn/LBC/autoscaler do now.

**Cons:**
- The same Helm scaffolding is copied per component (already three copies).
- No standardized lifecycle contract; each component reinvents reconcile/uninstall/`--force` handling.

### Option 3: Shared Helm interface + per-provider conditional listing (Chosen)

A shared installer encapsulates the Helm lifecycle; each provider declares the conditional charts it wants for a given cluster; a reconcile loop installs the listed set and uninstalls previously-managed releases no longer listed.

**Pros:**
- De-duplicates the Helm logic; standardized lifecycle.
- Provider owns its conditional set and values; no leakage into shared layers.
- Natural place for ordered teardown and idempotent reconcile.

**Cons:**
- Loses ArgoCD visibility/prune for conditional apps.
- Introduces a second foundational-software mechanism alongside GitOps.

## Detailed Design

The shared installer (the standardized deploy/destroy contract), in a shared package (e.g. `pkg/helm`):

```go
type Chart struct {
    Repo, Name, Version    string
    ReleaseName, Namespace string
    Values                 map[string]any
    Wait                   bool
    Timeout                time.Duration
}

type helmInstaller interface {
    // Install performs a history-check, then installs or upgrades the release.
    Install(ctx context.Context, kubeconfig []byte, c Chart) error
    // Uninstall removes the release if present; a missing release is not an error.
    Uninstall(ctx context.Context, kubeconfig []byte, release, namespace string) error
}
```

Each provider declares the conditional charts it wants, computed from its own config plus live cluster state:

```go
// Implemented per provider. Returns the conditional foundational charts for
// this cluster, derived from provider config + cluster state (GPU nodes
// present, existing-vs-cloud, storage availability, etc.).
type ConditionalSoftwareProvider interface {
    ConditionalCharts(ctx context.Context, clusterConfig *config.ClusterConfig, kubeconfig []byte) ([]Chart, error)
}
```

A shared reconcile step then:
1. installs every chart `ConditionalCharts` returns (idempotent via the installer's history-check), and
2. uninstalls any chart NIC previously managed for this cluster that is no longer in the returned set (so disabling a condition removes the software).

This is invoked from the provider's `Deploy()` (reconcile) and `Destroy()` (uninstall managed releases before `tofu destroy`), preserving the ordering Longhorn requires.

**Scope and migration.** This ADR records the decision and the target shape. The shared `helmInstaller` / `ConditionalCharts` implementation is #349. The GPU operator (#348) and cluster-autoscaler (#352) are implemented as hand-rolled imperative installs that match this decision and are folded into the shared interface in #349. MetalLB migrates from the ArgoCD-skip mechanism to this interface as part of the same work. Unconditional foundational software (cert-manager, Keycloak, Envoy Gateway, PostgreSQL, the Nebari operator) is out of scope and stays in GitOps.

## Links

- [ADR-0001: Git Provider for GitOps Bootstrap](0001-git-provider-for-gitops-bootstrap.md) — establishes the GitOps-for-software principle this ADR refines for conditional software.
- [ADR-0002: Longhorn Distributed Block Storage for AWS](0002-longhorn-distributed-block-storage-for-aws.md) — the existing imperative-Helm precedent and the before-`tofu destroy` teardown ordering.
- #349 — implementation of the shared `helmInstaller` + `ConditionalCharts` interface (this ADR's design); MetalLB migration.
- #348 — NVIDIA GPU Operator install; the first new conditional component under this decision.
- #352 — cluster-autoscaler; another instance folded into the shared interface.
