# Foundational Software Stack

## 10.1 Overview

NIC deploys an opinionated set of foundational platform services on every cluster. After the cluster provider finishes provisioning Kubernetes, NIC:

1. Installs ArgoCD into the `argocd` namespace via the embedded Helm Go SDK (`pkg/helm`).
2. Renders ArgoCD `Application` manifests for the rest of the stack into a Git repository (`pkg/argocd`).
3. Lets ArgoCD sync the stack via the `root.yaml` app-of-apps and sync waves.

The stack is intentionally small. A full LGTM observability backend (Loki / Grafana / Tempo / Mimir) is **not** part of it; only an OpenTelemetry Collector is shipped. The LGTM backend is a software pack ([`lgtm-pack`](https://github.com/nebari-dev/lgtm-pack)) that installs on top of the foundation.

## 10.2 Components (Actual)

The authoritative app set is the YAML under `pkg/argocd/templates/apps/`:

| Component | App manifest | Purpose |
|-----------|--------------|---------|
| **cert-manager** | `cert-manager.yaml` | TLS certificate automation |
| **trust-manager** | `trust-manager.yaml` | Distributes CA trust bundles across namespaces (cert-manager's trust-manager) |
| **trust-bundle** | `trust-bundle.yaml` | `Bundle` resource defining the cluster CA trust bundle |
| **cluster-issuers** | `cluster-issuers.yaml` | `ClusterIssuer` resources (selfsigned and/or Let's Encrypt) |
| **certificates** | `certificates.yaml` | Initial `Certificate` resources for foundational hostnames |
| **Envoy Gateway** | `envoy-gateway.yaml` | Kubernetes Gateway API implementation |
| **gateway-config** | `gateway-config.yaml` | `Gateway` and listener configuration |
| **httproutes** | `httproutes.yaml` | Initial `HTTPRoute` resources for foundational services |
| **securitypolicies** | `securitypolicies.yaml` | Envoy Gateway `SecurityPolicy` resources (OIDC enforcement at the gateway) |
| **postgresql** | `postgresql.yaml` | Bitnami PostgreSQL; backs Keycloak today |
| **CloudNativePG** | `cloudnative-pg.yaml` | CloudNativePG operator (operator-only install per [ADR-0007](../../adr/0007-cloudnativepg-managed-databases.md); per-database `Cluster` resources are created separately). Installed foundationally as Keycloak's DB backend migrates to CNPG |
| **Keycloak** | `keycloak.yaml` | OIDC identity provider (Codecentric keycloakx chart - context path `/auth`) |
| **MetalLB** | `metallb.yaml` | Bare-metal `LoadBalancer` implementation (only when `InfraSettings.NeedsMetalLB` is true) |
| **metallb-config** | `metallb-config.yaml` | `IPAddressPool` and `L2Advertisement` for MetalLB |
| **longhorn-backup** | `longhorn-backup.yaml` | Longhorn `BackupTarget`, the snapshot/backup `RecurringJob`s, and the `allow-recurring-job-while-volume-detached` Setting (only when `backups.longhorn` is enabled) |
| **OpenTelemetry Collector** | `opentelemetry-collector.yaml` | Telemetry pipeline (no backend deployed yet) |
| **Nebari Operator** | `nebari-operator.yaml` | Reconciles `NebariApp` CRs; source lives in `nebari-dev/nebari-operator` |
| **Nebari Landing Page** | `nebari-landingpage.yaml` | React/Go service catalog UI |
| **root** | `root.yaml` | App-of-apps entry point that owns all of the above |

**Longhorn is the exception to the GitOps rule.** The Longhorn chart itself is installed directly via Helm by `pkg/storage/longhorn.Install`, not as an ArgoCD `Application`, because it has to be in place before anything can claim a `StorageClass` from it. Only the backup resources above are GitOps-managed.

Not foundational apps (older docs reference them as if they were): Grafana, Loki, Mimir, Tempo, Promtail. Grafana, Loki, Mimir, and Tempo ship in the [`lgtm-pack`](https://github.com/nebari-dev/lgtm-pack) software pack, installed on top of the foundation; Promtail is not deployed by anything NIC owns.

## 10.3 GitOps Layout

NIC does not pull these manifests from a separate `nebari-foundational-software` repo. The templates live inside this repo (`pkg/argocd/templates/`) and are rendered at deploy time into the GitOps repository resolved from the `repository:` provider block. ArgoCD's source-of-truth for the deployed stack is therefore the user's own repo, which makes everything inspectable, auditable, and overridable.

Sketch of what `pkg/argocd` writes into the GitOps repo at the `repository.existing.path` subdirectory:

```
<repo>/<path>/
├── nic-config.yaml                  # Copy of nebari-config.yaml (auth holds env-var names, not secrets)
├── .bootstrapped                    # Marker file
├── apps/                            # One ArgoCD Application per app; root.yaml points here
│   ├── root.yaml                    # App-of-apps root (recurse: false)
│   ├── cert-manager.yaml
│   ├── trust-manager.yaml           # Gated on trust_bundle
│   ├── trust-bundle.yaml            # Gated on trust_bundle
│   ├── cluster-issuers.yaml
│   ├── certificates.yaml
│   ├── envoy-gateway.yaml
│   ├── gateway-config.yaml
│   ├── httproutes.yaml
│   ├── securitypolicies.yaml        # Gated on Longhorn
│   ├── postgresql.yaml
│   ├── cloudnative-pg.yaml
│   ├── keycloak.yaml
│   ├── metallb.yaml                 # Gated on NeedsMetalLB
│   ├── metallb-config.yaml          # Gated on NeedsMetalLB
│   ├── longhorn-backup.yaml         # Gated on backups.longhorn
│   ├── opentelemetry-collector.yaml
│   ├── nebari-operator.yaml
│   └── nebari-landingpage.yaml
└── manifests/                       # Plain-manifest and values content, grouped by concern
    ├── keycloak/                    # Realm-setup job, values
    ├── metallb/
    ├── nebari-operator/             # Kustomize patch over the upstream operator
    ├── networking/                  # Gateway, HTTPRoutes, SecurityPolicies, ReferenceGrants
    ├── security/                    # ClusterIssuers, Certificates, trust bundle
    └── storage/                     # Longhorn, longhorn-backup
```

The layout mirrors `pkg/argocd/templates/` one-for-one; `WriteManifests` walks the embedded template tree and renders it into the repo preserving relative paths. Files prefixed with `.` or `_` are skipped.

Gated templates are **removed**, not merely skipped, when their gate turns off. Toggling a feature off on an already-bootstrapped repo deletes its manifests so ArgoCD prunes the resources, rather than leaving them orphaned in git.

## 10.4 ArgoCD Bootstrap

ArgoCD is installed in the `argocd` namespace by `pkg/argocd/install.go` via the embedded Helm Go SDK. It is configured with:

- Keycloak OIDC for SSO (client secret generated by `cmd/nic/deploy.go` and passed into both the ArgoCD Helm values and the Keycloak realm-setup job)
- Read credentials for the GitOps repo (from `repository.existing.argocd_auth`, falling back to `auth`; a local repository needs none)
- `repoURL` and `path` from the resolved `repository.Source`

After ArgoCD comes up, `pkg/argocd/bootstrap.go:ApplyRootAppOfApps` applies the root `Application` directly to the cluster via client-go. Everything else syncs from there.

## 10.5 AppProject Scoping

NIC creates three `AppProject`s (`pkg/argocd/project.go`), applied directly to the cluster during bootstrap alongside the root app rather than written into the GitOps repo:

| Project | Purpose | Source repos | Destinations |
|---------|---------|--------------|--------------|
| `foundational` | The NIC-owned stack in §10.2. Every foundational `Application` sets `project: foundational`. | Derived from the embedded templates | Derived from the embedded templates |
| `nebari-apps` | Software packs (`NebariApp`-based user applications) | `'*'` (any chart source) | `namespace: '*'` |
| `default` | Deny-all. Exists so ArgoCD's built-in project cannot be used as a project-escape hatch. | `[]` | `[]` |

`foundational`'s scopes are **derived, not hardcoded**: `deriveProjectScopes` renders the embedded app and manifest templates and collects the distinct `repoURL` and namespace values they use. Adding an app therefore widens the project automatically, with no second list to keep in sync.

That derivation only recognizes specific shapes: namespaces from `metadata.namespace` and `spec.destination.namespace`, source repos from `spec.source.repoURL` and `spec.sources[].repoURL`. A template that declares a namespace or repo *only* some other way (a deeply-nested field, or a Kustomize top-level `namespace:`) is invisible to the scan and must also declare it via a recognized shape, or the app will be refused by its own project at sync time.

`nebari-apps`, by contrast, is **not** scoped: its `sourceRepos` is `'*'`. Packs legitimately ship from several places (the Nebari Helm index, that index's `oci://quay.io/nebari/charts` mirror, and third-party git repos), and NIC has no configuration surface for declaring which are trusted, so any fixed list refuses valid packs. Replacing the wildcard with an operator-declared allow-list is tracked in [#530](https://github.com/nebari-dev/nebari-infrastructure-core/issues/530) and is the `hardened` posture in [ADR-0010](../../adr/0010-high-security-mode.md).

`foundational` and `nebari-apps` both keep wildcard `clusterResourceWhitelist` / `namespaceResourceWhitelist` entries. That is deliberate for now: repo-and-namespace scoping is the boundary `foundational` enforces, and kind-level restriction is tracked as admission-controller follow-up work in [#480](https://github.com/nebari-dev/nebari-infrastructure-core/issues/480).

## 10.6 InfraSettings Drives Conditional Deployment

The Provider interface returns `InfraSettings` (see `pkg/providers/cluster/provider.go`), and the foundational layer reads from it instead of branching on provider name:

- **`NeedsMetalLB`** - if false, the MetalLB apps are skipped entirely
- **`MetalLBAddressPool`** - feeds `metallb-config`'s `IPAddressPool`
- **`StorageClass`** - default `StorageClass` name for foundational PVCs (postgresql, etc.)
- **`KeycloakBasePath`** - `/auth` for the Codecentric keycloakx chart; empty for upstream/Bitnami Keycloak
- **`HTTPSPort`** - Gateway HTTPS listener port (`443` normalized from `0`; can be overridden e.g. for local-dev on `8443`)
- **`LoadBalancerAnnotations`** - applied to the Gateway's provisioned `LoadBalancer` Service
- **`EFSStorageClass`** - name of the EFS-backed `StorageClass` if available (AWS-only)

Adding a new provider-shaped capability means adding a field to `InfraSettings` and populating it in each provider's `InfraSettings(cfg)`. There must be no `switch cfg.Cluster.ProviderName()` in `pkg/argocd` or `cmd/nic`.

## 10.7 Sync Waves

Cross-app dependencies are expressed via ArgoCD sync waves on each `Application`. The waves as they stand in `pkg/argocd/templates/apps/`:

| Wave | Apps | Why here |
|------|------|----------|
| 1 | envoy-gateway, metallb, metallb-config | CRDs and the `LoadBalancer` implementation must exist before anything asks for an address |
| 2 | cert-manager, gateway-config | cert-manager CRDs/webhooks before any issuer or `Certificate`; the `Gateway` once its CRDs are in |
| 3 | cluster-issuers, certificates, httproutes, trust-manager, cloudnative-pg, securitypolicies, longhorn-backup | Issuers and certs on top of cert-manager; routes on top of the `Gateway`; operators before the resources that need them |
| 4 | postgresql, keycloak, opentelemetry-collector, trust-bundle | Keycloak needs its database and a served certificate; the trust `Bundle` needs trust-manager |
| 5 | nebari-operator | Reconciles `NebariApp` CRs, so it wants routing and auth already up |
| 6 | nebari-landingpage | Discovers services registered by everything above |

The waves are the authoritative ordering; do not infer it from the table in §10.2. Note in particular that cert-manager is **not** first: Envoy Gateway and MetalLB precede it.

## 10.8 Health and Readiness

Foundational software health is observed via ArgoCD's own sync/health status, not a hardcoded list in NIC. NIC's `deploy` command does not block waiting for every component; it prints follow-up instructions (how to reach ArgoCD, how to reach Keycloak) and exits. Users who want to wait for full health can watch ArgoCD's UI or run `kubectl wait` against the relevant Applications.

A first-class `nic status` / health-check subcommand does not exist today; that work is tracked but not started.

## 10.9 Versions

Component versions are pinned in the individual template YAML files under `pkg/argocd/templates/apps/`. Search those files for `targetRevision:` and `version:` fields. The nebari-operator version is pinned in `pkg/argocd/templates/manifests/nebari-operator/kustomization.yaml`.

Bumping a foundational version is a config change inside the template file plus an `argocd app sync` on the deployed cluster.
