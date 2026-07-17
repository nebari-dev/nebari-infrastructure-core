# ADR-0009: Declarative Keycloak Configuration via keycloak-config-cli

## Status

Accepted

This ADR records a decision that was made and accepted before it reached an ADR. The declarative approach was proposed and accepted as a design in [PR #154](https://github.com/nebari-dev/nebari-infrastructure-core/pull/154) (closed without merge after the design was agreed, per the rewrite on the `design/declarative-keycloak-config` branch), and its Phase 1a implementation is [PR #289](https://github.com/nebari-dev/nebari-infrastructure-core/pull/289). Neither PR produced an ADR, so this document captures the decision so it is not lost.

## Date

2026-07-15

## Context

NIC bootstraps a `nebari` Keycloak realm during foundational-software install: realm settings, `admin`/`user` realm roles, an admin user, a `groups` client scope plus an `oidc-group-membership-mapper` (load-bearing, because operator-managed app authorization keys off group membership via `NebariApplication.allowedGroups`), the `argocd` OIDC client, and the `argocd-admins`/`argocd-viewers` groups.

Today this is done by an imperative shell script in `realm-setup-job.yaml`: chains of `kcadm` calls, each suffixed with `|| true` to fake idempotency. That shape has three problems:

- `|| true` masks real failures. A genuine error looks identical to a benign "already exists," so the Job reports success while leaving the realm half-configured.
- There is no drift detection. The script pushes state forward but never reconciles against what the realm actually contains.
- Extending the configuration means appending more brittle shell, and the original implementation rendered realm content (with `$(env:VAR)` placeholders) into a ConfigMap committed to the GitOps repo. That treats the GitOps repo as a place where realm content, and by extension secrets, can land.

[PR #154](https://github.com/nebari-dev/nebari-infrastructure-core/pull/154) proposed replacing the shell script with a declarative pipeline. The design was accepted; [PR #289](https://github.com/nebari-dev/nebari-infrastructure-core/pull/289) implements Phase 1a (defaults only). This ADR records the architectural decision behind that work.

## Decision Drivers

- Make realm configuration declarative and idempotent, with real failures surfaced instead of swallowed by `|| true`.
- Keep the GitOps repo free of realm content and secrets. The repo is treated as untrusted.
- Use one tool that covers realm settings, clients, groups, scopes, and identity providers, so there is no separate code path for "SSO setup" versus "realm setup."
- Coexist safely with operator-managed OAuth2 clients that NebariApplication CRDs create at runtime. The bootstrap tool must never delete resources it did not create.
- Keep the toolchain pinnable and reproducible: the reconciler and Keycloak versions move together.

## Considered Options

1. **Keep the imperative `kcadm` shell script.**
2. **keycloak-config-cli (kcc) with realm input in a GitOps-repo ConfigMap**, using `$(env:VAR)` placeholders for secret values.
3. **keycloak-config-cli (kcc) with realm input in an in-cluster Secret** created by NIC at bootstrap from embedded defaults.

## Decision Outcome

Chosen option: **Option 3 (kcc with an in-cluster Secret)**.

[keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli) reads a declarative YAML realm definition and reconciles it against Keycloak through the Admin API. NIC renders embedded defaults (`{{ .Domain }}` substituted at deploy time, used in the argocd client redirect URI) into a Secret named `keycloak-config-import` in the `keycloak` namespace, created immediately after the other Keycloak secret creators during foundational bootstrap. The GitOps repo holds only the Job manifest; realm content never leaves the cluster.

The Phase 1a implementation in #289 introduces `pkg/argocd/keycloak_defaults.yaml` (realm input in kcc's native format), `pkg/argocd/keycloak_defaults.go` (an embed plus a render helper), the `createKeycloakImportSecret` step in `pkg/argocd/foundational.go`, and a rewritten `realm-setup-job.yaml` that runs the kcc image and mounts the Secret. The previous gitops `realm-config-cm.yaml` ConfigMap is deleted.

This ADR commits to:

- **kcc as the reconciler** for the bootstrap Keycloak realm, replacing the imperative shell script.
- **An in-cluster Secret, not a GitOps-repo ConfigMap, as the carrier** for realm input. The GitOps repo is untrusted; a "the file only ever contains placeholders, never literals" rule is a footgun, because a single operator mistake leaks a secret into git history permanently. The Secret carrier also accepts a future user-supplied `keycloak.yaml` (Phase 1b) without restructuring.
- **Three-layer protection for operator coexistence**, so operator-managed clients are never touched by kcc:
  1. The kcc input declares only the `argocd` client. Operator-managed clients are not declared.
  2. `IMPORT_REMOTESTATE_ENABLED=true` (kcc default on v6.x): kcc only manages resources it itself created, tracked in a realm attribute.
  3. `IMPORT_MANAGED_CLIENT=no-delete`: belt-and-braces, so even if the input shape changes, kcc still will not delete unlisted clients.
- **Coupled version pinning**: the Keycloak chart image and the kcc image are pinned together (Keycloak `26.5.4` and kcc `6.5.0-26.5.4`), because kcc image tags are tighter than the upstream CI matrix advertises.

This ADR explicitly does NOT commit to:

- The user-supplied `keycloak.yaml` schema (Phase 1b) or the user-secrets pipeline (Phase 1c). Those follow in separate PRs.
- A NIC-side merge engine. kcc owns reconciliation via remote-state; the #154 design rewrite rejected building a separate merge/prefix engine in NIC.
- Backup and restore of realm state.

### Consequences

**Good:**

- Realm configuration is declarative and idempotent, and real failures surface instead of being hidden by `|| true`.
- No realm content or secrets live in the GitOps repo. The repo holds only the Job manifest.
- One tool spans realm settings, clients, groups, scopes, and identity providers. Declarative SSO (Google, GitHub, generic OIDC/SAML) lands as the same shape in Phase 1b/1c, with no separate "SSO setup" path.
- Migration from the shell setup is effectively a no-op for already-correct values: kcc detects existing realm, users, groups, scopes, and clients by natural key and adopts them into remote state. The pre-generated argocd OIDC client secret is byte-identical across the keycloak-namespace Secret, the argocd Secret, and Keycloak's stored value, so OIDC trust survives the cutover.
- A unit test (`keycloak_defaults_test.go`) asserts each load-bearing field renders and that no unresolved Go-template markers leak into the output, catching regressions before they reach a cluster.

**Bad:**

- **kcc applies list fields as full-replace, not merge.** `defaultDefaultClientScopes: [groups]` replaces the entire list, so every built-in default scope must be listed alongside `groups`, both at realm level and on the argocd client. The admin user's `groups` field is full-replace the same way: memberships added manually via the Keycloak UI are reverted on the next `nic deploy`.
- **A `HookSucceeded`-only delete policy traps stuck Jobs.** A kcc Job that fails never succeeds, so its cleanup never fires, and ArgoCD's hook finalizer keeps the immutable Job around indefinitely. `BeforeHookCreation` is added to the policy so each fresh sync first deletes any existing Job.
- **kcc runs string substitution over the whole file before parsing it as YAML,** comments included. A comment that literally contains the `$(env:VAR)` shape trips the substitutor. Comments must describe the syntax without writing the literal pattern.
- kcc is a new foundational tool dependency, and the Keycloak-to-kcc version coupling has to be maintained on upgrades.
- Force-deleting a stuck Job (by stripping its finalizer) can leave the ArgoCD application in a stuck operation state. This only surfaces when iterating on the same cluster, not on normal first deploys; the unstick pattern is `kubectl patch app <name> --type=merge -p '{"operation":null,"status":{"operationState":null}}'` followed by a fresh sync.

## Options Detail

### Option 1: Imperative kcadm shell script (status quo)

The existing `realm-setup-job.yaml` runs `kcadm` commands sequentially, each ending in `|| true` so a re-run does not fail on "already exists."

**Pros:**

- No new tool dependency; it works today.
- Fully visible in the Job manifest.

**Cons:**

- `|| true` masks genuine failures as benign, so a half-configured realm reports success.
- No drift detection or reconciliation; the script only pushes forward.
- Brittle and increasingly hard to extend as realm configuration grows.
- The variant that rendered realm content into a GitOps-repo ConfigMap puts realm content, and the risk of secrets, into the repo.

### Option 2: kcc with a GitOps-repo ConfigMap

kcc reconciles declaratively, but its realm input is a ConfigMap rendered into the GitOps repo with `$(env:VAR)` placeholders for secret values.

**Pros:**

- Declarative and idempotent, same reconciliation benefits as Option 3.
- Realm input is visible in the GitOps repo.

**Cons:**

- Realm content lives in the GitOps repo, which is treated as untrusted.
- Depends on a "placeholders only, never literals" rule that is a footgun: one mistake leaks a secret into git history permanently.
- No clean carrier for a future user-supplied `keycloak.yaml` without repeating the same repo-exposure problem.

### Option 3: kcc with an in-cluster Secret (chosen)

kcc reconciles declaratively, and its realm input is a Secret (`keycloak-config-import`) that NIC creates in-cluster at bootstrap from embedded, domain-substituted defaults. The GitOps repo holds only the Job manifest.

**Pros:**

- Declarative and idempotent, with the GitOps repo kept free of realm content and secrets.
- The Secret carrier accepts a user-supplied `keycloak.yaml` in Phase 1b without restructuring.
- Operator coexistence handled by the three-layer protection above.

**Cons:**

- Realm input is not directly visible in the GitOps repo; it is a cluster Secret rendered by NIC.
- Requires a NIC bootstrap step to render and create the Secret before the Job runs.

## Links

- [PR #154](https://github.com/nebari-dev/nebari-infrastructure-core/pull/154) - accepted design: declarative Keycloak configuration.
- [PR #289](https://github.com/nebari-dev/nebari-infrastructure-core/pull/289) - Phase 1a implementation (shell-to-kcc swap, defaults only).
- [PR #234](https://github.com/nebari-dev/nebari-infrastructure-core/pull/234) - ArgoCD OIDC SSO, whose realm-setup state is preserved through the migration.
- [Issue #288](https://github.com/nebari-dev/nebari-infrastructure-core/issues/288) - user-facing docs follow-up (full-replace semantics for user groups and identity providers).
- [ADR-0007](0007-cloudnativepg-managed-databases.md) - CloudNativePG, which references the Keycloak-to-kcc version-pinning pattern established here.
- [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli) - the declarative reconciler.
