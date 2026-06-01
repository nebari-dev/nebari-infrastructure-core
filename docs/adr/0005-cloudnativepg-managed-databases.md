# ADR-0005: CloudNativePG as Foundational Database Infrastructure

## Status

Proposed

## Date

2026-05-12

## Context

NIC has no managed-Postgres contract. Software packs that need a database today each handle it independently: pick an upstream chart, accept whatever sub-chart it bundles, configure values, hope it stays usable. The pack-level lifecycle and the platform-level lifecycle are not connected. Contributors with database-backed packs route through whichever individual previously set up shared infra, or stand up their own external database. The result is a sprawl of one-off instances, each owned by whoever set it up, each managed with whatever tooling they knew. Asking "who manages X?" becomes Slack archaeology, and adding capacity routes back to the original owner every time. Responsibilities silo around individuals rather than around the platform.

The Bitnami situation makes that structural gap concrete. On 2025-08-28, Broadcom moved Bitnami container images behind a paywall (`Bitnami Secure Images`, starting at $72,000/year). All versioned image tags were moved to a frozen `bitnamilegacy/` mirror that receives no further updates; the public catalog was scheduled for deletion on 2025-09-29. Only `:latest` tags remain freely available, which is unusable for reproducible deployments. The chart source code stays Apache-2 licensed, but the images those charts reference do not.

This is already biting across the ecosystem, with each pack inventing its own workaround for the same upstream disruption:

- [`nebari-superset-pack`](https://github.com/nebari-dev/nebari-superset-pack) pins its bundled Postgres and Redis to the `bitnamilegacy/` mirror, with an explicit comment in `chart/values.yaml` that upstream defaults now "reference removed tags." The pack is running on a frozen image set.
- [`nebari-mlflow-pack`](https://github.com/nebari-dev/nebari-mlflow-pack) enables a bundled Postgres sub-chart from the upstream mlflow community chart, with values matching the standard Bitnami shape (`auth.existingSecret`, `primary.persistence`, `postgres-password` key).
- NIC core's foundational Keycloak Postgres at `pkg/argocd/templates/apps/postgresql.yaml` uses the Bitnami chart at v18.2.0.

Three packs, three independent responses to the same upstream change. The Bitnami paywall won't be the last disruption packs face; the next one will fragment the same way unless the platform owns the database contract.

Issue [#303](https://github.com/nebari-dev/nebari-infrastructure-core/issues/303) proposes adding [CloudNativePG](https://cloudnative-pg.io/) (CNPG) as a foundational service so packs request a Postgres database through the existing NebariApp CRD, the same way they request Keycloak OIDC clients via `provisionClient: true` today. The contract belongs to the platform; the operator owns the lifecycle.

## Decision Drivers

- Give packs a self-service database contract owned by the platform, so the next upstream disruption (after Bitnami) doesn't fragment the same way.
- Centralize database lifecycle management around the platform rather than around individual contributors; no person-in-the-loop for the routine "I need a Postgres" case.
- Remove the dependency on `bitnamilegacy/` frozen images for the packs currently relying on it (`nebari-superset-pack`, NIC core's Keycloak Postgres, and the implicit Bitnami-derived sub-chart in `nebari-mlflow-pack`).
- Match the existing per-pack provisioning pattern (Keycloak OIDC client) so the operator's responsibility surface stays consistent.
- Preserve pack flexibility: any pack that has a reason to use its own database elsewhere (managed RDS, hosted DBaaS, self-managed cluster) must remain free to do that. This ADR establishes a default, not a mandate.

## Considered Options

1. **Status quo**: continue using Bitnami sub-charts (via the `bitnamilegacy/` mirror) and let packs continue routing one-off database needs through individual contributors.
2. **CloudNativePG as a foundational service**: deploy CNPG into the cluster during NIC bootstrap and extend the NebariApp CRD so packs can request a managed Postgres database with `database.enabled: true`. The operator creates a CNPG `Cluster` resource, waits for readiness, and exposes credentials in a per-pack Secret that the application consumes.

## Decision Outcome

Chosen option: **Option 2 (CloudNativePG)**.

The Bitnami situation alone is enough to force a move away from the current pattern. The structural friction from siloed, ad-hoc database setups is independently worth solving, and the cleanest path to solving both at once is to ship a managed-database contract through the operator. CNPG is the option with the strongest alignment to the existing platform shape: CNCF project, no external dependencies, Kubernetes-native, uses official PostgreSQL upstream images, and follows the same per-pack request-via-CRD pattern already established for OIDC clients.

This ADR commits to:

- CNPG as the foundational operator NIC deploys for in-cluster PostgreSQL.
- A NebariApp CRD extension (sketched in #303) where packs declare `database.enabled: true` and receive a `<name>-db-credentials` Secret containing `host`, `port`, `username`, `password`, `database`, `uri`.
- A clear opt-out: packs are free to ignore the managed contract and bring their own database from anywhere. The platform default is CNPG, not a requirement.

This ADR explicitly does NOT commit to:

- A specific HA topology (single-instance vs. replicated). Default to start, revisit per-pack.
- A specific backup/restore strategy (object storage targets, PITR retention).
- Monitoring/alerting defaults beyond the metrics CNPG exports natively.
- Migration tooling for existing packs currently using bundled Bitnami sub-charts.

Those follow in design docs or follow-up ADRs once Phase 1 of the contract is in place.

### Consequences

**Good:**

- Removes the Bitnami dependency from the platform.
- Packs that need PostgreSQL get a self-service contract: declarative request via CRD, no coordination with whoever happens to own the current shared infra.
- Database lifecycle is owned by the platform (one operator, all packs), not by individual contributors.
- Reproducible across environments: the same CRD declaration produces the same database resource on any NIC-managed cluster.
- Consistent with the existing operator pattern (Keycloak OIDC clients), so contributors don't learn a new model.
- Backup, HA, observability, and TLS are available as future extensions without rewriting the per-pack contract.

**Bad:**

- Adds CNPG as a new foundational dependency alongside Envoy Gateway, cert-manager, Keycloak, and ArgoCD. One more operator to lifecycle-manage.
- Packs that want to support both the managed contract and standalone (non-NIC) deployments need to handle both database-config paths. Pattern already exists for OIDC, but doubles the surface for packs that adopt it.
- The CNPG operator itself needs upgrade paths planned. Major-version CNPG upgrades may require coordinated PostgreSQL version bumps.
- Existing deployments using bundled Bitnami sub-charts need a migration story. Out of scope for this ADR, but the work doesn't go away.

## Options Detail

### Option 1: Status quo (Bitnami sub-charts or `bitnamilegacy/` mirror)

Pack Helm charts continue to depend on Bitnami's PostgreSQL sub-chart. The chart sources are still Apache-2 licensed and remain on GitHub, but the images they reference live behind the paywall as of 2025-08-28. The `bitnamilegacy/` mirror exposes the last freely-available image versions but receives no further updates.

**Pros:**

- Zero migration cost; everything keeps working until something breaks.
- No new platform component to operate.
- Each pack owns its own database independently (no shared platform-level dependency).

**Cons:**

- Frozen `bitnamilegacy/` images accumulate unpatched CVEs. Security exposure grows monotonically.
- No reproducible upgrade path off the paywalled images for packs that want a current PostgreSQL.
- The structural problem (siloed databases, person-in-the-loop provisioning) remains unsolved. The Bitnami situation forced the question; reverting to the status quo leaves the underlying friction in place.
- Each pack continues to ship its own database sub-chart, multiplying the surface area for divergent backup, HA, and observability configurations.

### Option 2: CloudNativePG as a foundational service

CNPG is deployed during NIC bootstrap alongside the other foundational components. The NebariApp CRD gains a `database` field; packs that set `database.enabled: true` get a CNPG `Cluster` provisioned by the operator and a credentials Secret they consume via standard `envFrom` / `extraEnv` patterns.

The pattern mirrors Keycloak OIDC client provisioning: the operator manages a shared platform service, packs request per-app resources through the CRD, secrets land in per-app namespaces, the pack consumes them.

**Pros:**

- CNCF project with vendor-neutral governance. No paywall risk equivalent to Bitnami's.
- Uses official PostgreSQL upstream images. No third-party image dependency.
- Kubernetes-native: declarative `Cluster` CRDs, no external coordinator (no Patroni/repmgr/Stolon). Failover and rolling updates use the standard Kubernetes API.
- Production-ready feature set when needed: replication, continuous backup to object storage, point-in-time recovery, PgBouncer connection pooling. Available as we want them; not required by Phase 1.
- Native Prometheus metrics exporter; integrates with the existing observability stack without bolt-ons.
- TLS, certificate rotation, RBAC integration are built in.
- One operator managing N per-pack databases is more resource-efficient than N Bitnami sub-charts running side by side.

**Cons:**

- New foundational dependency to operate. CNPG itself needs lifecycle planning.
- Adds a platform requirement to packs that want managed databases. Packs still need to work without it for standalone deployments (same as the OIDC opt-out today).
- Migration tooling for existing Bitnami-based packs is non-trivial and not solved by this ADR.
- Pinning the CNPG operator version to the cluster's PostgreSQL version is a coupling worth being explicit about.

## Open Questions

- HA topology defaults: single-instance for small/local deployments, replicated for production? Per-pack override?
- Backup strategy: which object-storage backends do we support out of the box, what retention windows, where do credentials come from?
- Multi-tenancy isolation: per-pack namespace with one `Cluster` each, or shared cluster with logical databases? The CRD design in #303 implies the former; worth confirming.
- Migration path for packs currently using bundled Bitnami sub-charts: out of scope here but tracked separately.
- CNPG operator upgrades: pin to a version verified against the cluster's PostgreSQL version, same way the Keycloak chart and keycloak-config-cli are pinned together in PR #289.

## Links

- [Issue #303](https://github.com/nebari-dev/nebari-infrastructure-core/issues/303) — CNPG proposal and CRD sketch
- [CloudNativePG documentation](https://cloudnative-pg.io/docs/)
- [Bitnami catalog changes (charts#35164)](https://redirect.github.com/bitnami/charts/issues/35164)
- [Bitnami container changes (containers#83267)](https://redirect.github.com/bitnami/containers/issues/83267)
- [CNCF blog: Cloud Neutral Postgres with CloudNativePG](https://www.cncf.io/blog/2024/11/20/cloud-neutral-postgres-databases-with-kubernetes-and-cloudnativepg/)
- [PR #234](https://github.com/nebari-dev/nebari-infrastructure-core/pull/234) — ArgoCD OIDC SSO. Established the per-pack-provisioning-via-CRD pattern this ADR extends to databases.
