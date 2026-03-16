# Declarative Keycloak Configuration

## Problem

Keycloak configuration is currently handled by a shell script (`realm-setup-job.yaml`) that runs `kcadm.sh` commands as a PostSync hook. This approach is:

- Hard to extend - every new resource means more imperative shell commands
- Not exportable - no way to capture current state for backup/restore
- Not overridable - users can't customize realm settings, identity providers, or auth flows without forking the script

## Proposal

Replace the imperative shell script with a declarative configuration system using [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli), an actively maintained open-source tool that applies JSON/YAML configuration to Keycloak via its Admin API. It's idempotent, supports Keycloak's own realm export format, and has "managed resource" tracking so it only touches resources it created.

## Goals

- Declaratively configure all Keycloak resources: realms, users, groups, roles, identity providers, authentication flows, client scopes
- Provide sensible defaults (nebari realm, standard roles, admin user) that work out of the box
- Allow users to override or extend defaults via `config.yaml`
- Enable full backup/restore round-trip: export current Keycloak state, restore it to a fresh instance
- Coexist safely with the nebari-operator, which dynamically creates OAuth2 clients

## Non-Goals

- Continuous reconciliation - this is initial bootstrap configuration, not a controller
- Managing operator-created clients - the operator owns those
- Replacing Keycloak's own admin UI for day-to-day changes

## Architecture

### How it Works

```
config.yaml (user overrides)
        |
        v
NIC merges with defaults --> ConfigMap YAML --> Git repo
        |
        v
ArgoCD syncs --> keycloak-config-cli Job (PostSync) --> Keycloak Admin API
```

1. NIC has baked-in defaults for the nebari realm (same config the current shell script creates)
2. Users override or extend those defaults in a new `keycloak` section of `config.yaml`
3. NIC deep-merges defaults with overrides and writes the result as a ConfigMap to the gitops repo
4. ArgoCD syncs the ConfigMap and a PostSync Job runs [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli) against it
5. keycloak-config-cli applies the config to Keycloak via Admin API - idempotent and safe to re-run

### Operator Coexistence

keycloak-config-cli supports "managed resources" with a configurable prefix. Resources created by this system are tagged with a `nebari:` prefix. The operator's dynamically created OAuth2 clients are untagged and left untouched. This is the boundary:

- **This system owns**: realms, base users/groups, roles, identity providers, auth flows, client scopes
- **Operator owns**: application-specific OAuth2 clients created via NebariApplication CRDs

### Secrets Handling

Secrets never go in `config.yaml`. The config references environment variable names using keycloak-config-cli's `${env:VAR}` substitution syntax. These references are written literally into the ConfigMap - no substitution happens in NIC.

At deploy time, NIC scans the merged config for `${env:*}` references, reads the actual values from the local environment (`.env` file or CI), and creates a Kubernetes Secret. The Job mounts that Secret, and keycloak-config-cli resolves the references at runtime.

Missing env vars at deploy time are a hard failure - the deploy stops with an error listing what's missing.

## config.yaml Schema

New top-level `keycloak` section. The format mirrors keycloak-config-cli's input (which mirrors Keycloak's own export format), so there's no translation layer.

```yaml
keycloak:
  realms:
    - realm: nebari
      displayName: "Nebari Platform"
      sslRequired: external
      registrationAllowed: false
      loginWithEmailAllowed: true
      resetPasswordAllowed: true
      bruteForceProtected: true

      # Token/session policies
      accessTokenLifespan: 300
      ssoSessionIdleTimeout: 1800

      # Roles
      roles:
        realm:
          - name: admin
          - name: user

      # Identity providers
      identityProviders:
        - alias: github
          providerId: github
          enabled: true
          config:
            clientId: "${env:GITHUB_CLIENT_ID}"
            clientSecret: "${env:GITHUB_CLIENT_SECRET}"

      # Default users (bootstrap only)
      users:
        - username: admin
          email: admin@nebari.local
          enabled: true
          emailVerified: true
          firstName: Admin
          lastName: User
          realmRoles:
            - admin
            - user
          credentials:
            - type: password
              value: "${env:REALM_ADMIN_PASSWORD}"

      # Authentication flows, client scopes, etc.
      authenticationFlows: []
      clientScopes: []

  # Automated backup (optional)
  backup:
    enabled: false
    schedule: "0 2 * * *"
    destination:
      type: s3
      bucket: my-keycloak-backups
      prefix: nebari/
      region: us-west-2
```

Users can add additional realms to the list. Defaults only apply to the `nebari` realm - additional realms must be fully specified.

## Default Configuration & Merge Strategy

Baked-in defaults represent what the current realm-setup-job creates:

- nebari realm with standard security settings
- admin and user realm roles
- admin user with admin + user roles
- brute force protection, email login, password reset enabled

Merge strategy - deep merge with config.yaml winning:

- **Scalars**: config.yaml value replaces default
- **Lists** (users, roles, identityProviders): merged by key field (`username`, `name`, `alias`). Matching key overrides that entry, new keys are appended
- **Removal**: set `enabled: false` on a default entry

Example - user only wants to add GitHub IdP and change session timeout. Everything else comes from defaults:

```yaml
keycloak:
  realms:
    - realm: nebari
      ssoSessionIdleTimeout: 3600
      identityProviders:
        - alias: github
          providerId: github
          enabled: true
          config:
            clientId: "${env:GITHUB_CLIENT_ID}"
            clientSecret: "${env:GITHUB_CLIENT_SECRET}"
```

## Backup & Restore

### On-demand: `nic backup keycloak`

```bash
# Writes timestamped file to current directory
nic backup keycloak -f config.yaml

# Custom output path
nic backup keycloak -f config.yaml -o /path/to/backup.yaml

# Stdout for piping
nic backup keycloak -f config.yaml -o -
```

Connects to the cluster, calls Keycloak's Admin REST API to export realm state (users, credentials, clients), and writes keycloak-config-cli-compatible YAML locally. Passwords are exported as salted hashes (PBKDF2), never plaintext. The backup file contains sensitive data (hashed credentials, client secrets) - users are responsible for storing it securely.

### Automated: CronJob

When `keycloak.backup.enabled: true`, NIC writes a CronJob manifest to the gitops repo that exports on schedule to S3/GCS. Cloud storage auth uses the pod's service account / IRSA / workload identity.

### Restore: `nic restore keycloak`

```bash
nic restore keycloak -f config.yaml --from keycloak-backup-2026-03-12T14-30-00.yaml
```

Reads the backup, writes it as the ConfigMap in the gitops repo, commits and pushes. ArgoCD syncs and keycloak-config-cli reconciles Keycloak to that state. This is a full restore - it replaces the entire config, not a selective merge.

## Migration from Current System

Existing deployments that already ran the old realm-setup-job will have a nebari realm with roles and an admin user. keycloak-config-cli handles this gracefully:

- Detects existing realms by name and updates rather than failing on create
- Matches existing users by username, roles by name
- Operator-created clients are protected by the managed resource prefix

No special migration steps are needed. The first run against an existing Keycloak is safe and idempotent.

## Phasing

**Phase 1**: Config merge + Job replacement - replace the shell script with keycloak-config-cli, add `keycloak` section to config.yaml, implement merge logic

**Phase 2**: Backup & restore - `nic backup keycloak`, `nic restore keycloak`, optional CronJob

Phase 2 depends on Phase 1 being deployed and validated.

## Open Questions

1. **keycloak-config-cli version pinning strategy** - keycloak-config-cli has strict version coupling with Keycloak (6.x targets Keycloak 25.x/26.x). How do we want to manage this? Pin in the Job manifest and document the compatibility mapping?

2. **Config validation** - Should `nic validate` check the merged Keycloak config against a schema before deploy, or is runtime failure from the Job acceptable initially?

3. **CronJob backup image** - Build a custom lightweight image, or use an existing tool for the export-to-cloud-storage flow?

## Dependencies

- [keycloak-config-cli](https://github.com/adorsys/keycloak-config-cli) - Docker image, actively maintained, idempotent, supports Keycloak's realm export format
- Keycloak Admin REST API for backup/export
