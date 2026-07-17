# ADR-0011: Per-app Gateway listener ownership (mergeGateways vs XListenerSet)

## Status

Proposed

This ADR is open for discussion. It records a direction decision that is currently
being made implicitly in code (NIC PR #492 and nebari-dev/nebari-operator#167),
and which reverses the recommendation captured in issue #484. Per the request on
PR #492, the direction should be ratified here before implementation lands across
NIC and the operator.

## Date

2026-07-17

## Context

The nebari-operator provisions per-application TLS. Today it does this by a
read-modify-write update of NIC's shared `envoy-gateway-system/nebari-gateway`
Gateway: it appends one HTTPS listener (`tls-<app>-<namespace>`) per NebariApp to
`Gateway.spec.listeners`. NIC also owns that Gateway through GitOps and declares
its `http` and `https` listeners.

Two controllers therefore co-own one mutable list field. This is issue #484. The
consequences all fall out of that single co-ownership:

- The `gateway-config` Argo CD application is permanently OutOfSync (the live
  listener list carries entries that are not in Git).
- With self-heal on, Argo CD and the operator can repeatedly rewrite the same
  field; a sync can momentarily remove an operator listener and detach an
  HTTPRoute from its TLS listener.
- A Gateway supports at most 64 listeners. Because NIC already owns two, the
  shared design has an effective ceiling of 62 per-app TLS listeners.
- Concurrent operator reconciles contend on the single Gateway object.

The deciding constraint (see the discussion on #484) is the per-app TLS secret
model: packs set `routing.tls.secretName`, and NIC issues certs over HTTP-01 only
(no DNS-01, no assumable public wildcard). So "one shared wildcard listener" is
not a general answer; each app legitimately needs its own certificate, selected at
`:443` via SNI.

Issue #484 recorded three options and recommended Option 2 (XListenerSet),
ranking Option 3 (mergeGateways) last ("only if experimental Gateway API usage is
unacceptable and the merged-Gateway integration work is justified"). The
in-progress PRs implement Option 3. This ADR exists to make that call explicitly
rather than in a PR body.

## Decision Drivers

- Remove the co-ownership at the root (one writer per listener), not mask it.
- Preserve the per-app secret model (each app its own cert, selected by SNI).
- Keep NIC's platform ingress independent of the operator (NIC keeps owning its
  own Gateway; the operator only adds per-app listeners).
- Prefer staying on the pinned Envoy Gateway v1.6.2 and the stable Gateway API
  surface, all else equal.
- Provide (or be able to provide) an ownership boundary for which app may claim
  which hostname (relates to the operator's hostname-ownership hardening).
- Minimize cross-repo coordination and upgrade-test surface.

## Considered Options

1. Argo CD `ignoreDifferences` on the shared Gateway's operator-created listeners.
2. XListenerSet (each app contributes listeners to the shared Gateway via a
   separate, singly-owned object). Issue #484's recorded target.
3. Per-app Gateway + Envoy Gateway `mergeGateways` (the operator creates one
   Gateway per NebariApp in the app's own namespace; EG merges every Gateway on
   the class onto one dataplane). What PR #492 and nebari-operator#167 implement.

## Decision Outcome

Recommended (for discussion): **Option 3 (per-app Gateway + mergeGateways) for
v0.1.0, with Option 2 (XListenerSet) as the planned migration once Envoy Gateway
v1.7.x is adopted and the XListenerSet API stabilizes.**

Rationale:

- On the deciding axis (per-app secrets) Options 2 and 3 are equivalent - both
  give each app its own listener and cert, selected by SNI.
- Option 3 works on the current EG v1.6.2 pin and uses only stable Gateway API
  kinds. Option 2 requires an EG upgrade to v1.7.x (v1.6.2 ships the XListenerSet
  CRD via the experimental Gateway API channel but does not reconcile it) plus an
  upgrade-test matrix across every supported Kubernetes version, and it rides an
  experimental `gateway.networking.x-k8s.io` API with no compatibility guarantee.
- Option 2's main advantage over Option 3 is `Gateway.spec.allowedListeners`, a
  native namespace allow-list controlling who may attach listeners. In our
  architecture the operator is the SOLE creator of per-app Gateways - users only
  create NebariApps and the operator mediates - so there is no untrusted third
  party attaching listeners for `allowedListeners` to fence off. The operator is
  the policy enforcement point and can guarantee hostname uniqueness itself. That
  deflates Option 2's biggest advantage for our specific model.

This recommendation reverses #484's Option-2 target. The reversal is deliberate
and rests on two facts #484 undersold: the EG v1.6.2-vs-v1.7.x upgrade cost, and
that our operator-mediated model does not need `allowedListeners`. It should be
ratified (or overturned) by the team here.

Both directions have been partially de-risked on a local k3d bed running the
pinned EG v1.6.2: Option 3 was validated end to end (three Gateways across two
namespaces merged onto one `:443`, each serving its own SNI cert); Option 2 was
confirmed unavailable on v1.6.2 (CRD present, controller does not reconcile it).

### Consequences

**Good:**
- Ships on the current EG v1.6.2 pin; no gateway-implementation upgrade for v0.1.0.
- No dependency on an experimental, unstable API for the v0.1.0 contract.
- Co-ownership is eliminated: each Gateway has exactly one writer, so #484's drift,
  churn, and concurrency issues go away.
- Per-app Gateways sidestep the 64-listener-per-Gateway cap (each is independent).
- Feature-flagged and additive (`TLS_PER_APP_GATEWAY`, default off), so it is
  independently releasable and reversible.

**Bad:**
- `mergeGateways` is an Envoy-Gateway-specific feature; this couples NIC and the
  operator to EG more tightly than the portable XListenerSet path would.
- No `allowedListeners` equivalent: `mergeGateways` merges every Gateway on the
  class, so the blast radius is broader and the operator must guarantee the
  `(port, protocol, hostname)` tuple is unique across all merged Gateways and fail
  cleanly on collision.
- Choosing Option 3 now means a future migration to Option 2 if/when portability
  or standardization outweighs the upgrade cost.
- The endpoint-discovery label selector must track `mergeGateways` (the merged
  dataplane Service is named after the GatewayClass, not the Gateway); NIC and the
  operator flag must stay in lockstep.

## Options Detail

### Option 1: Argo CD ignoreDifferences

Tell `gateway-config` to ignore operator-created `tls-*` listener entries.

**Pros:**
- Smallest change; stops the OutOfSync noise immediately.

**Cons:**
- Masks the co-ownership instead of removing it. Does not fix sync-window churn,
  the 62-listener ceiling, or concurrent-reconcile contention.
- Trades away real drift detection on the shared Gateway.

Rejected as a durable fix; acceptable only as a temporary unblock, and this ADR
recommends not relying on it.

### Option 2: XListenerSet

Each app creates an `XListenerSet` that references the shared Gateway via
`parentRef` and contributes its own listener(s). The Gateway owner opts in via
`spec.allowedListeners`.

**Pros:**
- Purpose-built Gateway API mechanism for exactly this problem; portable across
  conformant implementations once it graduates.
- Native `allowedListeners` namespace allow-list (delegation and blast-radius
  control; overlaps the operator's hostname-ownership boundary work).
- Attaches to the existing single Gateway, so no `mergeGateways` and no
  endpoint-discovery label change are needed on the NIC side.

**Cons:**
- Not reconciled by our pinned EG v1.6.2; requires an EG upgrade to v1.7.x plus an
  upgrade-test matrix across supported Kubernetes versions.
- Rides the experimental `gateway.networking.x-k8s.io` API: no compatibility
  guarantee, schema may change between Gateway API releases, and standard-channel
  clusters cannot use it (NIC bundles the experimental channel today, so this is
  satisfied on our stack but remains a constraint).
- Different implementation surface (route attachment to the ListenerSet/section)
  and thinner ecosystem/tooling support.

### Option 3: Per-app Gateway + mergeGateways

The operator creates one Gateway per NebariApp in the app's own namespace, each
owning a single HTTPS listener with its own cert (co-located in the same
namespace, owner-referenced for GC). Envoy Gateway `mergeGateways` (set on an
EnvoyProxy referenced by the GatewayClass) merges all Gateways on the class onto a
single dataplane.

**Pros:**
- Works on the current EG v1.6.2 pin using stable Gateway API kinds only.
- One writer per Gateway; co-ownership gone. Per-app Gateways relieve the
  64-listener cap. Owner references give clean GC.
- Validated end to end on a local k3d bed on the pinned EG.

**Cons:**
- EG-specific (`mergeGateways` is not portable Gateway API).
- No `allowedListeners` equivalent; broader blast radius, and the operator must
  enforce global `(port, protocol, hostname)` uniqueness and collision handling.
- NIC's endpoint discovery must select the merged dataplane by
  `owning-gatewayclass`, coupling that change to `mergeGateways` being enabled.

## Migration and reversibility

Option 3 -> Option 2 is feasible later without re-litigating the per-app secret
model: the operator swaps per-app Gateway creation for XListenerSet creation, NIC
drops the EnvoyProxy/mergeGateways config and reverts the endpoint selector, and
EG is upgraded to v1.7.x. The `TLS_PER_APP_GATEWAY` flag keeps the shared-listener
path available throughout, so either direction can be rolled back per-deployment.

## Related

- #484 (shared-Gateway co-ownership; recorded Option 2 target)
- #403 (Listener-only TLS; a related missing-primitive proposal)
- PR #492 (NIC: enable mergeGateways) and nebari-dev/nebari-operator#167
  (operator: per-app Gateway behind `TLS_PER_APP_GATEWAY`)
- The operator's hostname/TLS ownership-boundary hardening (blast-radius concern
  that `allowedListeners` would otherwise address).
