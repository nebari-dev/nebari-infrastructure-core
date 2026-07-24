# ADR-0011: Provider Credential Validation Standard

## Status

Proposed

## Date

2026-07-24

## Context

Credential and permission validation in NIC is currently ad-hoc, and the design
for a unified approach is spread across several issues and two closed pull
requests with no ratified decision:

- **#6** "Implement credential validation in AWS provider" - a detailed AWS
  proposal (STS identity check + IAM Policy Simulator, run always-on before
  every operation). Open, no comments, never ratified.
- **#61** (closed, stale) - implemented #6 as an optional `CredentialValidator`
  interface plus a `--validate-creds` flag. Closed "as stale", not on the merits.
- **#182** (closed, superseded by #504) - the DNS analogue. Established the
  two-phase model (offline config always; live credentials opt-in) and was
  explicitly held pending #6.
- **#137 / #111** - track the DNS `Validate` method and early DNS credential
  validation.
- **#54** - `--gen-perms` flag, which wants a config-aware required-permission
  list and depends on #6.
- **#423** - openly questions whether validation should be per-provider
  `Validate` or something more general.
- **#159** "Epic: Config Standardization and Validation" - the umbrella.

The offline config-validation half shipped in **#504** (DNS zone consistency on
`dns.Provider.Validate`, with a `ValidateOptions{CheckCreds bool}` whose
credential branch is a deliberate no-op stub). Every live-credential path is
still deferred, waiting on the standard this ADR proposes.

Current state in code:

- Cloud `Provider.Validate(ctx, projectName, clusterConfig)` has no options
  parameter (`pkg/providers/cluster/provider.go`).
- AWS `Validate` calls `sdkCfg.Credentials.Retrieve(ctx)` - it resolves the
  credential chain but makes no API call, so it cannot detect expired or
  unauthorized credentials (`pkg/providers/cluster/aws/provider.go`).
- `sts:GetCallerIdentity` is already wired (client interface, mock, and a
  `getAccountID` caller in `pkg/providers/cluster/aws/state.go`) - it is simply
  not used by `Validate`.
- No IAM Policy Simulator code exists anywhere.
- DNS `Validate` already carries `ValidateOptions{CheckCreds bool}` with a
  stubbed credential branch (`pkg/providers/dns/cloudflare/provider.go`).

We need a single standard that unifies cloud and DNS providers, preserves the
offline `nic validate` contract, gives users fast and actionable failures on
bad credentials, and unblocks `--gen-perms` (#54).

## Decision Drivers

- **Fast, actionable failures.** An expired or unauthorized credential must fail
  before provisioning, with a specific message - not a cryptic mid-deploy error.
- **`nic validate` stays offline.** Its contract is no network calls and no
  credentials unless explicitly requested.
- **Consistency across provider families.** Cloud and DNS providers should
  validate through the same shape; the DNS side already shipped one.
- **Boundary abstraction.** Providers must receive all data from the
  orchestrator and never reach into the registry themselves.
- **Never block a deploy that would succeed.** A permission pre-check with false
  negatives (from SCPs, permission boundaries, resource policies) is worse than
  no check if it hard-fails.
- **Do not duplicate the permission list.** The list feeding a permission check
  and the list feeding `--gen-perms` must be one source of truth.

## Considered Options

1. Separate `CredentialValidator` interface, opt-in (the #61 approach).
2. Fold credential checks into each provider's existing `Validate` via a shared
   `ValidateOptions{CheckCreds bool}` (the approach the DNS side already shipped).
3. A dedicated `RequiredPermissions` method as the primary mechanism (#54).

## Decision Outcome

Chosen: **Option 2 as the primary gate, composed with Option 3 as an optional
companion; Option 1 rejected.**

The standard, in one paragraph: every provider validates through its own
`Validate(ctx, cfg, opts)`; `opts.CheckCreds` toggles a two-tier live check - a
cheap, permission-free identity probe (`GetCallerIdentity`) that runs
automatically before any *mutating* command and hard-fails on bad or expired
credentials, plus an opt-in, advisory permission check (IAM Policy Simulator,
fed by an optional `RequiredPermissions` interface shared with `--gen-perms`)
that warns rather than blocks. `nic validate` stays offline unless
`--check-creds` is passed. Providers receive all data from the orchestrator and
never touch the registry.

### The two tiers

| Tier | Mechanism | Cost | Detects |
|------|-----------|------|---------|
| 1 - identity | `sts:GetCallerIdentity` (AWS); token whoami (others) | 1 call, needs no IAM permission | expired / invalid / unset credentials |
| 2 - access | IAM Policy Simulator, fed by `RequiredPermissions` | many calls, needs `iam:SimulatePrincipalPolicy`, false negatives from SCPs | missing permissions |

### Command bindings

```
nic validate                    -> config only (offline, no network)
nic validate --check-creds      -> config + tier-1 + tier-2 (advisory);
                                   sets CheckCreds=true for BOTH the cluster
                                   provider and the DNS provider Validate
nic deploy|destroy|reconcile    -> config + tier-1 always (fail-fast);
                                   tier-2 only when --check-creds is passed
```

### Interface changes

Add an options parameter to the cloud provider interface, mirroring DNS:

```go
// pkg/providers/cluster/provider.go
Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts ValidateOptions) error

type ValidateOptions struct {
    CheckCreds bool
}
```

Add an optional companion interface implemented only by providers for which it
is meaningful (AWS yes; local/kind and existing no), discovered by type
assertion:

```go
type RequiredPermissioner interface {
    RequiredPermissions(ctx context.Context, clusterConfig *config.ClusterConfig) ([]string, error)
}
```

`Validate` is the gate; `RequiredPermissions` is the data source that both
tier-2 and `nic --gen-perms` (#54) consume, so the permission list is defined
once.

### Permission-check policy (tier-2)

- Results are **warnings by default**, not errors. Hard-fail only under an
  explicit strict mode (e.g. `--check-creds --strict`).
- If `iam:SimulatePrincipalPolicy` is itself denied, **degrade gracefully**:
  warn "could not verify permissions (simulate not permitted) - proceeding"
  rather than failing.
- Tier-1 remains the authoritative hard gate; tier-2 is advisory.

### Consequences

**Good:**

- Reconciles #6 (always-on intent, satisfied by tier-1 on mutating commands),
  the offline-`validate` contract, and the opt-in-network posture of #61/#182.
- Fixes the expired-credential UX by swapping `Retrieve` for `GetCallerIdentity`
  (scaffolding already present).
- One validation entry point per provider; consistent with the shipped DNS side.
- Unblocks #54 and answers the architecture question in #423.
- No duplicated permission list.

**Bad:**

- Widens the cloud `Provider.Validate` signature - a breaking change to the
  interface that every cluster provider implementation must absorb (mechanical).
- Maintaining a hand-curated, config-aware IAM permission list is ongoing work
  and will drift from what the OpenTofu modules actually require.
- Policy Simulator ignores SCPs, permission boundaries, and resource-based
  policies, so tier-2 can mislead; keeping it advisory mitigates but does not
  eliminate this.

## Options Detail

### Option 1: Separate `CredentialValidator` interface (rejected)

Providers opt into a distinct interface (the #61 design).

**Pros:**
- Keeps credential logic out of `Validate` for providers that do not need it.
- Matches #6's original framing.

**Cons:**
- Fragments validation into two entry points when every provider already has
  `Validate`.
- Predates and diverges from the DNS `Validate`-with-opts pattern already
  shipped in #504; adopting it now would make the two provider families
  inconsistent.

### Option 2: Fold into `Validate` via `ValidateOptions` (chosen, primary)

Add `opts ValidateOptions{CheckCreds bool}` to the provider `Validate` methods.

**Pros:**
- One pattern across cloud and DNS; DNS already ships it.
- Orchestrator passes `opts` in - respects the boundary principle.
- Single validation entry point per provider.

**Cons:**
- Breaking signature change to the cloud interface.

### Option 3: `RequiredPermissions` method (chosen, as optional companion)

A separate, optional interface returning the config-aware permission list.

**Pros:**
- Single source of truth shared by tier-2 and `--gen-perms` (#54).
- Optional via type assertion, so providers without a permission model
  (local/kind, existing) need not implement it.

**Cons:**
- Only useful alongside a gate; not a complete solution on its own, hence
  composed with Option 2 rather than used alone.

## Implementation Plan

Phased, each phase independently shippable:

1. **Cloud interface + tier-1 (the core value).**
   - Add `ValidateOptions{CheckCreds bool}` and widen cloud
     `Provider.Validate(..., opts)`. Update all cluster provider
     implementations and callers.
   - In AWS `Validate`, replace `Credentials.Retrieve` with a
     `GetCallerIdentity` call (reuse the existing STS client) so expired or
     invalid credentials fail with a specific message.
   - Wire the orchestration: `deploy`/`destroy`/`reconcile` always run tier-1;
     `validate` runs it only when `--check-creds` is set.

2. **`--check-creds` flag.**
   - Add `--check-creds` to `nic validate` (and accept it on `deploy`), setting
     `CheckCreds=true` for both the cluster-provider and DNS-provider `Validate`
     calls. This also flips the DNS credential branch from stub to real for
     Cloudflare (the remaining half of #137).

3. **Tier-2 + `RequiredPermissions` (advisory).**
   - Add the optional `RequiredPermissioner` interface; implement it for AWS
     (config-aware list, EFS entries only when enabled).
   - Implement the Policy Simulator check as advisory/warn-by-default with
     graceful degradation when simulate itself is denied.

4. **`--gen-perms` (#54).**
   - Implement `nic --gen-perms` reusing `RequiredPermissions`.

## Open Questions To Ratify

These are genuine judgment calls the team should decide before or during
implementation; this ADR records a recommended default for each:

1. **Always-on tier-1 on `destroy`?** Users may legitimately want to tear down
   with degraded credentials. Recommendation: tier-1 is warn-only (not a hard
   gate) specifically on `destroy`, hard-gate on `deploy`/`reconcile`.
2. **Is a hand-curated IAM permission list worth the maintenance,** given SCP
   false negatives? Recommendation: yes, but only as an advisory tier-2 and as
   the `--gen-perms` source; revisit if drift becomes painful.
3. **Confirm the always-on-vs-opt-in split.** Verbal discussion in JATIC syncs
   reportedly favored pre-command credential validation; that has not been
   recorded in the tracker and should be confirmed by the maintainers who were
   in the room, not treated as settled.

## Links

- [ADR-0004: Out-of-Tree Provider Plugin Architecture](0004-out-of-tree-provider-plugins.md)
- [ADR-0005: nic config CLI surface](0005-nic-config-cli-surface.md)
- Issues: #6, #54, #111, #137, #159, #423
- Pull requests: #504 (offline half, merged direction), #61 and #182 (closed prior attempts)
