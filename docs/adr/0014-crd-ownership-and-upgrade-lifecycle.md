# ADR-0014: CRD Ownership and Upgrade Lifecycle Across ArgoCD and Helm

## Status

Proposed (2026-08-04) — ownership and deletion rules proposed as settled; the CRD
*upgrade* question and the question of whether conditional foundational software
belongs in ArgoCD are deliberately left open for discussion.

Refines [ADR-0006](0006-conditional-foundational-software-helm.md) by specifying
what its shared installer must do about CRDs, and records the CRD-ownership
invariant that [ADR-0011](0011-gateway-listener-ownership.md) and
[#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) both
rely on but neither states. Implementation lands via
[#349](https://github.com/nebari-dev/nebari-infrastructure-core/issues/349).

## Date

2026-08-04

## Context

### Why CRDs are not ordinary manifests

A CustomResourceDefinition does not deploy a workload — it *extends the cluster's
own API surface*. Applying one registers a new resource kind with the API server,
defines the schema every object of that kind is validated against, and declares
which version those objects are persisted in etcd. It is closer to a schema
migration on a shared database than to a Deployment update.

Three properties follow, and all three are why Helm treats CRDs differently from
everything else in a chart:

- **CRDs are cluster-scoped and singly-owned.** One definition serves every
  namespace and every controller that consumes the group. Two writers means the
  last apply wins, cluster-wide.
- **Deleting a CRD deletes the data.** Removing a CRD garbage-collects every
  custom resource of that kind. Deleting `longhorn.io` takes the Volume objects
  with it; deleting `gateway.networking.k8s.io` takes every Gateway and HTTPRoute.
  There is no undo and no dry run that shows it.
- **A schema change can invalidate objects that already exist.** Narrowing a
  field, removing a served version, or changing the storage version can leave
  persisted objects unreadable, or be rejected outright by the API server.

The practical failure is that a bad CRD apply does not look like a failed deploy.
The apply succeeds; the controller starts; and the damage surfaces later as
resources that cannot be read, reconciled, or recreated. On the ArgoCD path,
where CRDs are applied automatically on every sync, a major CRD upgrade lands
with no gate and no signal — the sync goes green and nobody learns anything until
a controller misbehaves. That combination, high blast radius plus no feedback, is
why this needs a rule rather than a convention.

### The two mechanisms

NIC delivers CRDs through two mechanisms with opposite upgrade behavior, and
does not currently record which mechanism owns a CRD group. Neither behavior was
chosen for CRD reasons — each is the default of its mechanism, and no document
states a position either way.

| Mechanism | Components | Installed | Upgraded |
|---|---|---|---|
| ArgoCD app | Envoy Gateway, cert-manager, CloudNativePG, trust-manager | Rendered with `--include-crds` (no `skipCrds` anywhere in the tree) | Yes, as ordinary manifests |
| Imperative Helm ([ADR-0006](0006-conditional-foundational-software-helm.md)) | AWS LBC, Longhorn, cluster-autoscaler, GPU operator, and the Argo CD bootstrap | `action.Install` applies `crds/` | Never |

### Helm's CRD boundary

Against the pinned `helm.sh/helm/v3 v3.21.1`, `--skip-crds` is Helm's only CRD
flag and is an opt-out. On `helm upgrade` it matters only when `--install`
falls through to an install because the release does not exist
(`cmd/helm/upgrade.go:137`); `pkg/action/upgrade.go` has no CRD application
path. On install, `installCRDs` calls `KubeClient.Create` and skips existing
CRDs and logs the skip (`pkg/action/install.go:161-180`), so a chart's
already-present CRDs are not updated on install or upgrade.

This is deliberate upstream. Helm documents that it does not support upgrading
or deleting CRDs because of the risk of unintentional data loss, notes that
there is no community consensus on CRD lifecycle, and recommends a separate
CRD chart for operators that need lifecycle management.

### Risk and current pressure

`status.storedVersions` records every version in which objects have been
persisted. A version cannot be removed from `spec.versions` until existing
objects are re-encoded through storage-version migration; removing a served
version too early is rejected by the API server or leaves objects unreadable.
Risk therefore scales by CRD group, the number of CRs, and the schema change.
NIC has substantial `longhorn.io`, `postgresql.cnpg.io`, and
`gateway.networking.k8s.io` objects, but essentially no `gateway.k8s.aws`
objects.

| Pressure | Evidence |
|---|---|
| AWS LBC | LBC 3.5.0 adds `v1` as the storage version to all three `gateway.k8s.aws` CRDs and demotes `v1beta1`, a 1,271-line diff in `crds/gateway-crds.yaml`. An imperative bump would ship a controller reading `v1` against CRDs serving only `v1beta1`. [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) pins the latest 3.4.x partly to sidestep this; those `crds/` are unchanged within the 3.4 line, so the gap is deferred rather than solved. (Not yet merged — `defaultLBCChartVersion` is still `3.2.1` in `pkg/providers/cluster/aws/config.go`.) |
| Gateway API | Envoy Gateway 1.6.2 ships bundle v1.4.1; 1.8.2 ([#496](https://github.com/nebari-dev/nebari-infrastructure-core/pull/496)) ships v1.5.1 and adds `listenersets`. LBC 3.5.0 requires v1.6.0 and auto-disables NLB-gateway below it. LBC probes for Gateway API CRDs only at startup ([#383](https://github.com/nebari-dev/nebari-infrastructure-core/issues/383), [#417](https://github.com/nebari-dev/nebari-infrastructure-core/issues/417)). Confirmed on a fresh cluster: the crash path triggers only when the Gateway API CRDs are present **and** the LBC pod restarts so it notices them. Ordering between two independently-managed components decides the outcome. |

Deleting a CRD cascades to every CR of that type. NIC's ArgoCD apps use
`prune: true`, `selfHeal: true`, and
`resources-finalizer.argocd.argoproj.io` (for example,
`pkg/argocd/templates/apps/envoy-gateway.yaml`), so removing or renaming the
`envoy-gateway` app would prune the Gateway API CRDs and every Gateway,
HTTPRoute, and ListenerSet. ADR-0006's reconcile loop ("uninstalls managed
releases no longer listed") therefore needs an explicit CRD carve-out.

Upstream packaging is moving toward separate CRD ownership, but the boundary is
unstable. Envoy Gateway moved CRDs into a `charts/crds` subchart so Helm and
ArgoCD could upgrade them, and publishes a standalone
[`gateway-crds-helm`](https://github.com/envoyproxy/gateway/tree/main/charts/gateway-crds-helm)
chart. In 1.8.0, the `safe-upgrades` ValidatingAdmissionPolicy was under
`charts/crds/crds/`, breaking Flux and other external CRD tooling
([envoyproxy/gateway#9015](https://github.com/envoyproxy/gateway/issues/9015));
1.8.1 moved it back into templates. Existing separate CRD installs must
hand-add Helm ownership metadata to that policy before upgrading.

### Existing patterns

- **Flux** exposes `.spec.install.crds` and `.spec.upgrade.crds` with `Skip`,
  `Create`, or `CreateReplace`; the defaults are `Create` on install and `Skip`
  on upgrade. Auto-apply is a per-release opt-in, not a global default.
- **Versioned CRD artifacts** are increasingly upstream practice. Envoy
  Gateway's CRD-only chart is OCI-based and has `standard`/`experimental`
  channels; cert-manager exposes `crds.enabled` / `crds.keep`. Envoy Gateway's
  install docs prescribe `helm template … | kubectl apply --server-side` and
  upgrading CRDs before the controller.
- **Manual gating** is explicit in charts that do not provide a CRD-only
  artifact: kube-prometheus-stack says CRDs are not updated by default and
  should be updated manually, and a CRD change forces a major chart version.
- **ArgoCD's CRD convention** is a dedicated Application at a negative sync
  wave with `Prune=false` and `ServerSideApply=true`; NIC already applies the
  server-side half for CNPG.
- **Storage-version migration** is available through Kubernetes'
  `StorageVersionMigration` API (`migration.k8s.io`, beta and off by default)
  or the out-of-tree kube-storage-version-migrator, which rewrites existing CRs
  into a new storage version.

This is a structural decision across GitOps, provider-driven Helm, and
third-party chart pins, not a single installer bug.

## Decision Drivers

- Both current behaviours are unreviewed. On the imperative path a controller can
  upgrade while its CRDs do not, with no report until it misbehaves. On the
  ArgoCD path a destructive schema change applies on the next sync and reports
  success. Neither was chosen; both are the default of their mechanism.
- Unintentional data loss is worse than drift, and Helm's stated reasoning
  applies to NIC — but NIC already accepts auto-apply on most of the register, so
  invoking that reasoning against only one path needs justifying.
- CRD risk is group-specific, depending on existing CRs and the schema change,
  so a uniform global rule mis-fits.
- Whatever the rule is, operators must learn a bump needs manual action *before*
  running it, not after a five-minute Helm timeout.
- Both delivery mechanisms must be covered. ADR-0006's shared installer is
  still unimplemented ([#349](https://github.com/nebari-dev/nebari-infrastructure-core/issues/349)),
  so adding the requirement now costs one design line instead of four
  retrofits.

## Considered Options

Options 1, 2, 4 and 5 are candidate answers to the open upgrade question and are
not ranked here. Option 3 is orthogonal — it changes *who delivers* a group, and
composes with any of the others.

1. Auto-apply CRDs in the shared installer
2. Never auto-apply; detect drift and fail closed
3. Hoist CRDs into a NIC-owned artifact
4. Gate CRD-changing chart bumps
5. Policy per CRD group

## Decision Outcome

**Partially decided.** The four rules below are proposed as settled: they are
about *ownership* and *deletion*, and hold regardless of how the upgrade question
resolves. Whether NIC should auto-apply CRD upgrades, detect and gate them, or
some mix per group is **left open** — see [Open Questions](#open-questions). This
ADR should not be read as forbidding auto-apply.

Proposed rules:

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

2. **Automation never deletes a CRD.** Excluded from `Uninstall`, from the
   reconcile loop's "no longer listed" pruning, and from ArgoCD pruning
   (`Prune=false` on CRD resources). CRD removal is a documented manual
   operator action. No app sets `Prune=false` on CRDs today, so this is a change,
   not a description.

3. **Shared-group contention rule.** When two controllers can consume the same
   CRD group, one is designated owner at `max(required bundle version)` and the
   other's consumption is disabled by configuration. Concretely, Envoy Gateway
   owns Gateway API; LBC's `ALBGatewayAPI`, `NLBGatewayAPI`, and
   `GatewayListenerSet` gates stay off ([#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532)).
   [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532) sets
   those gates; this rule is what stops the next person from enabling them and
   inheriting unsolved ownership contention.
   Enabling them is a design change requiring a follow-up ADR that addresses
   hoisting Gateway API CRDs to their own app at a negative sync wave and LBC's
   startup-only CRD probe.

4. **CRD version floors are recorded** next to the chart pin they constrain, so
   a chart bump cannot silently outrun the CRDs on disk.

## Open Questions

These are for discussion, not decided here.

**1. Should NIC auto-apply CRD upgrades?** The two paths currently answer this
differently and neither answer was chosen deliberately:

| | Imperative Helm | ArgoCD |
|---|---|---|
| On upgrade | Never applies CRDs — silent divergence | Applies every sync — no gate, no signal |
| Reviewable before it hits a cluster? | Only as a chart version bump | Yes, as a rendered manifest diff in git |
| Failure mode | Controller runs ahead of its CRDs | A destructive schema change lands green |

The asymmetry is worth confronting directly: the ArgoCD path already does the
thing Option 1 would introduce on the Helm path, and it does it for the two
groups with the most CRs (`postgresql.cnpg.io`, `gateway.networking.k8s.io`).
Arguments in both directions:

- *For auto-apply:* it is what already happens on most of the register, it keeps
  controllers and CRDs versioned together, and Flux ships it as a supported
  per-release option (`CreateReplace`). Forbidding it on one path while relying
  on it on the other is hard to defend.
- *Against blanket auto-apply:* NIC cannot tell an additive schema change from a
  destructive one without inspecting it, and a bad apply is invisible until a
  controller breaks. Helm declined this deliberately for that reason.
- *Middle ground:* auto-apply as the default with detection and a named gate on
  the changes that actually carry risk — a removed served version, a changed
  storage version, a narrowed field — rather than on any `crds/` diff. This is
  Option 5 in spirit, and needs Option 2's detection to exist first to tell us
  how often each case arises.

**2. Where is the gate — the deploy path, or a dedicated `nic upgrade` step?**
If detection lands, drift can block the deploy, log a warning, or be handled
somewhere else entirely. Blocking creates a new failure mode operators must
understand; warning can be ignored indefinitely; either needs an override with
teeth.

A third answer is a dedicated `nic upgrade` step that handles CRD removal and
replacement in a controlled sequence, rather than making `nic deploy` refuse to
run. The precedent is Nebari Classic, which applied versioned upgrade steps in
sequence from the config's version up to the target release — awkward in places,
but it worked. Foundational software has no software pack to carry its
migrations, so it likely needs `nic upgrade` machinery of its own regardless.

This has a prerequisite the other answers do not: **sequential upgrade steps
imply versioned configs, which NIC does not have.** Choosing this answer makes
versioned configs a blocker rather than a nice-to-have.

**3. Does the ArgoCD path need a review gate at all?** A CRD change there is
visible as a manifest diff at PR time, which may already be the gate — or may be
review theatre, given how large CRD diffs are (LBC 3.5.0 is 1,271 lines) and how
unlikely they are to be read.

**4. Which groups, if any, move to a dedicated CRD app?** Option 3 is cheap for
Gateway API because upstream publishes `gateway-crds-helm`. It is not obviously
worth it elsewhere.

**5. Should conditional foundational software move into ArgoCD at all?** This
question is in scope alongside the CRD one, and it subsumes much of the rest: with
one delivery mechanism, most of the CRD problem is a mechanism-mismatch problem
that stops existing.

The starting position for discussion is that **everything except ArgoCD itself
belongs in ArgoCD, conditional foundational software included** — the burden of
proof sits on keeping the imperative path, not on removing it.

The industry pattern is the same direction. GitOps Bridge — the provisioner stops
installing software and instead writes cluster metadata that ArgoCD keys addon
selection and values off, which AWS EKS Blueprints adopted in place of Terraform
`helm_release` — answers ADR-0006's objection that provider-computed values are
awkward to express declaratively. NIC is already close: `pkg/argocd/writer.go`
computes values in Go and renders per-cluster manifests, which is the same idea
without ApplicationSets.

The residual case for the imperative path is teardown ordered against
`tofu destroy` (Longhorn,
[ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md)) — one component,
not a mechanism.

If this resolves toward ArgoCD, rows 6–9 of the register collapse into the ArgoCD
path and the shared-group rule loses most of its scope. The register above should
therefore be read as a description of today, not a design to build against, and
**question 1 should not be settled in a way that only makes sense while two
mechanisms exist.** Resolving this also requires revising ADR-0006, which this
ADR does not do.

Whichever way question 1 resolves, the manual sequence is the same and is not
in dispute: apply the new CRDs server-side, migrate stored objects
(`StorageVersionMigration` or kube-storage-version-migrator), then upgrade the
controller. What is open is when NIC does this for you and when it makes you do
it.

## Consequences

Of the four proposed rules only — the upgrade question carries its own trade-offs,
listed under [Open Questions](#open-questions).

| Benefits | Costs |
|---|---|
| Every CRD group has one writer, so "who owns this" stops being re-litigated per component. | Per-group ownership is more to hold in mind than one global rule. |
| No automated path can cascade a CRD deletion into every CR of that type. | CRDs are never pruned, so they accumulate on long-lived clusters, including for removed software. |
| Gateway API ownership is checkable, including why `ALBGatewayAPI` cannot be enabled. | `Prune=false` must be added to every CRD-bearing app; until it is, the rule is aspirational. |
| Recorded version floors mean a chart bump cannot silently outrun its CRDs. | Floors are hand-maintained and rot unless something checks them. |
| New foundational software has a CRD owner before it ships rather than after. | The upgrade question stays open, so the divergence on the imperative path is documented but not yet fixed. |

## Options Detail

### Option 1: Auto-apply CRDs in the shared installer

Server-side-apply `chrt.CRDObjects()` before `action.Upgrade`, with
`SkipCRDs = true` on install so one path owns CRDs. This removes drift and keeps
controllers and CRDs together, but cannot infer destructive schema changes and
can still fail during a real migration after partially applying a set. Flux's
`CreateReplace` is the precedent, but only as a per-release opt-in.

### Option 2: Never auto-apply; detect drift and fail closed

A preflight compares chart `crds/` with live CRDs and reports a named operator
action before cloud work. It is cheap and addresses the actual harm, but the
upgrade remains manual and the implementation must choose whether drift blocks
the deploy or becomes an ignorable warning.

### Option 3: Hoist CRDs into a NIC-owned artifact

A dedicated ArgoCD app at a negative sync wave owns CRDs with `Prune=false` and
`ServerSideApply=true`, making them visible and diffable and resolving shared
ownership. This is Helm's recommended Method 2 and the standard ArgoCD
convention. For Gateway API, use the upstream CRD-only artifact; groups without
one still require vendoring and tracking.

### Option 4: Gate CRD-changing chart bumps

Refuse a chart version whose `crds/` differ from the installed release's, making
the bump an explicit opt-in migration. It composes with Option 2 and matches the
practice represented by [#532](https://github.com/nebari-dev/nebari-infrastructure-core/pull/532),
but can slow harmless bumps and needs an override with real teeth.

### Option 5: Policy per CRD group

Auto-apply only where a change is provably additive and no CRs exist; require the
explicit path otherwise. This matches group-specific risk, but needs schema
diffing, CR counts, and a policy table, while "provably additive" is precisely
the hard case. It is premature before Option 2 produces data.

## Links

- [ADR-0006](0006-conditional-foundational-software-helm.md) — conditional
  foundational software via provider-driven Helm; the mechanism with the gap
  (decided in [#361](https://github.com/nebari-dev/nebari-infrastructure-core/pull/361),
  scope clarified in [#475](https://github.com/nebari-dev/nebari-infrastructure-core/issues/475))
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
- [Kubernetes — Migrate Kubernetes Objects Using Storage Version Migration](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/storage-version-migration/)
  — the named remedy for the `storedVersions` blocker
- [Flux — HelmRelease CRD policies](https://fluxcd.io/flux/components/helm/helmreleases/)
  — `Skip` / `Create` / `CreateReplace`; auto-apply as a per-release opt-in
- [Envoy Gateway — `gateway-crds-helm`](https://github.com/envoyproxy/gateway/tree/main/charts/gateway-crds-helm)
  and [install docs](https://gateway.envoyproxy.io/docs/install/install-helm/) —
  an upstream versioned CRD-only chart, and the "CRDs first, then the controller"
  ordering
- [envoyproxy/gateway#9015](https://github.com/envoyproxy/gateway/issues/9015) —
  the 1.8.0 `safe-upgrades` VAP placement that broke external CRD tooling
- [GitOps Bridge](https://github.com/gitops-bridge-dev/gitops-bridge) — the
  metadata-not-installs pattern behind "everything ArgoCD-managed"
- Helm source at v3.21.1: `pkg/action/install.go:161-180`,
  `pkg/action/upgrade.go`, `cmd/helm/upgrade.go:267`
