# ADR-0010: High-Security Mode (Opt-In "Whitelist Everything You Install" Hardening)

## Status

Proposed (2026-07-15), DRAFT

Builds on the "GitOps for software" principle in [ADR-0001](0001-git-provider-for-gitops-bootstrap.md) and the foundational-software split in [ADR-0006](0006-conditional-foundational-software-helm.md). Records the target posture that the v0.9.0 security-audit remediation (epic #472) is converging toward. The ArgoCD AppProject scoping (#458) is the first step of this posture; the admission controller (#480) is one of its mechanisms.

## Date

2026-07-15

## Context

NIC's default posture is highly privileged, and that is a property of the model rather than a bug:

- **GitOps means repo write is cluster-admin-equivalent.** ArgoCD applies whatever lands in the tracked repository, with automated prune and self-heal. Anyone who can write to the GitOps repo can deploy arbitrary cluster resources.
- **Software packs are arbitrary workloads.** The `nebari-apps` AppProject (#458) that hosts packs must be permissive by necessity: packs create their own namespaces and arbitrary resources, so its scope cannot be tightened without knowing the workloads in advance.
- **The AppProject scoping in #458 is defense-in-depth, not a hard boundary.** It blocks content from unapproved repos, confines foundational apps to known namespaces, and closes the `default`-project escape hatch. It does not stop malicious content committed to an approved repo, because the resource-kind whitelists stay open.

This default is correct for usability: NIC must work out of the box with no operator configuration. But a class of operators (regulated, government, and other high-assurance environments) runs a known, fixed set of workloads and needs to trade convenience for a genuinely locked-down cluster.

The v0.9.0 security audit surfaced a cluster of findings that, taken individually, each read as "harden this," but taken together describe a single posture choice rather than a pile of unrelated toggles:

- H-02: the foundational ArgoCD project was wildcard (addressed at the standard level by #458).
- M-02: deployment fails open (reports success before security services are installed).
- M-04: lenient config parsing silently ignores security-relevant fields.
- M-05: cloud control-plane defaults are open to the internet.
- M-06: OpenTelemetry ingestion is unauthenticated and the collector is over-privileged.
- M-08: third-party deployment inputs are versioned but not digest-immutable.

Each has a "make it stricter" answer that is the right default for nobody who values convenience and the right default for everybody who values assurance. That tension is what this ADR resolves.

## Decision Drivers

- The default posture must stay usable with zero operator configuration. Hardening cannot be the default.
- Hardening must be opt-in and coherent: an operator should be able to say "I am in hardened mode" and reason about what that guarantees, rather than assembling a correct combination of independent flags.
- The guiding principle for the hardened posture is "explicitly whitelist everything you install": in hardened mode, nothing deploys unless the operator has named it (its repos, its namespaces, its resource kinds).
- The posture must span NIC beyond ArgoCD: cloud control-plane exposure, telemetry, configuration strictness, and deploy semantics are all part of it.
- It must be enforcement, not just a documentation guide. A prose "how to harden" page is necessary but not sufficient.

## Considered Options

1. **Always-hardened default.** Make the strict posture the only posture. Rejected: it breaks out-of-the-box usability, forces every operator to enumerate their workloads/namespaces/repos before anything deploys, and imposes ongoing maintenance (enumerated resource kinds drift on every chart bump) on users who do not need it.
2. **Independent per-control flags.** A separate toggle for each hardening control (`strict_config`, `private_control_plane`, `require_digests`, and so on). Rejected: no coherent posture emerges, operators cannot easily answer "am I hardened," the configuration surface grows combinatorially, and it is easy to end up partially hardened without realizing it.
3. **Documentation-only hardening guide.** Ship a "how to lock down NIC" doc and no enforcement. Rejected on its own: no guarantees, easy to misconfigure, and the audit findings ask for controls, not only prose. (A hardening guide is still produced, as the human-readable companion to the enforced mode.)
4. **A single `security_level` config that ratchets a coherent set of controls.** Chosen. See below.

## Decision Outcome

Chosen option: **Option 4.** Introduce a single `security_level` configuration field with three levels, forming a ladder from convenience to assurance:

- **`standard`** (default): today's behavior. The #458 AppProject scoping (foundational scoped, `nebari-apps` for packs, `default` deny-all), permissive where packs require it, usable with no extra configuration. This is the honest "privileged but sane" default.
- **`hardened`** (opt-in): enforces "explicitly whitelist everything you install" across NIC. Less convenient, materially more secure, intended for operators who know their workloads.
- **`permissive`** (opt-in, development and testing only): relaxes the pack/user side of the standard posture so a developer can deploy packs from arbitrary repositories without configuring an allow-list. Explicitly insecure and guarded against production use. See "Permissive mode" below.

The table below contrasts `standard` (the default) with `hardened`. `permissive` is defined as a small delta from `standard`, described separately, because it loosens only two things and leaves everything else at the standard posture.

In `hardened` mode, NIC enforces the following controls. Each maps to an audit finding and is the strict counterpart of a standard-mode default:

| Area | Standard (default) | Hardened | Audit finding |
|------|--------------------|----------|---------------|
| ArgoCD foundational project | resource kinds `*` | enumerated resource-kind whitelist (no `*`) | H-02 (#458) |
| ArgoCD `nebari-apps` project | namespaces `*`, default pack repos | operator-declared pack namespaces and repos only | H-02 (#458) |
| Workload admission | none | admission policy required (block privileged pods, hostPath, host namespaces, unapproved cluster RBAC) | #480 |
| Deploy semantics | fail-open (warns, continues) | fail-closed (any required-service failure aborts non-zero) | M-02 |
| Config decoding | lenient (unknown fields dropped) | strict (unknown/misplaced fields rejected) | M-04 |
| Cloud control-plane | open defaults allowed with a warning | private endpoints or explicit CIDR allowlists required; all-internet refused; dedicated control-plane nodes | M-05 |
| Telemetry | plaintext OTLP, debug exporter, broad RBAC | OTLP mTLS/auth required, no debug exporter, `nodes/proxy` dropped, NetworkPolicies | M-06 |
| Supply chain | version-pinned charts/images | charts and images pinned by digest; provenance/signature verification | M-08 |
| GitOps repo access | operator responsibility (documented) | documented and checked expectation: branch protection, signed commits, restricted writers | (cross-cutting) |

The last row reflects the central truth from #458's security model: because repo write is cluster-admin-equivalent, no in-cluster control substitutes for governing who can write to the GitOps repo. Hardened mode makes that expectation explicit rather than implicit.

### Permissive mode (development and testing only)

`permissive` exists because the "explicitly whitelist everything" principle creates real friction for a developer iterating on a pack from an arbitrary or throwaway repository, which standard mode's pack-source allow-list would reject. It is defined as a deliberately small, well-bounded relaxation of `standard`:

- `nebari-apps.sourceRepos` becomes `*` (packs may come from any repository).
- The built-in `default` project is left usable rather than deny-all, so an Application that omits `project:` still syncs.

Everything else stays at the standard posture. In particular, **`foundational` remains scoped in permissive mode** (its derived repos/namespaces are unchanged), because nothing about development requires loosening NIC's own control plane. Permissive is a user/pack-side convenience, not a revert to the pre-#458 wildcard posture.

Guardrails, because a permissive cluster is exactly the exposure the audit flagged:

- It is never a default, not even for the local provider. It must be explicitly selected, so it cannot be shipped by accident.
- Selecting it emits a prominent, repeated warning that the cluster is insecure and for development/testing only.
- On cloud providers it is refused (or, at minimum, gated behind an explicit "I understand this is insecure" acknowledgement), since a permissive cloud deployment reintroduces the H-02 hole on an internet-reachable cluster.

Much of the deliberate case (a known additional pack repository) is better served by the planned multi-repo pack configuration, which adds a specific repo to `nebari-apps.sourceRepos` without opening it to `*`. Permissive is for the throwaway/iterate case where configuring anything is not worth it.

### Consequences

**Good:**
- A real, auditable hardened posture for operators who need it, expressed as one switch they can reason about.
- The audit's scattered "harden this" findings are unified under a single, coherent, opt-in decision instead of N independent toggles.
- The usable `standard` default stays intact for the majority who need out-of-the-box usability, so hardening never becomes a barrier to first use.
- A bounded `permissive` mode removes the pack-iteration friction for developers without a full wildcard revert, and gives that insecurity an explicit, visible name instead of leaving developers to hand-wildcard projects (a habit that leaks toward production).
- Each control has a clear standard/hardened pair, which makes the security model easy to document and to test.

**Bad:**
- Less convenient in hardened mode: the operator must declare workloads, namespaces, repos, and CIDRs up front, and keep them current.
- More configuration and ongoing maintenance: enumerated resource kinds and pinned digests drift as charts change and must be refreshed.
- Some controls (namespace and repo whitelists, control-plane CIDRs) require per-deployment knowledge NIC cannot infer.
- `permissive` is an intentionally insecure mode; its safety depends on the guardrails above (never a default, loud warnings, refused on cloud). If those guardrails are weak, it becomes a production footgun that reintroduces H-02.
- Three code paths (`permissive`, `standard`, `hardened`) to build and test where a control differs across them.

## Scope and Phasing

This ADR records the decision and the target shape; it is deliberately not a single pull request.

- **Step one (shipping now):** the standard posture, delivered by #458 (ArgoCD project scoping) plus the honest security-model documentation.
- **Incremental hardened controls:** the admission policy (#480), digest pinning (M-08), cloud control-plane strictness (M-05), telemetry hardening (M-06), strict config decoding (M-04), and fail-closed deploy (M-02) each land as their own scoped work, on the hardened side of the switch.
- **The `security_level` knob is introduced only once `hardened` genuinely ratchets at least one control.** Shipping the flag while `hardened` is indistinguishable from `standard` would be a hollow abstraction. Until the first hardened control exists, a written hardening guide documents the manual path.

## Relationship to the Security Audit (epic #472)

This ADR is the umbrella that gives the audit's medium-severity hardening findings a coherent home. It does not itself close any finding; it defines the posture into which their fixes land. The standard posture (#458) and the admission controller (#480) are the first two concrete pieces.
