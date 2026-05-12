# Declarative Keycloak Configuration

## Problem

Keycloak configuration is currently handled by a shell script (`realm-setup-job.yaml`) that runs `kcadm.sh` commands as a PostSync hook. This approach is:

- Hard to extend. Every new resource means more imperative shell commands.
- Not declarative. There is no single source of truth for what the realm should look like.
- Not reproducible. Re-running against a fresh cluster requires running through the same imperative steps.
- Not overridable. Users cannot customize realm settings, identity providers, or auth flows without forking the script.
- Blocks blue/green Keycloak upgrades. There is no "apply this exact realm definition to a fresh instance" workflow.

## Proposal

Replace the imperative shell script with a declarative pipeline using [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli) (kcc), an actively maintained open-source tool that applies JSON/YAML configuration to Keycloak via its Admin API. kcc is idempotent, accepts Keycloak's own realm export format, and has a `remote-state` mechanism that tracks resources it itself created so it can coexist with resources created out-of-band (e.g., by the nebari-operator).

The user's realm definition lives in a separate `keycloak.yaml` file alongside `config.yaml`. The file is in kcc's native input format, with no NIC-side translation. NIC reads it, validates it, writes it to an in-cluster Secret, and emits a Job manifest into the gitops repo. The Job runs kcc against the Secret.

## Goals

- Declaratively define all Keycloak realm content: realms, users, groups, roles, identity providers, authentication flows, client scopes, mappers.
- Make the realm definition reproducible: same input file plus same secret env vars produces the same realm state.
- Enable blue/green Keycloak upgrades: apply the same definition to a fresh instance, validate, swap traffic.
- Provide sensible baked-in defaults that reproduce the current realm-setup-job's output, including the `groups` client scope and group-membership mapper.
- Allow users to override or extend defaults by providing their own `keycloak.yaml`.
- Coexist safely with the nebari-operator, which dynamically creates OAuth2 clients at runtime.
- Keep secrets out of the gitops repo entirely.

## Non-Goals

- Continuous reconciliation in the controller sense. The kcc Job runs on ArgoCD sync; it does not watch Keycloak for drift.
- Managing operator-created clients. The operator owns those at runtime.
- Replacing Keycloak's own admin UI for day-to-day changes to resources that are not declared in `keycloak.yaml`.
- Bulk end-user provisioning. End users are expected to arrive via IdP login or self-registration. `keycloak.yaml` is for bootstrap accounts and service accounts.

## Architecture

### Pipeline

```
keycloak.yaml (operator's machine or infra repo)
        |
        v
nic deploy:
  1. read file
  2. validate schema, block literal secrets in sensitive fields, block unset env refs
  3. write file content to in-cluster Secret: keycloak-config-import
  4. scan for $(env:VAR) references, populate Secret: keycloak-config-user-secrets from .env
  5. emit kcc Job manifest to gitops repo with a deploy-hash annotation
        |
        v
gitops repo: Job manifest only. No realm content. No secrets.
        |
        v
ArgoCD syncs Job --> mounts both Secrets --> kcc applies to Keycloak
```

### Where things live

| Location | Contents | Trust level |
|---|---|---|
| `keycloak.yaml` (infra repo or operator's machine) | Full realm config with `$(env:VAR)` placeholders for any secret-bearing field | Same as `config.yaml`: private, unencrypted by default |
| `.env` (local) or GHA secrets (CI) | Plaintext values for the env references | Sensitive. Operator-managed. |
| In-cluster Secret `keycloak-config-import` | Byte-for-byte content of `keycloak.yaml`, placeholders preserved | Cluster Secret (etcd-encrypted, RBAC-controlled) |
| In-cluster Secret `keycloak-config-user-secrets` | Env values for the references in the file | Cluster Secret |
| In-cluster Secrets created today by NIC (`keycloak-admin-credentials`, `nebari-realm-admin-credentials`, etc.) | NIC-generated bootstrap passwords | Unchanged from current behavior |
| Gitops repo | Job manifest with kcc runtime configuration (substitution flags as env vars on the container) | No realm content of any kind |

The gitops repo never contains realm structure, IdP names, redirect URIs, credential hashes, or any secret values. The only Keycloak-related artifact there is a Job that mounts named Secrets.

### Why no ConfigMap in the gitops repo

We considered writing the kcc input as a ConfigMap to the gitops repo with `$(env:...)` placeholders, letting kcc substitute at runtime from a mounted Secret. We rejected that approach because:

1. The gitops repo is treated as untrusted. Any pattern that requires "the file in the repo only contains placeholders, never literals" creates a footgun: one operator mistake leaks a secret into git history forever.
2. The kcc input file can encode information beyond raw secrets that is still sensitive: full IdP configurations, redirect URI patterns, realm structure, credential hashes from `kc.sh export`.
3. Putting realm content in the gitops repo at all means the gitops repo's trust boundary has to be tightened to cluster-Secret level, which is a heavier ask than necessary.

The in-cluster Secret model removes the leak vector entirely.

### Operator coexistence with the nebari-operator

The nebari-operator dynamically creates OAuth2 clients at runtime in response to `NebariApplication` CRDs. We need kcc not to delete those clients on subsequent runs.

The doc previously proposed a "managed resource prefix" mechanism. After checking kcc's docs and source, that mechanism does not exist. kcc has a cleaner three-layer model that we lean on instead:

1. **Omit `clients:` from `keycloak.yaml` entirely.** Per kcc's resource management semantics (see [kcc MANAGED.md](https://github.com/adorsys/keycloak-config-cli/blob/v6.5.0/docs/MANAGED.md) and [issue #1306](https://github.com/adorsys/keycloak-config-cli/issues/1306)), omitting a top-level resource type means "leave this type alone." This is different from `clients: []`, which means "delete all clients."
2. **`import.remote-state.enabled=true`** (the v6.x default). kcc tracks resources it itself created in a Keycloak realm attribute (`de.adorsys.keycloak.config.state-*`). On re-apply, kcc only considers resources tracked there for deletion. Operator-created clients are invisible to kcc's delete logic even if `clients:` were declared.
3. **`import.managed.client=no-delete`** (override the v6.x default of `full`). Belt-and-braces: even if a future change accidentally listed `clients:` in the input, kcc still would not delete unlisted ones.

Three independent layers. No need to retrofit a prefix onto existing operator-created clients, which would itself have been a migration concern.

The boundary:

- **kcc owns**: realms, base users, groups, roles, identity providers, authentication flows, client scopes, mappers.
- **Operator owns**: application-specific OAuth2 clients created via `NebariApplication` CRDs.

### No NIC-side merging

`keycloak.yaml` is passed through to kcc verbatim. NIC does not parse the realm schema, does not merge it with anything, does not own deep-merge logic. The file IS the kcc input.

If `keycloak.yaml` is absent, NIC ships a baked-in default kcc input (embedded via `//go:embed`) that reproduces what the current realm-setup-job creates, including:

- nebari realm with security settings (sslRequired=external, brute force protection, email login, password reset)
- admin and user realm roles
- admin user with admin + user roles
- **groups client scope** with `oidc-group-membership-mapper` registered as a realm default-default scope (this is what makes `groups` claims appear in tokens, which is what makes `NebariApplication.allowedGroups` work in the operator)

If `keycloak.yaml` is present, the user's file replaces the default entirely. There is no merge. If the user wants the default groups scope, they include it in their file. `nic init keycloak` (a future helper) can dump the defaults to a starting `keycloak.yaml`.

Rationale: kcc's `remote-state` model is the reconciliation engine. NIC doesn't need its own. Edits made to NIC-managed resources via Keycloak's admin UI will revert on next sync (that is the declarative contract). Edits to admin-created resources outside the input file are preserved (because kcc never tracked them).

## Secrets Handling

The gitops repo never holds secrets. Secrets land in cluster Secrets only, via three populator paths.

### Path 1: NIC-generated bootstrap (unchanged from today)

NIC continues to generate the admin password, DB passwords, Postgres passwords, and realm-admin password on first deploy via `generateSecurePassword(rand.Reader)` (see `cmd/nic/deploy.go:175-186`). These are written to existing Secrets (`keycloak-admin-credentials`, `keycloak-postgresql-credentials`, `postgresql-credentials`, `nebari-realm-admin-credentials`) by `pkg/argocd/foundational.go`. `createSecret` has no-op-if-exists semantics, so on subsequent deploys the original generated passwords are preserved. Operators can rotate via `kubectl edit secret`.

This pipeline is untouched. The kcc Job mounts `nebari-realm-admin-credentials` to set the realm admin password declaratively in the input (or we keep using a small post-step; see open verification item below).

### Path 2: User-supplied via env references

For secrets the operator owns and supplies (IdP client secrets, custom credentials), `keycloak.yaml` uses kcc's variable substitution syntax. kcc 6.x uses `$(env:VAR)` with parentheses, not `${env:VAR}` with braces. Default-value form is `$(env:VAR:-fallback)`.

```yaml
identityProviders:
  - alias: github
    providerId: github
    enabled: true
    config:
      clientId: "$(env:GITHUB_CLIENT_ID)"
      clientSecret: "$(env:GITHUB_CLIENT_SECRET)"
```

At deploy time, NIC scans the file for `$(env:*)` references and reads each from the local environment (loaded via godotenv from `.env`, or from CI env). Values land in `keycloak-config-user-secrets` (overwrite-on-deploy, because the env is the source of truth for these). The kcc Job mounts that Secret as env vars and runs with `import.var-substitution.enabled=true` (off by default in kcc, must be enabled explicitly). `import.var-substitution.undefined-is-error=true` is kcc's default and we rely on it: unset references at apply time fail the Job loudly.

### IdP and confidential client secrets: preserved by construction

Preservation across re-applies is automatic under this model:

1. Operator stores the GitHub OAuth app secret (or LDAP bind password, or any IdP credential) in their password manager.
2. The value lives in `.env` locally or GHA secrets in CI.
3. `nic deploy` writes it to `keycloak-config-user-secrets`. kcc resolves the placeholder.
4. Re-applying with the same env value produces no change in Keycloak. kcc is idempotent on unchanged input.
5. Rotation: new secret on the IdP side, update `.env`/GHA secret, `nic deploy`. kcc pushes the new value.

NIC never generates or rotates IdP secrets. The operator owns them; NIC just plumbs values through.

### Bootstrap user credentials

Users declared in `keycloak.yaml` (the bootstrap admin, service accounts) get credentials applied by kcc. The recommended pattern:

```yaml
users:
  - username: alice
    email: alice@example.com
    enabled: true
    emailVerified: true
    credentials:
      - type: password
        value: "$(env:ALICE_INITIAL_PASSWORD)"
        temporary: true
    requiredActions: ["UPDATE_PASSWORD"]
```

Operator picks the temp password, sets the env var, communicates it out-of-band. User logs in and is forced to reset. The password they set after reset lives only in Keycloak; it is not in `keycloak.yaml`.

When SMTP is configured, an alternative pattern avoids the temp-password communication step entirely: omit `credentials[]`, set `requiredActions: ["UPDATE_PASSWORD"]` plus `emailVerified: false`, and let Keycloak send a reset email on first interaction.

**Open verification item for Phase 1a**: confirm whether kcc re-applies `credentials[]` on every run (likely yes, because Keycloak does not expose existing credential plaintexts for diffing), and whether re-application re-arms `UPDATE_PASSWORD` such that the user has to reset on every deploy. If yes, omitting `credentials[]` and relying on SMTP-based password setup is the only stable pattern. The doc will be updated based on the finding.

### What NIC validates and blocks

Before writing to cluster Secrets, NIC runs these checks against `keycloak.yaml`:

1. **Reject literal values in known-sensitive fields.** Walk a maintained list: `clients[].secret`, `identityProviders[].config.clientSecret`, `users[].credentials[].value`, `users[].credentials[].secretData`, `users[].credentials[].credentialData`, `components[].config.bindCredential`, `smtpServer.password`. Any value that is not a `$(env:...)` reference (or empty/unset) is a deploy-blocking error. Escape hatch: `--allow-literal-secrets` flag for power users who accept the risk on their local disk.
2. **Reject unresolved env references in sensitive fields.** If `clientSecret: "$(env:GITHUB_CLIENT_SECRET)"` is declared but `GITHUB_CLIENT_SECRET` is unset, fail the deploy. Catches "operator forgot to set the env var" before kcc applies an empty secret and silently breaks GitHub login.
3. **Reject `secretData` / `credentialData` (credential hashes).** By default, declared users may not include credential hashes. Hashes are sensitive (offline-attackable for weak passwords) and should not sit in plaintext in the infra repo. Escape hatch: `--allow-credential-hashes` flag for the migration scenario where users cannot be forced to reset.
4. **Warn on declared users without `temporary: true` + `requiredActions`.** Not blocking, but the warning tells the operator their declared user's password will be re-applied on every deploy unless they opt into the recommended pattern.

This scanner is best-effort. The list of sensitive fields will lag Keycloak version additions. Documented as a known limit.

### `kc.sh export` is not directly drop-in safe

A raw `kc.sh export` output contains plaintext IdP `clientSecret` values, plaintext confidential client secrets, LDAP bind passwords, SMTP passwords, and credential hashes. The literal-secret scanner above will refuse to deploy such a file, which is the correct default.

A Phase 2 helper `nic keycloak sanitize <export.yaml>` will rewrite literal secrets to `$(env:...)` references (printing the env var names that need to be set) and strip credential hashes. Until that exists, the doc will warn loudly that export output must be hand-sanitized before being used as `keycloak.yaml`.

## CI Integration

`keycloak.yaml` is treated identically to `config.yaml` for CI purposes: a plaintext file in the infra repo, with secret env values supplied separately.

```yaml
# .github/workflows/deploy.yml
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.DEPLOY_ROLE_ARN }}
      - name: Deploy
        env:
          GITHUB_CLIENT_SECRET: ${{ secrets.GITHUB_CLIENT_SECRET }}
          ALICE_INITIAL_PASSWORD: ${{ secrets.ALICE_INITIAL_PASSWORD }}
          CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}
        run: nic deploy -f config.yaml
```

And on PRs:

```yaml
# .github/workflows/validate.yml
on: [pull_request]
jobs:
  validate:
    steps:
      - uses: actions/checkout@v4
      - run: nic validate -f config.yaml
```

`nic validate` runs the scanner above plus a check that every `$(env:VAR)` reference has a value available. This catches config errors before they hit the cluster as cryptic PostSync Job failures.

We do not recommend storing `keycloak.yaml` itself as a GitHub Actions secret:

- GHA secrets cap at 48KB each; real realm configs can exceed this.
- The file as a secret blob loses diff-ability in PR review.
- The file is no more sensitive than `config.yaml` once the literal-secret scanner is enforced.

For the edge case of migrating an existing realm with credential hashes that cannot be reset, Phase 2 will support SOPS-encrypted `keycloak.yaml` with the decryption key as a GHA secret.

## Blue/Green Keycloak Upgrades

A direct benefit of the file-based declarative model:

1. Deploy Keycloak-v2 in a parallel namespace (manual step or future `nic deploy --target=v2`).
2. Run `nic deploy` pointing at the v2 namespace. NIC writes `keycloak-config-import` and `keycloak-config-user-secrets` there. kcc Job applies the same `keycloak.yaml`.
3. Validate v2: check the rendered realm via Admin API, run smoke tests against the new instance.
4. Swap traffic via the Gateway. Decommission v1.

The source of truth for both v1 and v2 is the same `keycloak.yaml` plus the same env values. Reproducibility is the whole point of the declarative model.

## Job Re-Roll Mechanism

The kcc Job is a one-shot. To make Argo re-roll it when content changes, NIC sets a `nic.nebari-dev.io/deploy-timestamp` annotation on the Job manifest to the current deploy time. Every `nic deploy` creates a fresh Job spec, Argo rolls it out, kcc is idempotent so re-running is safe and cheap.

A content-hash annotation (hash of `keycloak.yaml` plus hashed env var names) would skip re-rolls when nothing changed. We're deferring that optimization to a follow-up; the timestamp approach is simpler and the cost of an extra kcc run on every deploy is negligible.

## Migration from Current System

Existing deployments that already ran the old realm-setup-job will have a nebari realm with roles, an admin user, and the groups client scope. kcc handles this gracefully:

- Detects existing realms by name and updates rather than failing on create.
- Matches existing users by username, roles by name, groups by name, scopes by name.
- Operator-created clients are protected by `clients:` being omitted from `keycloak.yaml` (and by `remote-state` tracking, and by `managed.client=no-delete`).

The first kcc run against an existing Keycloak should be safe and idempotent. Phase 1a will include an integration test that:

1. Starts a Keycloak with the current realm-setup-job applied.
2. Runs kcc against the baked-in default `keycloak.yaml`.
3. Asserts a token issued to a user in a group contains the `groups` claim (the contract that `NebariApplication.allowedGroups` depends on).
4. Asserts an operator-created client is still present and untouched.

## Phasing

We're splitting the original "Phase 1" into three sub-phases. Each is independently testable, and 1a alone is forward-compatible with both this design and the gitops-overlay alternative listed below.

### Phase 1a: kcc replaces kcadm, defaults only

- Replace `realm-setup-job.yaml` (the kcadm shell script) with a kcc Job.
- kcc reads from `keycloak-config-import`, populated by NIC from baked-in defaults via `//go:embed`.
- No `keycloak.yaml` parsing. No user-facing schema. No env scanning.
- Validates the kcc-replaces-kcadm migration in isolation: groups scope present, operator coexistence works, kcc's `remote-state` model behaves as expected.
- Confirms or refutes the kcc-credential-re-application open question above.

### Phase 1b: user `keycloak.yaml` file

- NIC reads `keycloak.yaml` if referenced from `config.yaml` via `keycloak.config_file: ./keycloak.yaml`.
- Pass-through to `keycloak-config-import` Secret. No merging.
- Literal-secret scanner, credential-hash blocker, env-ref validator.
- `nic validate` checks the scanner before any deploy.
- `--allow-literal-secrets` and `--allow-credential-hashes` escape hatches.

### Phase 1c: env substitution and user-secrets Secret

- NIC scans `keycloak.yaml` for `$(env:*)` references.
- Populates `keycloak-config-user-secrets` from local env / `.env`.
- Job mounts both Secrets, kcc runs with `var-substitution.enabled=true`.
- Unset env refs in sensitive fields fail the deploy.

Each phase is a separate commit (or PR) with clear scope boundaries.

## Phase 2 (Follow-up)

Out of scope for this design doc, listed here for context on what comes next:

- `nic keycloak sanitize <export.yaml>` helper that converts `kc.sh export` output into a safe `keycloak.yaml` (rewrites literals to env refs, strips credential hashes).
- SOPS-encrypted `keycloak.yaml` support for migration cases requiring credential hashes.
- ExternalSecrets Operator interop: NIC detects when `keycloak-config-user-secrets` is managed by ESO and skips writing it.
- Content-hash Job annotation instead of timestamp (skip re-roll when nothing changed).
- Backup/restore (separate design doc; see below).

## Backup & Restore (Separate Follow-up)

The original draft of this doc included `nic backup keycloak` and `nic restore keycloak` CLI commands plus a CronJob-based automated backup. We're moving that to a separate follow-up design doc because:

- It's a different code path (CLI commands and cluster access, not the render pipeline).
- It introduces an operational pattern NIC does not have today: CLI commands that need cluster access plus port-forward or exec to reach Keycloak.
- The original "restore is full replace, not selective merge" semantic conflicts with this design's "kcc remote-state handles reconciliation" model.
- Velero-PVC-snapshot vs Keycloak-realm-export are two reasonable approaches that deserve their own trade-off discussion.

In the meantime, with the file-based model, a manual backup is just `kc.sh export > realm-snapshot.yaml`, and a manual restore is hand-sanitizing the export and using it as the next `keycloak.yaml`.

## Alternatives Considered

### Alternative A: NIC owns deep-merge over an embedded `keycloak:` section in `config.yaml`

The original draft of this doc put the realm config under a `keycloak:` section in `config.yaml` and had NIC deep-merge user overrides with baked-in defaults. We rejected this because:

- NIC would own a deep-merge engine over a deeply nested upstream schema (Keycloak realm export).
- Real realm configs are large (1000-5000 line JSON files are normal in production). Embedding them in `config.yaml` would dominate the file and make it hard to read or diff.
- A separate `keycloak.yaml` plays nicely with `kc.sh export` output and with encryption-at-rest tools (SOPS) without dragging `config.yaml` along for the ride.
- The "removal via `enabled: false`" semantic the original draft proposed was wrong: in Keycloak, `enabled: false` disables a resource rather than deleting it. kcc's `remote-state` model handles deletions correctly without NIC needing semantics for it.

### Alternative B: NIC ships a default ConfigMap, users customize via gitops overlays

NIC writes a default kcc input ConfigMap into the gitops repo, and users customize via Kustomize overlays or direct ConfigMap edits in the gitops repo. NIC stays scoped to infrastructure.

This was Vini's suggestion (PR #154, comment dated 2026-04-10). We rejected it because:

- It moves the source of truth for Keycloak realm state into the gitops repo, which we've decided is untrusted for any content beyond Job manifests and references.
- Users who customize Keycloak via the gitops repo have to learn Kustomize patterns rather than editing one file via `nic deploy`.
- Loses reproducibility from `nic deploy` alone: the realm state after a deploy depends on what's currently committed in the gitops repo, which may have been edited by hand.

### Alternative C: Sealed Secrets or SOPS-encrypted Secret manifests in the gitops repo

Write the kcc input as a Sealed Secret or SOPS-encrypted Secret manifest to the gitops repo. Encryption at rest by construction, decryption in-cluster by a controller.

Possible future direction. Rejected for Phase 1 because:

- Adds a hard dependency on a controller (sealed-secrets, SOPS operator) that we'd either ship or require users to install.
- The encrypted blob is not diff-able in PR review.
- The in-cluster Secret model achieves the same security property without requiring new controllers.

## Keycloak / kcc Version Coupling

kcc has strict coupling with Keycloak versions. v6.x targets Keycloak 24.x/25.x/26.x (the verified-compatible image for Keycloak 24.0 is `quay.io/adorsys/keycloak-config-cli:6.5.0-24.0.5`).

There's a deeper coupling question underneath: `keycloak.yaml` mirrors kcc's input, which mirrors Keycloak's export schema. A Keycloak major-version upgrade can rename, add, or remove fields, and existing user `keycloak.yaml` files may break.

We're taking stance **C: accept upstream as-is**. The doc will explicitly state: user `keycloak.yaml` files may require updates across Keycloak major-version upgrades. We'll document the upgrade path in the release notes for each Keycloak version bump.

Stances considered:

- **A. Pin Keycloak aggressively.** Treat upgrades as opt-in events with migration tooling. Reversible.
- **B. NIC-owned subset schema.** Translate to upstream at render time. Locks NIC into maintaining a translator forever.
- **C. Accept upstream as-is.** Document upgrade-time breakage. Reversible. Lowest NIC code surface.

C is consistent with the rest of this design's "pass-through, no NIC-side schema" philosophy.

## Dependencies

- [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli), specifically the verified-compatible image for our Keycloak version (currently `quay.io/adorsys/keycloak-config-cli:6.5.0-24.0.5` for Keycloak 24.0).
- Keycloak Admin REST API (already a dependency for the current shell script).

## Open Questions

1. **kcc credential re-application behavior.** Does kcc re-apply `credentials[]` on every run, and does that re-arm `UPDATE_PASSWORD` such that the user has to reset on every deploy? Verified empirically in Phase 1a. The bootstrap-user pattern in this doc may need to be revised based on the finding.

2. **Realm-admin password source.** The kcc input could set the realm admin user's password via `credentials[].value: "$(env:REALM_ADMIN_PASSWORD)"` mounted from `nebari-realm-admin-credentials`, or we could keep the current pattern where a small post-step uses `kcadm.sh set-password`. The latter avoids putting the realm admin password into the kcc input file path at all. To be decided during Phase 1a implementation.

3. **`keycloak.config_file` as path or directory.** kcc supports `import.files.locations` as a glob, so a directory of files (e.g., `users.yaml`, `groups.yaml`, `idps.yaml`) works as well as a single file. Default to accepting either, or pick one for Phase 1b?
