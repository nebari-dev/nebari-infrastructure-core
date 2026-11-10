# ADR-0022: Per-User Secrets as Foundational Software

## Status

Proposed

Tracking issue: [#763](https://github.com/nebari-dev/nebari-infrastructure-core/issues/763).

The design this ADR adopts is in [nebari-dev/nebari-user-secrets](https://github.com/nebari-dev/nebari-user-secrets), `docs/design/user-secrets.md`, under review in [nebari-dev/nebari-user-secrets#1](https://github.com/nebari-dev/nebari-user-secrets/pull/1). This ADR records where that component lives and what NIC takes on to deploy it.

## Date

2026-11-10

## Context

Nebari users have nowhere to keep their own API keys. A data vendor token, an OpenAI key or a fine-grained GitHub PAT ends up in a notebook cell or a `.env` file in the user's home directory, where it sits on persistent storage, travels with any notebook they share, and never reaches the Dask clusters they launch.

The proposed design gives each user a store tied to their Keycloak `sub` and delivers the secrets they enable for an app as files in that app's pods. The hard part is proving, inside the cluster, which user a pod belongs to. The design does that with a projected, pod-bound ServiceAccount token for the pod's own identity, a user annotation stamped by the component that already authenticated the user (the JupyterHub hub, the dask-gateway controller), and two ValidatingAdmissionPolicies that keep anyone else from stamping or changing it. The policies are tested on kind against Kubernetes 1.30, 1.32 and 1.34.

`nebari-dev/external-auth-pack` tried an earlier version of this as a software pack, with a shared secret and a `?user=` parameter. Any caller holding the secret could ask for any user's credentials, which is the property the new design exists to remove.

Two parts of the design decide where it lives:

- The consumers span several software packs today (data-science-pack, dask-gateway-pack) and more later (Javo, Ray, job schedulers). A secrets store that lives inside one pack makes every other pack depend on that pack.
- It needs cluster-scoped admission policies that apply to pods created by any of those packs. Whoever installs them owns a piece of cluster-wide pod admission.

## Decision Drivers

- One store for every pack that runs user workloads, with no pack depending on another.
- The cluster-scoped admission policies belong with the install that already owns cluster-scoped foundational objects.
- No change for existing deployments until a pack opts in.
- Follow the existing pattern for Nebari-owned foundational components (separate repo, released chart, rendered by NIC as an Argo CD application).

## Considered Options

1. A separate repo, `nebari-dev/nebari-user-secrets`, deployed by NIC as unconditional foundational software
2. A software pack (`user-secrets-pack`) installed per deployment
3. Part of data-science-pack, next to JupyterHub
4. An off-the-shelf secrets manager (Vault, External Secrets Operator) deployed by NIC

## Decision Outcome

Chosen option: "A separate repo deployed by NIC as unconditional foundational software", because every software pack that runs user workloads needs the same store and the same pod-identity guard, and the guard is cluster-scoped admission that NIC is already the right owner for. It follows how NIC deploys `nebari-operator` and the landing page.

NIC takes on:

- An Argo CD `Application` template for the nebari-user-secrets chart in `pkg/argocd/templates/apps/`, after cert-manager and Keycloak, since the chart needs a cert-manager CA and a `NebariApp` Keycloak client.
- Applying the component's `cluster/` bundle (the two ValidatingAdmissionPolicies and their bindings) in a later sync wave than the chart, because policy B reads a ConfigMap the chart creates. Installing them in the other order only denies labelled pods until the chart is in, but the ordering avoids that window.
- Rendering the creator registry (which namespaces and apps each creator may name users for) from the packs NIC knows are installed, through the existing values overlay seam ([ADR-0014](0014-helm-valuefiles-overlay-seam.md)).
- A user journey test ([ADR-0017](0017-user-journey-tests-for-foundational-software.md)) once a consumer is wired: a user stores a secret and a notebook receives it, and a second user does not.

The component is unconditional in the sense of [ADR-0006](0006-conditional-foundational-software-helm.md): it does not depend on provider or cluster shape. Installing it changes no workload, because nothing reaches a pod until a pack opts in.

### Consequences

**Good:**
- Every pack consumes the same store through one written contract, with values-only registration.
- The admission policies are installed and versioned by the same tool that installs the rest of the cluster's foundational objects.
- Teams that review cluster-scoped objects before install review one small directory with its own `REVIEW.md` and test transcripts.

**Bad:**
- Every Nebari cluster runs the service and its two admission policies, including clusters where no pack has opted in yet.
- Policy A is evaluated on every pod write and ServiceAccount token request in the cluster. It is designed to deny only requests involving user-secrets metadata, and the test matrix shows unrelated pods, scheduler bindings, kubelet status updates and kubelet token minting pass, but a CEL error in it would deny the requests it matches. The component mitigates this with a canary probe and a CI matrix; NIC inherits the blast radius.
- NIC's supported Kubernetes versions must stay at 1.30 or later (ValidatingAdmissionPolicy GA), including the local provider's kind node image, and clusters must run the NodeRestriction admission plugin.
- One more component in NIC's release and upgrade path.

## Options Detail

### Option 1: Separate repo, deployed by NIC as foundational software

The service, init binary, helper, chart and `cluster/` bundle live in `nebari-dev/nebari-user-secrets` with their own release cycle and image, like `nebari-operator`. NIC renders an Argo CD application for the chart and applies the cluster bundle.

**Pros:**
- Matches the existing pattern for Nebari-owned foundational components.
- One install owns the cluster-scoped policies.
- Packs depend on a platform contract, not on each other.

**Cons:**
- Present on every cluster.
- NIC's minimum Kubernetes version becomes a hard requirement of the component.

### Option 2: A software pack installed per deployment

The original plan: `user-secrets-pack`, installed like any other pack.

**Pros:**
- Only clusters that want it run it.

**Cons:**
- Every consuming pack would need the secrets pack installed first, which is a pack-to-pack dependency the pack model does not express.
- A software pack would install cluster-wide admission policies, which is more authority than packs otherwise take.

### Option 3: Part of data-science-pack

Ship the store with JupyterHub, since the first consumer is the notebook.

**Pros:**
- Fewest moving parts for the first release.

**Cons:**
- dask-gateway-pack, Javo and any later consumer would depend on data-science-pack.
- Javo is a possible successor to JupyterHub in data-science-pack, so the store would be tied to the component expected to change.

### Option 4: Vault or External Secrets Operator

Deploy an off-the-shelf secrets manager instead of a Nebari component.

**Pros:**
- Mature storage, rotation and audit.

**Cons:**
- Both key identity on ServiceAccount or namespace, so per-user identity would need a ServiceAccount or namespace per user, which the design rejects (some deployments get one namespace).
- External Secrets materialises per-workload Kubernetes Secrets, which the design treats as an anti-goal.
- The pod-identity problem is unchanged, so the admission policies would still be needed.

## Links

- Design and cluster-scoped objects under review: [nebari-dev/nebari-user-secrets#1](https://github.com/nebari-dev/nebari-user-secrets/pull/1) (`docs/design/user-secrets.md`, `cluster/REVIEW.md`)
- [ADR-0006](0006-conditional-foundational-software-helm.md): conditional foundational software
- [ADR-0014](0014-helm-valuefiles-overlay-seam.md): Helm valueFiles overlay seam
- [ADR-0017](0017-user-journey-tests-for-foundational-software.md): user journey tests for foundational software
