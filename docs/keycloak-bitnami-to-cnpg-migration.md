# Migrating Keycloak's Database from Bitnami PostgreSQL to CloudNativePG

Clusters bootstrapped before the CNPG cutover run Keycloak against the Bitnami
`postgresql` chart (ArgoCD app `postgresql`, StatefulSet `postgresql` in the
`keycloak` namespace). New bootstraps use a CloudNativePG `Cluster` named
`keycloak-db` instead. This runbook moves an existing cluster's Keycloak data
onto CNPG. Fresh installs do not need it.

**Read this before touching anything:** on a pre-CNPG cluster, running
`nic deploy --regen-apps` outside this procedure repoints Keycloak at a fresh,
empty CNPG database. Keycloak comes back up with only the bare realm shell;
your users, clients, groups, and sessions are stranded in the old Bitnami
volume until you restore them. Regen is step 4 of this runbook, after the
backup exists.

## How it works

A logical dump (`pg_dump`) is taken from the Bitnami database, NIC regenerates
the GitOps manifests (which installs the CNPG operator if absent, creates the
`keycloak-db` Cluster, and repoints Keycloak at it), and the dump is restored
into CNPG over the empty schema Keycloak bootstraps. The Bitnami app is then
retired. Rollback is possible at any point before decommissioning because the
Bitnami database is never modified.

Plan a maintenance window. Any change made in Keycloak between the dump
(step 3) and the restore (step 5) is lost, and Keycloak briefly serves an
empty realm during the rollover.

## Prerequisites

- A `nic` build containing the CNPG cutover, and your usual deploy env vars.
- `kubectl` access: `./nic kubeconfig -f <config>.yaml -o /tmp/kubeconfig && export KUBECONFIG=/tmp/kubeconfig`
- Push access to the cluster's GitOps repository (for rollback and decommission).

## 1. Check version compatibility

A logical dump restores into the same or a newer PostgreSQL major version,
never an older one.

```bash
kubectl exec postgresql-0 -n keycloak -- postgres --version
```

NIC's pinned Bitnami chart (18.2.0) ships PostgreSQL 18.1, and the CNPG
operator installed by NIC (1.30.0) defaults to PostgreSQL 18.4, so this check
passes for standard NIC deployments. If your source somehow reports a newer
major version than 18, stop and file an issue before proceeding: the
`keycloak-db` manifest is NIC-generated and does not currently expose an
image override.

## 2. Start the maintenance window

Announce the window; logins and admin changes made from here until step 6 are
lost.

## 3. Dump the Bitnami database

```bash
PGPASS=$(kubectl get secret postgresql-credentials -n keycloak -o jsonpath='{.data.postgres-password}' | base64 -d)

kubectl exec -n keycloak postgresql-0 -- env PGPASSWORD="$PGPASS" \
  pg_dump -U postgres -d keycloak > keycloak-backup.sql

# Sanity: the dump has content, and record the user count to verify against later
grep -c 'CREATE TABLE' keycloak-backup.sql   # EXPECT: ~100 tables
kubectl exec -n keycloak postgresql-0 -- env PGPASSWORD="$PGPASS" \
  psql -U postgres -d keycloak -tc 'select count(*) from user_entity;'
```

Keep `keycloak-backup.sql` somewhere safe until the migration is verified.

## 4. Deploy with regenerated manifests

```bash
./nic deploy -f <config>.yaml --regen-apps
```

This commits and pushes to the GitOps repo: `apps/cloudnative-pg.yaml` (the
operator, if this cluster predates it), `manifests/keycloak/keycloak-db-cluster.yaml`,
and the repointed `apps/keycloak.yaml`. The committed `apps/postgresql.yaml`
is left untouched, so the Bitnami database keeps running.

Wait for the rollover:

```bash
kubectl wait --for=jsonpath='{.status.health.status}'=Healthy application/cloudnative-pg -n argocd --timeout=300s
kubectl wait --for=condition=Ready cluster/keycloak-db -n keycloak --timeout=600s
kubectl rollout status statefulset/keycloak-keycloakx -n keycloak --timeout=600s
```

At this point Keycloak is running against an empty CNPG database (the
realm-setup job has recreated the bare realm). That state is expected and is
about to be overwritten.

## 5. Restore the dump into CNPG

CNPG pods allow local superuser `psql` via peer auth, so no password is
needed. `DROP ... WITH (FORCE)` disconnects Keycloak's open connections;
dropping the database discards the schema Keycloak just bootstrapped so the
restore lands on a clean slate.

```bash
kubectl exec -n keycloak keycloak-db-1 -- psql -U postgres -c 'DROP DATABASE keycloak WITH (FORCE);'
kubectl exec -n keycloak keycloak-db-1 -- psql -U postgres -c 'CREATE DATABASE keycloak OWNER keycloak;'
kubectl exec -i -n keycloak keycloak-db-1 -- psql -U postgres -d keycloak -v ON_ERROR_STOP=1 < keycloak-backup.sql
```

Restart Keycloak so it reconnects and drops any cached state from the empty
interim database:

```bash
kubectl rollout restart statefulset/keycloak-keycloakx -n keycloak
kubectl rollout status statefulset/keycloak-keycloakx -n keycloak --timeout=600s
```

## 6. Verify

```bash
# User count matches the value recorded in step 3
kubectl exec -n keycloak keycloak-db-1 -- psql -U postgres -d keycloak -tc 'select count(*) from user_entity;'
```

Then log into Keycloak with a pre-migration (non-bootstrap) user and confirm
realms, clients, and groups are present. The maintenance window can end here.

If verification fails, roll back: `git revert` the regen commit in the GitOps
repo and push. ArgoCD repoints Keycloak at the untouched Bitnami database.
Investigate before retrying; the dump file is still valid.

## 7. Decommission Bitnami (after a retention period)

Once you are confident (suggested: days, not minutes), retire the old stack.
Each step below is irreversible.

```bash
# In the GitOps repo: delete apps/postgresql.yaml (under git_repository.path,
# if set), commit, push. ArgoCD prunes the Bitnami chart.

# The StatefulSet's PVC survives pruning and still holds the old data:
kubectl delete pvc data-postgresql-0 -n keycloak

# Retired credentials (no longer created or consumed by anything):
kubectl delete secret postgresql-credentials keycloak-postgresql-credentials -n keycloak
```

Keep `keycloak-backup.sql` until after this step.
