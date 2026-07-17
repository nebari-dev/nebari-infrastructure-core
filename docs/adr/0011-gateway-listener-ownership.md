# ADR-0011: Per-app Gateway listener ownership

## Status

Proposed

Open for discussion. This ADR records the direction for eliminating the
shared-Gateway listener co-ownership described in #484. It has been revised from
its first draft: an earlier version recommended Option 3 (mergeGateways) on the
premise that Option 2 required an experimental API. That premise no longer holds
(see below), so the recommendation is now Option 2. Both directions have been
validated on a local k3d bed; the evidence is recorded here.

## Date

2026-07-17

## Context

The nebari-operator provisions per-application TLS by a read-modify-write update
of NIC's shared `envoy-gateway-system/nebari-gateway` Gateway: it appends one
HTTPS listener (`tls-<app>-<namespace>`) per NebariApp to `Gateway.spec.listeners`.
NIC also owns that Gateway through GitOps. Two controllers therefore co-own one
mutable list field (#484). Consequences: `gateway-config` is permanently
OutOfSync; self-heal can churn and momentarily detach a route from its listener;
the 64-listener-per-Gateway cap leaves an effective ceiling of 62 per-app
listeners; and concurrent reconciles contend on one object.

The deciding constraint (per the discussion on #484) is the per-app secret model:
packs set `routing.tls.secretName`, and NIC issues certs over HTTP-01 only (no
DNS-01, no assumable public wildcard). So a single shared wildcard listener is not
a general answer; each app needs its own certificate, selected at `:443` by SNI.
Listing every per-app secret in the shared listener's `certificateRefs` does not
help either - that is the same co-ownership on a different field.

Removing co-ownership therefore requires the operator to own its own listener
resource. This ADR decides which one.

## Decision Drivers

- Remove co-ownership at the root (one writer per listener), not mask it.
- Preserve the per-app secret model (each app its own cert, selected by SNI).
- Keep NIC's platform ingress independent of the operator.
- Provide an ownership/delegation boundary for which namespace may attach
  listeners (relates to the operator's hostname-ownership hardening).
- Avoid founding the operator's first stable contract on an unstable API.
- Minimize upgrade-test surface and downtime risk.

## Considered Options

(Numbering follows #484 so "Option N" means the same thing across the discussion.)

1. Shared wildcard certificate + per-app HTTPRoutes on the shared listener.
2. Gateway API `ListenerSet` (operator owns one per app, attached to NIC's Gateway).
3. Separate Gateways + Envoy Gateway `mergeGateways`.
4. Explicit operator SSA field manager + NIC `managedFieldsManagers` ignore.

Short-term mitigation (not a target architecture): scoped Argo CD
`ignoreDifferences` on operator-created listeners with `RespectIgnoreDifferences`.

## Decision Outcome

Recommended (for discussion): **Option 2 - the operator owns a per-app
`ListenerSet` (standard-channel `gateway.networking.k8s.io/v1`) attached to NIC's
Gateway, on Envoy Gateway v1.8.x.**

Rationale:

- **The experimental-API objection is gone.** Gateway API v1.5 graduated
  `ListenerSet` to the Standard channel (`gateway.networking.k8s.io/v1`), and
  Envoy Gateway v1.8 reconciles it unconditionally (no feature flag, no
  experimental channel). This is the stable `ListenerSet`, not the experimental
  `x-k8s.io/XListenerSet` the first draft analyzed. The only remaining cost of
  Option 2 is the EG v1.6.2 -> v1.8.x upgrade, and that upgrade is already
  prototyped (NIC PR #496).
- **Option 3 does not avoid that upgrade; it defers it and adds work.** If
  ListenerSet is the intended end state, Option 3 still needs the EG upgrade
  eventually, while adding the `mergeGateways` implementation now plus a later
  migration. That migration changes the dataplane Service ownership model (from
  the GatewayClass under `mergeGateways` back to the shared Gateway), which can
  rotate the Service / load balancer and cause downtime unless identity is
  explicitly preserved.
- **Option 2 has a native delegation boundary.** `Gateway.spec.allowedListeners`
  authorizes listener attachment by namespace once; `mergeGateways` has no
  equivalent Gateway API attachment policy, so its boundary must be enforced in
  the operator, RBAC, or admission.
- The residual experimental risk is soft for us: we own both NIC and the
  operator, so an API change is a coordinated bump on our side, not a
  cross-vendor break.

**Bridge clause:** if upgrading Envoy Gateway is a hard constraint for v0.1.0,
Option 3 (per-app Gateway + `mergeGateways`) is a defensible bridge because it
removes co-ownership immediately on the current pin. If we take the bridge, this
ADR must additionally record (a) why the EG upgrade cannot land now, (b) an exit
criterion back to Option 2, and (c) the Service-identity migration risk above.

Both options were validated on a local k3d bed (details under Options Detail).
This recommendation reverses the first draft; it should be ratified or overturned
by the team here.

### Consequences

**Good:**
- Per-app listener + cert in an operator-owned object; NIC's Gateway is never
  mutated by the operator. Co-ownership, the 64-listener cap, and the concurrency
  contention all go away.
- Stable Gateway API kind for the operator's first stable contract.
- `allowedListeners` gives a real, Gateway-API-native delegation boundary.
- Single Gateway / single dataplane model is preserved (no Service-identity
  migration, unlike the Option 3 -> Option 2 path).

**Bad:**
- Requires upgrading Envoy Gateway from v1.6.2 to v1.8.x, with upgrade testing
  across every supported Kubernetes version (NIC PR #496 is the pin change).
- `ListenerSet` is newer than the core kinds; ecosystem/tooling support is thinner
  and EG does not yet support `ListenerSet` as an xPolicy `targetRef` (not a
  blocker today - the operator's SecurityPolicy targets the HTTPRoute - but worth
  integration-test coverage).

## Options Detail

### Option 1: Shared wildcard certificate + per-app HTTPRoutes

Keep NIC's single shared HTTPS listener; every operator HTTPRoute attaches to its
`https` section; the operator stops creating per-app Certificates and stops
mutating the Gateway.

**Pros:** no EG upgrade; no shared-resource mutation; no per-app listener limit.
**Cons:** no per-app certificate isolation, and a NIC-managed public wildcard needs
DNS-01, which NIC does not support. Viable only as a lighter mode for genuine
single-cert or self-signed deployments, not as the default.

### Option 2: Gateway API ListenerSet (recommended)

The operator creates and owns one `ListenerSet` per NebariApp in the app's own
namespace, attached to NIC's Gateway via `spec.parentRef`, each listener carrying
its own cert (co-located, resolved without a ReferenceGrant). NIC opts in via
`Gateway.spec.allowedListeners`. HTTPRoutes attach via `parentRefs` to the
ListenerSet.

**Pros:** purpose-built Gateway API delegation; removes co-ownership and the
64-listener cap; keeps `routing.tls.secretName` working with per-app certs; native
`allowedListeners` boundary; stable API on EG v1.8.
**Cons:** requires the EG v1.6.2 -> v1.8.x upgrade + test matrix.

**Bed validation (EG v1.8.1 on k3d):** the standard
`listenersets.gateway.networking.k8s.io` (v1) CRD is present and EG reconciles it
with no feature flag (`extensionApis: {}`), matching PR #496. A ListenerSet in a
separate namespace attached to `nebari-gateway` reached Accepted=True,
Programmed=True; its HTTPRoute resolved; `curl` returned HTTP 200 serving the
app's own SNI cert; and NIC's `Gateway.spec.listeners` was not modified by the
ListenerSet (no co-ownership).

### Option 3: Per-app Gateway + mergeGateways

The operator creates one Gateway per NebariApp in the app's namespace, each with a
single HTTPS listener + co-located cert, owner-referenced for GC. EG `mergeGateways`
merges all Gateways on the class onto one dataplane. (Per-app, not one shared
operator Gateway, so the 64-listener cap is avoided.)

**Pros:** works on the current EG v1.6.2 pin using stable Gateway API kinds only;
one writer per Gateway; validated end to end on the k3d bed (three Gateways across
two namespaces merged onto one `:443`, each serving its own SNI cert).
**Cons:** EG-specific (not portable Gateway API); no `allowedListeners` equivalent,
so the operator must guarantee global `(port, protocol, hostname)` uniqueness and
fail cleanly on collision; NIC endpoint discovery must select the merged dataplane
by `owning-gatewayclass`; and if ListenerSet is the end state, a later migration
rotates dataplane Service ownership (GatewayClass -> shared Gateway) with
LB-rotation/downtime risk.

### Option 4: Explicit operator SSA field manager

Operator uses server-side apply with a unique field owner; NIC ignores that
manager's fields via `managedFieldsManagers`.

**Pros:** small coordinated change; more robust than jq name-matching; better
concurrent-update behavior.
**Cons:** still tolerates co-ownership by design; does not remove the 64-listener
cap; no architectural isolation. Fallback, not a target.

## Migration and reversibility

The companion operator PR is feature-flagged (`TLS_PER_APP_GATEWAY`, default off),
so the shared-listener path stays available and any direction is reversible
per-deployment. Going straight to Option 2 avoids the Option 3 -> Option 2
dataplane Service-identity migration entirely.

## Related

- #484 (shared-Gateway co-ownership; option analysis and the per-app-secret axis)
- #403 (Listener-only TLS; a related missing-primitive proposal)
- PR #496 (NIC: Envoy Gateway v1.6.2 -> v1.8.1 pin; prerequisite for Option 2)
- PR #492 (NIC: mergeGateways - the Option 3 implementation) and
  nebari-dev/nebari-operator#167 (operator: per-app Gateway behind
  `TLS_PER_APP_GATEWAY` - Option 3). Retained for reference; superseded if Option 2
  is adopted.
- The operator's hostname/TLS ownership-boundary hardening (the boundary that
  `allowedListeners` provides natively under Option 2).
