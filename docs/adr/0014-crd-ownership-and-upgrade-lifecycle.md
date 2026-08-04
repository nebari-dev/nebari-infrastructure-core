# ADR-0014: CRD Ownership and Upgrade Lifecycle Across ArgoCD and Helm

## Status

Proposed (2026-08-04)

Refines [ADR-0006](0006-conditional-foundational-software-helm.md) by specifying
what its shared installer must do about CRDs, and records the CRD-ownership
invariant that [ADR-0011](0011-gateway-listener-ownership.md) and [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) both rely
on but neither states. Implementation lands via [#349](https://github.com/nebari-dev/nebari-infrastructure-core/issues/349).

## Date

2026-08-04

## Context

NIC delivers CRDs through two mechanisms that behave oppositely on upgrade, and
nothing records which mechanism owns a given CRD group.

| Mechanism | Components | CRDs installed | CRDs **upgraded** |
|---|---|---|---|
| ArgoCD app | Envoy Gateway, cert-manager, CloudNativePG, trust-manager | rendered with `--include-crds` (no `skipCrds` anywhere in the tree) | **yes** — synced as ordinary manifests |
| Imperative Helm ([ADR-0006](0006-conditional-foundational-software-helm.md)) | AWS LBC, Longhorn, cluster-autoscaler, GPU operator, and the Argo CD bootstrap itself | `action.Install` applies `crds/` | **never** |

### Helm's behaviour, and Helm's position

Verified against the pinned `helm.sh/helm/v3 v3.21.1`:

- `--skip-crds` is the only CRD flag and it is an opt-*out*. On `helm upgrade` it
  takes effect only when `--install` falls through to an install because the
  release does not exist (`cmd/helm/upgrade.go:137`). `pkg/action/upgrade.go`
  contains no CRD application path at all.
- Even on install, `installCRDs` uses `KubeClient.Create` and skips anything
  already present with a log line (`pkg/action/install.go:161-180`). A chart
  whose CRDs already exist never gets them updated, on install or upgrade.

This is deliberate upstream, not a gap awaiting a patch. Helm's
[CRD best practices](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/)
state: "There is no support at this time for upgrading or deleting CRDs using
Helm. This was an explicit decision after much community discussion due to the
danger for unintentional data loss." Helm also notes there is "currently no
community consensus around how to handle CRDs and their lifecycle," and its
recommended pattern for operators who need lifecycle is to separate CRDs into
their own chart, installed independently.

So "make the shared installer apply `crds/`" is not the obvious fix. It is one
option among several, and it accepts a risk Helm declined on purpose.

### Why the risk is real

A CRD's `status.storedVersions` records every version its objects have been
persisted in, and a version cannot be dropped from `spec.versions` while it
remains in `storedVersions`. Existing custom resources must be re-encoded
(storage-version migration) first. An apply that removes a served version is
therefore either rejected by the API server or leaves objects unreadable.

Risk also scales per group with how many CRs exist. NIC has substantial
`longhorn.io`, `postgresql.cnpg.io` and `gateway.networking.k8s.io` objects, and
essentially no `gateway.k8s.aws` objects while [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) keeps LBC's Gateway API
controllers disabled. A single global rule is therefore either too paranoid for
one group or too reckless for another.

### Two live pressures

1. **AWS LBC 3.5.0** adds `v1` as the storage version to all three
   `gateway.k8s.aws` CRDs and demotes `v1beta1` — a 1,271-line diff in
   `crds/gateway-crds.yaml`. On the imperative path an in-place bump ships a
   controller reading `v1` against CRDs serving only `v1beta1`. [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) pins 3.4.3
   partly to sidestep this: 3.4.3's `crds/` are byte-identical to 3.4.2, so the
   gap is not exercised. That is a deferral, not a strategy.
2. **Gateway API has two would-be consumers with diverging floors.** Envoy
   Gateway 1.6.2 ships bundle v1.4.1; 1.8.2 ([#496](https://github.com/nebari-dev/nebari-infrastructure-core/pull/496)) ships v1.5.1 and adds
   `listenersets`. LBC 3.5.0 requires v1.6.0 and auto-disables NLB-gateway below
   it. [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) makes this moot by pinning LBC's Gateway API feature gates off, but
   nothing records that as an invariant, so the next person to enable them
   inherits unsolved ownership contention. LBC compounds it by probing for
   Gateway API CRDs only at startup, so its behaviour depends on whether a pod
   happened to restart after Envoy Gateway synced ([#383](https://github.com/nebari-dev/nebari-infrastructure-core/issues/383), [#417](https://github.com/nebari-dev/nebari-infrastructure-core/issues/417)).

### Two hazards no current document addresses

- **Deleting a CRD cascades to every CR of that type.** NIC's ArgoCD apps carry
  `prune: true`, `selfHeal: true` and `resources-finalizer.argocd.argoproj.io`
  (e.g. `pkg/argocd/templates/apps/envoy-gateway.yaml`). Removing or renaming
  the `envoy-gateway` app would prune the Gateway API CRDs and every Gateway,
  HTTPRoute and ListenerSet on the cluster. ADR-0006's reconcile loop
  ("uninstalls managed releases no longer listed") needs an explicit CRD
  carve-out before it is implemented.
- **Upstream is already restructuring around this.** Envoy Gateway 1.8.2 moved
  its CRDs out of `crds/` into a `charts/crds` subchart with a `safe-upgrades`
  ValidatingAdmissionPolicy, specifically so Helm and ArgoCD would upgrade them.
  We cannot restructure third-party charts, so any reconcile must be on our side.

This spans the GitOps layer, the provider-driven Helm layer and third-party
chart pins, so it is a structural decision rather than a bug fix.

## Decision Drivers

- Silent divergence is the concrete harm today: a controller upgrades, its CRDs
  do not, and nothing reports it until a controller misbehaves in production.
- Unintentional data loss is worse than drift. Helm's stated reasoning applies to
  us and should not be overridden casually.
- CRD risk is per-group, driven by whether CRs exist and by what the schema
  change actually does, so a uniform global rule mis-fits.
- Operators need to learn that a bump requires manual action *before* running it,
  not after a five-minute Helm timeout.
- Two mechanisms already exist ([ADR-0006](0006-conditional-foundational-software-helm.md));
  this ADR must cover both rather than assume everything converges on one.
- ADR-0006's shared installer is still unimplemented ([#349](https://github.com/nebari-dev/nebari-infrastructure-core/issues/349)), so a requirement
  placed now costs one design line instead of four retrofits.

## Considered Options

1. Auto-apply CRDs in the shared installer
2. Never auto-apply; detect drift and fail closed
3. Hoist CRDs into a NIC-owned artifact
4. Gate CRD-changing chart bumps
5. Policy per CRD group

## Decision Outcome

Chosen option: **Option 2 + Option 4 as the default, Option 3 where NIC already
effectively vendors the CRDs, and Option 1 never blanket-enabled.**

The reasoning: the harm today is that divergence is *silent*. Detection removes
the silence at a fraction of the risk auto-apply accepts, and it does not require
NIC to guess whether a given schema change is safe. Gating CRD-changing bumps
turns the remaining cases into an explicit, documented migration rather than an
accident. Where NIC would have to vendor CRDs anyway to control their version
(Gateway API being the live example), a NIC-owned wave-0 ArgoCD app is both
Helm's own recommended pattern and the mechanism that already upgrades CRDs
correctly.

This ADR also records five specific rules:

1. **Ownership register.** Every CRD group has exactly one owner and one
   delivery mechanism. No group has two writers. Proposed starting register:

   | CRD group | Owner | Mechanism |
   |---|---|---|
   | `gateway.networking.k8s.io` | `envoy-gateway` app | ArgoCD |
   | `gateway.envoyproxy.io` | `envoy-gateway` app | ArgoCD |
   | `cert-manager.io` | `cert-manager` app | ArgoCD |
   | `postgresql.cnpg.io` | `cloudnative-pg` app | ArgoCD |
   | `trust.cert-manager.io` | `trust-manager` app | ArgoCD |
   | `longhorn.io` | AWS/Hetzner provider | imperative Helm |
   | `elbv2.k8s.aws` | AWS provider | imperative Helm |
   | `gateway.k8s.aws` | AWS provider | imperative Helm (unused; gates off) |
   | `argoproj.io` | bootstrap | imperative Helm |

2. **NIC does not auto-upgrade CRDs.** The shared installer detects divergence
   between a chart's `crds/` and the live cluster and reports it; it does not
   silently apply. Where it must apply (a group NIC owns outright under
   Option 3), it does so through the GitOps path, not through Helm.

3. **Automation never deletes a CRD.** Excluded from `Uninstall`, from the
   reconcile loop's "no longer listed" pruning, and from ArgoCD pruning
   (`Prune=false` on CRD resources). CRD removal is a documented manual operator
   action.

4. **Shared-group contention rule.** When two controllers can consume the same
   CRD group, one is designated owner at `max(required bundle version)` and the
   other's consumption is disabled by configuration. Concretely: Envoy Gateway
   owns Gateway API; LBC's `ALBGatewayAPI`, `NLBGatewayAPI` and
   `GatewayListenerSet` gates stay off ([#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532)). Enabling them is a design change
   requiring a follow-up ADR that must address hoisting Gateway API CRDs to a
   wave-0 app and LBC's startup-only CRD probe.

5. **CRD version floors are recorded** next to the chart pin they constrain, so a
   chart bump cannot silently outrun the CRDs on disk.

### Consequences

**Good:**

- Divergence becomes loud instead of silent, which is the actual failure mode
  operators hit today.
- NIC does not take on responsibility for judging whether a third-party schema
  change is destructive.
- Aligns with Helm's documented position rather than working against it, so
  future Helm behaviour changes are unlikely to break us.
- The Gateway API invariant becomes checkable, and the "why can't I enable
  `ALBGatewayAPI`?" conversation has a documented answer.
- One rule covers both mechanisms, so new foundational software has a CRD story
  before it ships rather than after.

**Bad:**

- Some CRD upgrades stay manual. A deploy can now refuse to proceed on drift,
  which is a new failure mode operators must understand.
- A runbook is required, and runbooks rot. Without the detection wired into
  `nic deploy` the rule is unenforced.
- Option 3 means vendoring third-party CRDs for any group NIC takes over, with
  the tracking burden that implies on every upstream bump.
- CRDs that are never pruned accumulate on long-lived clusters, including for
  software that has been removed.
- Per-group policy is more to hold in your head than a single global rule.

## Options Detail

### Option 1: Auto-apply CRDs in the shared installer

Server-side-apply `chrt.CRDObjects()` before `action.Upgrade`, with
`SkipCRDs = true` on the install path so exactly one code path owns CRDs rather
than racing Helm's `Create`-and-skip.

**Pros:**
- No drift, no manual step, no runbook to rot.
- One change in one place fixes all imperative call sites at once.
- Keeps the controller and its CRDs versioned together by construction.

**Cons:**
- Directly contradicts Helm's explicit decision and its stated data-loss
  reasoning.
- An additive-looking chart diff can still remove a served version or narrow a
  schema; the installer cannot tell without inspecting every CRD change.
- Blocked by `storedVersions` in exactly the cases that matter, so it would fail
  loudly on real migrations anyway — after already having applied part of the set.

### Option 2: Never auto-apply; detect drift and fail closed

A preflight compares the chart's `crds/` against live CRDs and refuses the deploy
(or warns) with a named operator action when they diverge.

**Pros:**
- Removes the silence, which is the real harm, without accepting the real risk.
- Cheap: a comparison, not a migration engine.
- Runs before any cloud work, so the failure is fast and named rather than a
  five-minute Helm timeout.

**Cons:**
- The upgrade itself stays manual, and someone has to actually do it.
- Requires a documented per-group procedure to be useful.
- Fail-closed on drift can block an otherwise-fine deploy; warn-only can be
  ignored forever. The ADR must pick one.

### Option 3: Hoist CRDs into a NIC-owned artifact

A dedicated wave-0 ArgoCD app (or vendored CRD-only chart) owns the CRDs, taking
them off the imperative path entirely. This is Helm's own recommended Method 2,
and what Envoy Gateway 1.8.2 adopted upstream.

**Pros:**
- CRDs become visible and diffable in GitOps, and ArgoCD already upgrades them.
- Resolves shared-group contention directly: one owner, one pinned bundle
  version, independent of any consumer's chart.
- Matches where upstream is moving.

**Cons:**
- NIC vendors third-party CRDs and must track them on every upstream bump.
- Needs `Prune=false` and finalizer care, or app removal cascades to every CR.
- Splits a chart's CRDs from its templates, so a version mismatch between them
  becomes newly possible.

### Option 4: Gate CRD-changing chart bumps

Refuse a chart version whose `crds/` differ from the installed release's, making
such bumps an explicit opt-in migration with a documented procedure.

**Pros:**
- Turns the dangerous class of bump into a deliberate decision.
- Composes with Option 2: same comparison, applied at pin time rather than
  deploy time.
- Already the de facto practice — [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) pins 3.4.3 for exactly this reason.

**Cons:**
- Slows routine bumps that happen to touch CRDs harmlessly.
- Needs a documented override, and an override with no teeth is decoration.

### Option 5: Policy per CRD group

Auto-apply where the change is provably additive and no CRs exist; require the
explicit path otherwise.

**Pros:**
- Most precise: matches the fact that risk genuinely varies per group.
- Would let low-risk groups (`gateway.k8s.aws` today) upgrade automatically.

**Cons:**
- Most machinery: needs schema diffing, CR counting and a per-group policy table.
- "Provably additive" is the hard part, and getting it wrong is the data-loss
  case Helm warns about.
- Premature until Option 2's detection exists and tells us how often each case
  actually arises.

## Links

- [ADR-0006](0006-conditional-foundational-software-helm.md) — conditional
  foundational software via provider-driven Helm; the mechanism with the gap
  (decided in [#361](https://github.com/nebari-dev/nebari-infrastructure-core/pull/361), scope clarified in [#475](https://github.com/nebari-dev/nebari-infrastructure-core/issues/475))
- [ADR-0011](0011-gateway-listener-ownership.md) — per-app Gateway listener
  ownership, the adjacent decision on the ArgoCD side
- [ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md) — Longhorn, an
  imperative-Helm component with substantial CRs
- [#349](https://github.com/nebari-dev/nebari-infrastructure-core/issues/349) — shared `helmInstaller` / `ConditionalCharts` implementation; where the
  detection requirement lands
- [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) — LBC gates-off pin and the 3.4.3 bump that defers the 3.5.0 CRD migration
- [#496](https://github.com/nebari-dev/nebari-infrastructure-core/pull/496) — Envoy Gateway 1.6.2 → 1.8.2, bundle v1.4.1 → v1.5.1 plus `listenersets`
- [#383](https://github.com/nebari-dev/nebari-infrastructure-core/issues/383), [#417](https://github.com/nebari-dev/nebari-infrastructure-core/issues/417) — LBC's startup-only CRD probe and the resulting crash loop
- [#364](https://github.com/nebari-dev/nebari-infrastructure-core/issues/364) — sibling gap in the same code path: `shouldSkipUpgrade` compares chart
  version only, so values changes are inert on existing clusters
- [#513](https://github.com/nebari-dev/nebari-infrastructure-core/issues/513) — CRD-*defaulted fields* and phantom ArgoCD drift (distinct problem, same
  subject area)
- [#425](https://github.com/nebari-dev/nebari-infrastructure-core/issues/425), [#426](https://github.com/nebari-dev/nebari-infrastructure-core/issues/426) — the upgrade-story placeholders this feeds into
- [Helm — Custom Resource Definitions best practices](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/)
  — the upstream argument against automated CRD upgrade/delete
- [Kubernetes — versions in CustomResourceDefinitions](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/)
  — `storedVersions` and storage-version migration
- Helm source at v3.21.1: `pkg/action/install.go:161-180`,
  `pkg/action/upgrade.go`, `cmd/helm/upgrade.go:267`
