# ADR-0016: OpenTofu Runtime Version Policy (External Binaries and Compatibility Window)

## Status

Proposed (2026-08-12)

Records the policy implemented by [#576](https://github.com/nebari-dev/nebari-infrastructure-core/pull/576) (closes [#554](https://github.com/nebari-dev/nebari-infrastructure-core/issues/554)), part of the pixi/prefix.dev distribution epic [#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552). Reverses the "callers never look up tofu in `PATH`" invariant previously recorded in [Key Decisions §4.3](../design-doc/architecture/04-key-decisions.md).

## Date

2026-08-12

## Context

NIC shells out to OpenTofu for the declaratively provisioned providers (AWS, Azure). Until #576, the binary was always NIC's own: downloaded on first use at the version pinned in `pkg/tofu/version.go`, GPG-verified by `tofudl`, and cached under `os.UserCacheDir()/nic/tofu/`. The design docs stated this as an invariant — there was deliberately no `PATH` lookup.

Three pressures broke that invariant:

- **Packaged distribution ([#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552)).** NIC is distributed via the prefix.dev `github-releases` channel, and a pixi workspace can declare `opentofu` as a first-class dependency. A packaged tool that phones home to download a second copy of a dependency the package manager already installed is wrong on ergonomics, and unacceptable in air-gapped environments.
- **Version skew is the steady state, not the exception.** The pin has moved once in the project's history (1.11.2 → 1.11.3 in February 2026), while conda-forge ships 1.12.5. Any policy that requires an external binary to exactly match the pin would reject essentially every externally installed OpenTofu, defeating the purpose.
- **Air-gapped operation.** Before #576, the only options were pre-seeding the download cache or waiting out a 10-minute network timeout. An explicit "use this binary" override is the direct remediation.

This forces two decisions that this ADR records: **which external OpenTofu versions NIC accepts**, and **how failures are treated at each tier of resolution**.

## Decision Drivers

- Default behavior must not change: with no external binary present, NIC downloads its pinned version exactly as before, so nothing breaks for existing users.
- A stale system `tofu` on `PATH` predates NIC and must not break a deploy the user never asked it to participate in.
- An explicit `NIC_TOFU_PATH` override is stated operator intent; silently substituting a different binary would mask misconfiguration — precisely in the air-gapped environments where the override matters most.
- Exact version pinning for external binaries is self-defeating (see Context); the contract must be a range.
- OpenTofu follows semantic versioning: minor releases within 1.x preserve CLI and state compatibility, while 2.0 is where breaking changes are fair game.
- Downloaded binaries are integrity-verified (tofudl GPG); external binaries cannot be. The version gate must not be mistaken for a trust boundary.

## Considered Options

1. Exact pin match: external binaries accepted only at `pkg/tofu.Version`
2. Compatibility window: floor at a minimum supported version, exclusive cap at the next major version
3. No version gate: accept any binary that identifies itself as OpenTofu

## Decision Outcome

Chosen option: **Option 2 — a compatibility window `[MinVersion, 2.0.0)`**, currently `[1.11.3, 2.0.0)`, with severity split by resolution tier.

**Resolution order** (implemented in `pkg/tofu/resolve.go`, documented in [Packaging and External Binaries](../operations/packaging.md)):

1. `NIC_TOFU_PATH` — explicit override. Any problem (missing, directory, not executable, not OpenTofu, outside the window) is a **hard error**. NIC never falls back to another binary when the operator has named one.
2. `tofu` on `PATH` — used if compatible. Any problem is a **warning plus fallback to download**. This deliberately diverges from the "hard error below the floor" wording of [#554](https://github.com/nebari-dev/nebari-infrastructure-core/issues/554): discovery is not operator intent, and hard-erroring on a stale system tofu would break existing users, contradicting "the download stays the default so nothing breaks."
3. Download — unchanged: the pinned `pkg/tofu.Version`, GPG-verified, cached.

**Window semantics:**

- The floor is `pkg/tofu.MinVersion` (1.11.3, currently equal to the pinned download version). The cap is exclusive at 2.0.0, where OpenTofu may break CLI or state-format compatibility.
- Pre-releases are compared by their base version (`Core()`): `1.11.3-rc1` is accepted (it carries the features NIC needs from 1.11.3), `2.0.0-beta1` is rejected (it previews 2.0 breaking changes).
- An in-window version that differs from the pin produces an info-level notice naming the version NIC is tested against — info, not warning, because differing is the normal steady state for packaged installs.
- Binaries must identify themselves as OpenTofu via the `tofu version` banner. Terraform is rejected explicitly: the committed lockfiles pin `registry.opentofu.org` providers that Terraform would silently re-resolve against `registry.terraform.io`.

**Maintenance policy:**

- `MinVersion` must always be ≤ the pinned `Version` (a test pins `compatibleVersion(Version)`). Bumping the download pin does not automatically raise the floor; raising the floor rejects binaries that previously worked and must be a deliberate, changelog-worthy decision.
- The CI lockfile workflow's OpenTofu version is a separate concern from the runtime floor: `tofu providers lock` output is determined by provider constraints in the templates, not the generating binary's version. The workflow nevertheless runs at 1.11.3 (bumped from a stale 1.9.0 in this ADR's PR) so the repository holds one opinion about acceptable versions.
- The version gate is a correctness check, not a trust boundary. External binaries are executed unverified (the probe itself runs the binary). Whether high-security mode ([ADR-0010](0010-high-security-mode.md)) should disable `PATH` discovery — the only tier that is neither integrity-checked nor explicit operator intent — is tracked as a follow-up to that ADR.

### Consequences

**Good:**

- Packaged and air-gapped installs work without network access, and `nic version` diagnoses exactly which binary would run and why others were rejected.
- Default behavior is byte-for-byte unchanged when no external binary is present.
- conda-forge/pixi's current OpenTofu (1.12.5) is usable today without waiting for NIC's pin to catch up.
- Misconfigured overrides fail fast and loudly, before any cloud resources are created.

**Bad:**

- NIC now executes binaries it did not verify; the supply-chain posture for external binaries is delegated to the operator (documented in the packaging guide).
- "NIC is tested against 1.11.3" is a weaker statement than "NIC runs exactly 1.11.3"; an OpenTofu regression in an untested in-window version becomes NIC's support burden.
- Two more configuration surfaces (`NIC_TOFU_PATH`, `PATH` discovery) that support requests must now rule out; mitigated by `nic version` reporting the resolved source.

## Options Detail

### Option 1: Exact pin match

External binaries accepted only when their version equals `pkg/tofu.Version`.

**Pros:**

- NIC runs only the version it is tested against.
- Simplest possible contract.

**Cons:**

- Rejects conda-forge's 1.12.5 against NIC's 1.11.3 pin — the primary packaging use case fails on day one.
- Couples every external install to NIC's release cadence, which has historically moved the pin once in six months.

### Option 2: Compatibility window (chosen)

Floor at `MinVersion`, exclusive cap below 2.0.0; severity split between override (hard error) and discovery (warn + fallback).

**Pros:**

- Matches OpenTofu's semver stability contract: 1.x minors are compatible, 2.0 is not guaranteed.
- Works with the versions package managers actually ship.
- The severity split preserves both guarantees that matter: explicit intent is never silently overridden, and existing users are never broken by a binary they didn't choose.

**Cons:**

- NIC runs versions it has not explicitly tested (accepted; mitigated by the tested-against notice and the state-format argument below).
- The floor requires occasional deliberate maintenance.

### Option 3: No version gate

Accept any binary identifying as OpenTofu.

**Pros:**

- Zero maintenance; no floor to keep current.

**Cons:**

- OpenTofu releases older than 1.11 predate features NIC's templates rely on; failures would surface deep inside `tofu apply` instead of at resolution time.
- A 2.x binary could break CLI-flag or state-format expectations mid-deploy, the worst possible place to discover incompatibility.

## Links

- [PR #576 — implementation](https://github.com/nebari-dev/nebari-infrastructure-core/pull/576)
- [#554 — external binary issue](https://github.com/nebari-dev/nebari-infrastructure-core/issues/554)
- [#552 — pixi/prefix.dev distribution epic](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552)
- [#558 — same treatment for hetzner-k3s](https://github.com/nebari-dev/nebari-infrastructure-core/issues/558)
- [#241 — redundant tofu work per invocation (memoization follow-up)](https://github.com/nebari-dev/nebari-infrastructure-core/issues/241)
- [Packaging and External Binaries](../operations/packaging.md) — operator-facing contract
- [ADR-0010](0010-high-security-mode.md) — candidate home for disabling `PATH` discovery in hardened mode
