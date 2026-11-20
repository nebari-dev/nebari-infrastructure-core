# ADR-0023: Pack Platform Contract

## Status

Proposed

## Date

2026-11-17

## Context

Packs integrate with Nebari through a mix of documented interfaces, implicit details and workarounds. Across NIC, nebari-operator and the 25 software packs in nebari-dev, only NebariApp has a normative spec (`nebari-operator/docs/pack-specification.md`, v0.1.0). Packs also depend on storage, Postgres, Keycloak, the trust bundle, the Argo CD project and the telemetry pipeline through names nobody promised. Five or more packs hardcode `keycloak-keycloakx-http.keycloak.svc.cluster.local`, twelve example Applications use the deny-all `default` Argo CD project, two packs read the master Keycloak admin password, and about twelve packs bundle their own Postgres.

Each of these dependencies works until a chart rename, a provider difference or a namespace change breaks it, and nothing tells the pack author which names are safe. Nothing tells them either how Nebari will change: whether a capability can disappear in the next release, how much notice they get, or what happens when a security fix has to break something.

Nebari needs a short list of promises that pack authors can depend on, that hold on every supported target (AWS, Azure, Hetzner, local kind and existing clusters on which NIC installs all of the foundational software; GCP joins when its provider is supported), that a test proves on each of them, and that change only under rules pack authors can plan around.

The guiding rule is "no is temporary, yes is forever". The contract offers what packs demonstrably need and nothing more. Anything not in the contract is not promised, even if it exists today.

## Decision Drivers

- Pack authors need one place that says exactly what Nebari provides, what it does not, and how that changes.
- A promise that works on some targets, or differently per target without the pack being told, is not a promise.
- A promise without a passing conformance test drifts from reality.
- The contract must sit next to the code and tests that keep it. Most promises (storage classes, CloudNativePG, the trust bundle, the Argo CD project, telemetry and every per-target difference) are built in NIC.
- Promises expressed as incidental names (a Service, a namespace, a Gateway name, a chart-generated resource) break when the incidental thing changes.
- Any foundational software that satisfies the contract should be swappable, not only the specific products NIC installs today, so restricted platforms such as OpenShift can supply their own equivalents.
- Pack authors need a strong compatibility guarantee, a predictable deprecation process and a clear rule for security exceptions, in the spirit of Go's.
- Clusters already deployed must reach the contract by running `nic deploy` with a new NIC release, without recreating the cluster.

**Related proposals.** [ADR-0021](https://github.com/nebari-dev/nebari-infrastructure-core/pull/749) proposes that packs get plain values and never query the cluster, with render-time values from `nic outputs`; #670 proposes typed render-time pack values; nebari-operator PR #211 proposes a `PackContract` CRD. P5 uses the render-time channel: platform facts reach a pack as Helm values under `global.nebari`, which NIC writes into the GitOps repository and the pack's Argo CD Application loads as a value file. A pack needs these facts when its chart is rendered (the NebariApp `spec.hostname` and an upstream app's public URL and OIDC settings), and Argo CD renders charts without cluster access, so a runtime ConfigMap could not supply them. How a pack's Application references the file depends on #670, and the nebari-app library chart in nebari-operator is the recommended way to consume the values. A runtime channel is listed in [Not yet](../pack-contract/not-yet.md). A `PackContract` CRD could later become how a pack declares its contract version and the promises it uses.

## Considered Options

1. Extend the operator spec to cover everything
2. NIC owns the contract; the operator spec is one chapter
3. A separate `nebari-dev/pack-contract` repository

## Decision Outcome

Chosen option: **Option 2, NIC owns the contract and the operator spec is one chapter**, because NIC owns the deployment targets and the end-to-end tests, so each promise sits next to the code and the test that keep it.

**Where it lives.** This ADR sets the contract, its levels and its policies. The [pack contract reference](../pack-contract/README.md) has one page per promise, the lists of what is not promised, and the policy pages. The NebariApp chapter stays in the operator spec, and the reference pins the spec version it includes. Conformance tests extend the ADR-0017 journey suite with a `pack_contract` group that runs on every target.

**The promises.** Eighteen promises, nine stable and nine experimental, each with a level, a state (built, partly built or planned), an interface, what packs may and must not rely on, per-target notes and a conformance test. All eighteen are required for 1.0.

**This is the 1.0 contract, and most of it is not built yet.** It lists every promise contract 1.0 will make, so pack authors and NIC maintainers work toward the same target. Most promises are still planned or partly built; the table shows the state of each. A promise's level says what it will be once built; its state says what holds now. A planned promise carries no guarantee, whatever its level, and its interface may change until it is built (see [Compatibility](../pack-contract/policy/compatibility.md#what-is-not-covered)).

| # | Promise | Level | State |
|---|---|---|---|
| P1 | Routing, TLS, SSO and landing tile through NebariApp | stable | partly built |
| P2 | Several NebariApps per pack | stable | planned |
| P3 | Default storage class | stable | partly built |
| P4 | Postgres through CloudNativePG | stable | built |
| P5 | Platform facts (`global.nebari` Helm values) | stable | planned |
| P6 | Groups claim as full paths | stable | partly built |
| P7 | Trust bundle | stable | partly built |
| P8 | Argo CD project for packs | stable | built |
| P9 | Telemetry endpoint | stable | partly built |
| P10 | Machine clients | experimental | planned |
| P11 | API clients with bearer tokens through the gateway | experimental | planned |
| P12 | Logged-in identity passed to the app | experimental | planned |
| P13 | Per-route timeouts, streaming and buffer size | experimental | planned |
| P14 | Generated secrets stable under Argo CD | experimental | planned |
| P15 | GPU node label and taint | experimental | planned |
| P16 | NetworkPolicy enforced | experimental | planned |
| P17 | Bucket binding | experimental | planned |
| P18 | Internal certificates | experimental | planned |

**Entry rules.** A capability enters the contract only when it has a proven need (two packs, or one plus a clear near-term need), works on every target (in-cluster where a cloud lacks it), has a stable, implementation-neutral interface (a Kubernetes or open standard, or a Nebari-owned abstraction, never one product's API; a promise bound to one product is accepted only when no neutral interface exists yet, and is marked and given a path to one), has a conformance test on every target, and fits the versioning rules. Details are in [Proposals and governance](../pack-contract/policy/proposals.md).

**Levels.** Stable promises may be relied on without reservation. Experimental promises are tested on every target once built, but carry no stability guarantee: they may change or be removed in any contract version, without notice, and packs rely on them at their own risk. Within 18 months of being built, each is graduated, renewed with a reason, or retired.

**Versioning and support.** The contract is versioned with EffVer, like the Pack Specification and the rest of nebari-dev: the version measures the effort an upgrade costs a pack author. It ships as `0.Macro.Micro` while any required promise is not fully built and becomes 1.0 when all are built and pass conformance. Each contract version names the Pack Specification version it includes, and each NIC release implements exactly one contract version. As with Go releases, a release line is supported until two newer lines exist, and for at least 12 months after the next one. Packs will declare the contract version they target in `pack-metadata.yaml`, next to the Pack Specification version they already declare, the way a Go module declares its `go` version (planned). See [Versioning and support](../pack-contract/policy/versioning.md).

**Compatibility.** Following the Go 1 compatibility promise, a pack that uses only stable promises keeps working, unchanged, on every later contract version, from the version in which each promise is built. The only exceptions are security, behavior the contract does not specify, errors in the contract, bugs, and upstream end of life. See [Compatibility](../pack-contract/policy/compatibility.md).

**Deprecation and behavior changes.** Deprecated stable promises stay, marked on their page and in release notes, and, once built, reported at runtime. Stable behavior that must change does so behind a compatibility setting, like a GODEBUG setting, kept for at least two years, with defaults that follow the contract version a cluster or pack declares. See [Deprecation and behavior changes](../pack-contract/policy/deprecation.md).

**Security exceptions.** A promise may break outside the normal process only when keeping it would leave a boundary open and no compatible fix exists. The fix is minimal, applies to every pack, ships in patch releases of every supported version, and comes with an advisory; reports follow PUBLIC, PRIVATE and URGENT tracks adapted from Go's security policy. See [Security exceptions](../pack-contract/policy/security.md).

**Adoption and governance.** The contract is behavior in NIC that packs rely on, so under the Nebari governance (RFD #59) every change to what it promises is a platform RFD in `nebari-dev/governance`, decided by Core, except changes to experimental promises, which need only a pull request. New capabilities must meet the entry rules, enter as experimental unless they document existing behavior, and graduate through an RFD; the first contract version sets its own levels, so P2, P5 and P6 start as stable. Implementation designs live in the repository that owns the change. A Go test that runs in CI checks the level, state, change record, graduation clock and conformance test of every promise page, so these rules hold without anyone having to remember them. This ADR is itself accepted through a platform RFD. See [Proposals and governance](../pack-contract/policy/proposals.md).

**Conformance.** The contract covers a cluster only when NIC installs and manages every foundational component on it. A partial or a la carte installation, where any foundational component is left out or replaced, is outside the contract: some promises may hold there, but packs cannot rely on them. That limit exists because the conformance suite is the only proof and it is not built yet. The goal is that once it is, a cluster is covered when the suite passes on it, whatever software implements the foundational pieces; promises bound to one product still need that product. A cluster conforms to a contract version when that version's suite passes on it. NIC will check the requirements it can observe on existing clusters before installing anything (planned). Packs stay inside the compatibility promise by using only contract interfaces and meeting the obligations in [Conformance](../pack-contract/policy/conformance.md).

**ADR-0007.** If this ADR is accepted, [ADR-0007](0007-cloudnativepg-managed-databases.md) is accepted with an amendment: the stable promise is the CloudNativePG `postgresql.cnpg.io/v1` API used directly (P4), the NebariApp `database` field is deferred to a later experimental promise built on top of P4, and backups to object storage are not promised. The API comes first for four reasons. It is built today on every target, and the field is not. Unlike a Keycloak client, which needs admin credentials a pack must never hold, a CNPG Cluster in the pack's own namespace needs no extra privilege, so routing it through the operator adds no protection. A stable field would fall under the compatibility promise forever while packs need pooling, extra databases, extensions and sizing that a thin field cannot cover, and a thick one would reimplement the CNPG Cluster spec inside NebariApp. And a pack that uses CNPG directly runs on any cluster with CNPG. The field still has real value (one line instead of a manifest, platform defaults for sizing, monitoring and later backups, and a credentials Secret with fixed keys), so it is listed as a candidate experimental promise that graduates only if packs adopt it. It is also P4's path to an implementation-neutral interface: P4 is bound to CloudNativePG, and a NebariApp field would let another Postgres operator satisfy it.

### Consequences

**Good:**

- A pack author reads one reference to know what Nebari provides, what it does not, what not to rely on, and how all of that will change.
- Each promise sits next to the code and the conformance test that keep it, so the document cannot drift from reality without a test going red.
- Packs can drop the workarounds found today: hardcoded service names, CoreDNS patches, `verifyCert=false`, master admin credentials, bundled Postgres and hand-made Envoy policies.
- Breaking changes become rare, announced and reversible per cluster for a stated time, and security fixes have a known path.
- Moving to a separate repository later (Option 3) does not break any pack.
- Interfaces are standards or Nebari-owned abstractions, so foundational software can be swapped without changing packs. The two promises bound to a product (P4 to CloudNativePG, P8 to Argo CD) are marked and each has a path to a neutral interface.

**Bad:**

- The NebariApp chapter lives in another repository, so the contract has cross-repo links. Pinning the operator spec version in the reference makes the dependency explicit.
- Every stable promise adds a permanent conformance test on every target, which costs CI time, and the suite for each contract version is kept.
- A strong compatibility promise makes every stable promise expensive to change. Mistakes in a stable interface are carried, deprecated, indefinitely, so the contract has to stay small.
- Compatibility settings add code paths that must be kept and tested for at least two years each.
- While the contract is 0.x, pack authors depend on a document that still lists planned parts.
- Platform facts reach a pack only through how the pack is installed, so P5 depends on #670, and a pack installed by hand must load the platform value file itself. A pack that wraps an upstream chart that does not template its values needs a small shim (an object it renders from `global.nebari`) to pass the facts on.
- Until the conformance suite can verify substituted software, restricted installations that bring their own identity provider, gateway or other foundational software get no guarantee. Packs may still work there, but that is between the pack and the admin.

## Options Detail

### Option 1: Extend the operator spec to cover everything

`nebari-operator/docs/pack-specification.md` grows from NebariApp to the whole platform.

**Pros:**
- Pack authors already read it and it is already normative.

**Cons:**
- The operator does not build most of the promises. Storage classes, CloudNativePG, the trust bundle, the Argo CD project, telemetry and every per-target difference live in NIC, so the contract would describe things its repository cannot test.
- Its version would be tied to operator releases.

### Option 2: NIC owns the contract; the operator spec is one chapter (chosen)

An ADR and a reference in NIC own the contract, with the NebariApp chapter pinned from the operator spec.

**Pros:**
- Each promise sits next to the code and the end-to-end test that keep it.
- Conformance extends the existing ADR-0017 journey suite.

**Cons:**
- Cross-repo links to the operator spec, made explicit by the pinned spec version.

### Option 3: A separate `nebari-dev/pack-contract` repository

Spec and conformance suite versioned on their own; NIC and the operator declare which contract version they implement.

**Pros:**
- Cleanest versioning, and the natural home once packs declare contract versions.

**Cons:**
- Adds a repository and a release process before there is a 1.0 to version.
- Option 2 can move to this later without breaking any pack.

## Links

- [Pack contract reference](../pack-contract/README.md)
- [ADR-0007](0007-cloudnativepg-managed-databases.md) (amended if this ADR is accepted)
- [ADR-0008](0008-otel-collector-software-pack-override-point.md) (the collector override is not part of the pack contract)
- [ADR-0014](0014-helm-valuefiles-overlay-seam.md) (the values overlay seam is provided, not promised)
- [ADR-0017](0017-user-journey-tests-for-foundational-software.md) (conformance extends this suite)
- [Pack specification (nebari-operator)](https://github.com/nebari-dev/nebari-operator/blob/main/docs/pack-specification.md)
- [Go 1 compatibility promise](https://go.dev/doc/go1compat)
- [Go GODEBUG settings](https://go.dev/doc/godebug)
- [Go release policy](https://go.dev/doc/devel/release#policy)
- [Go security policy](https://go.dev/doc/security/policy)
- [Go proposal process](https://github.com/golang/proposal#readme)
