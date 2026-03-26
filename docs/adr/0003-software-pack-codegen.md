# ADR-0003: Software Pack Codegen via ArgoCD Application Generation

## Status

Proposed

## Date

2026-03-12

## Context

NIC deploys a fixed set of foundational services (cert-manager, Keycloak, PostgreSQL, etc.) as ArgoCD Applications written to a GitOps repository. Users who want to extend their Nebari deployment with additional software packs - like [nebari-data-science-pack](https://github.com/nebari-dev/nebari-data-science-pack) - must manually author ArgoCD Application manifests and wire up Helm values for domain, storage class, Keycloak integration, and other deployment-specific settings.

This is error-prone and creates a barrier to adoption. NIC already has all the deployment context needed to generate these manifests automatically.

Software packs follow a consistent pattern:
- They are Helm charts (hosted in git repos or Helm registries)
- They optionally include a `NebariApp` CRD template for routing and auth integration
- They accept well-known Helm values for nebari integration (`nebariapp.*`, `sharedStorage.*`)

## Decision Drivers

- Users should be able to add a software pack with a single config line
- Convention over configuration - packs that follow nebari patterns should need zero manual wiring
- Software packs should remain decoupled from the foundational stack
- Support both git-hosted charts and Helm registries
- Pack authors should be able to declare requirements without NIC needing to know about every pack

## Considered Options

1. Convention-only value wiring
2. Pack-declared integration metadata only
3. Layered approach: conventions + optional pack metadata + user overrides

## Decision Outcome

Chosen option: "Option 3 - Layered approach", because it handles the common case automatically while providing escape hatches for pack-specific requirements and user customization.

### Consequences

**Good:**
- Adding a pack is a one-liner in config.yaml for packs that follow conventions
- Pack authors can declare additional requirements via nebari-integration.yaml
- Users always have final say via inline values or values files
- Foundational and software pack concerns are cleanly separated

**Bad:**
- NIC needs to fetch remote metadata (Chart.yaml, values.yaml) at deploy time
- Convention detection adds implicit behavior that could surprise users
- Two app-of-apps patterns to maintain (foundational + software-packs)

## Options Detail

### Option 1: Convention-Only Value Wiring

NIC inspects each pack's `values.yaml` and applies a fixed set of conventions. If a pack needs something outside the convention set, the user must provide it as an override.

**Pros:**
- Simplest implementation
- No extra files for pack authors to maintain

**Cons:**
- Packs with unusual requirements can't communicate what they need
- Users get no guidance on what values are missing - deploy just fails

### Option 2: Pack-Declared Integration Metadata Only

Each pack ships a `nebari-integration.yaml` that fully describes how to wire it. No implicit conventions.

**Pros:**
- Fully explicit - no surprise behavior
- Pack author controls everything

**Cons:**
- Every pack must ship this file, even when conventions would suffice
- Duplicates information already derivable from values.yaml
- Higher barrier for pack authors

### Option 3: Layered Approach (Chosen)

Three layers with clear precedence (highest wins):
1. **Conventions** - auto-wire known values by inspecting values.yaml
2. **nebari-integration.yaml** - pack declares additional required values
3. **User config** - inline values or values file override everything

**Pros:**
- 80% of cases handled by conventions alone
- Pack authors opt in to metadata only when needed
- Users always have escape hatch

**Cons:**
- More moving parts than either option alone
- Convention detection is implicit

## Design

### Config Surface

```yaml
software_packs:
  # Git repo source (minimal)
  - url: "https://github.com/nebari-dev/nebari-data-science-pack"
    version: "v0.1.0-alpha.10"

  # Git repo source with overrides
  - url: "https://github.com/nebari-dev/nebari-data-science-pack"
    version: "v0.1.0-alpha.10"
    namespace: "datascience"
    values:
      nebariapp:
        hostname: jupyter.custom.com
      sharedStorage:
        enabled: true

  # With external values file
  - url: "https://github.com/nebari-dev/nebari-data-science-pack"
    version: "v0.1.0-alpha.10"
    values_file: "overrides/data-science.yaml"

  # Helm registry source
  - chart: my-pack
    repo_url: "oci://registry.example.com/charts"
    version: "1.2.0"
```

### Convention Layer

NIC fetches the pack's `values.yaml` and inspects it for known keys. If a key exists, the convention applies:

| Detected Key | Convention Value | Source |
|-------------|-----------------|--------|
| `nebariapp.enabled` | `true` | Always when deploying in NIC |
| `nebariapp.hostname` | `<chart-name>.<domain>` | Chart.yaml name + config domain |
| `sharedStorage.storageClass` | Cluster storage class | `InfraSettings.StorageClass` |

Detection is key-existence based - if the pack's values.yaml doesn't have `sharedStorage`, NIC won't inject it. No spurious values.

### nebari-integration.yaml (Optional)

Lives in the pack repo root. Intentionally minimal today:

```yaml
required_values:
  - path: some.deep.key
    description: "Human-readable explanation of what this is"
```

If a required value isn't satisfied by conventions or user overrides, deploy fails with a clear error message. If the file doesn't exist, NIC assumes conventions are sufficient.

No version field, no templating, no kind. Extend later when we know more.

### GitOps Repository Structure

```
<git-path>/
  apps/                              # existing foundational
    root.yaml
    cert-manager.yaml
    ...
  software-packs/                    # new
    software-packs-root.yaml         # app-of-apps for software packs
    nebari-data-science-pack.yaml    # generated ArgoCD Application
  manifests/                         # existing
    ...
```

### ArgoCD Organization

Software packs get their own ArgoCD project (`software-packs`), separate from the `foundational` project. A dedicated app-of-apps (`software-packs-root`) watches the `software-packs/` directory and deploys any YAML files it finds.

The software-packs-root app-of-apps uses sync wave "6" (after all foundational apps at waves 1-5). Individual software pack apps use sync wave "10".

### Generated ArgoCD Application (git source)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nebari-data-science-pack
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: nebari-software-packs
    app.kubernetes.io/managed-by: nebari-infrastructure-core
  annotations:
    argocd.argoproj.io/sync-wave: "10"
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: software-packs
  source:
    repoURL: https://github.com/nebari-dev/nebari-data-science-pack
    targetRevision: v0.1.0-alpha.10
    path: .
    helm:
      releaseName: nebari-data-science-pack
      values: |
        nebariapp:
          enabled: true
          hostname: nebari-data-science-pack.nebari.example.com
        sharedStorage:
          storageClass: gp3
  destination:
    server: https://kubernetes.default.svc
    namespace: nebari-data-science-pack
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
```

### Generated ArgoCD Application (Helm registry source)

```yaml
spec:
  source:
    chart: my-pack
    repoURL: oci://registry.example.com/charts
    targetRevision: 1.2.0
    helm:
      releaseName: my-pack
      values: |
        # ... same convention-based values
```

### Deploy Flow

```
nic deploy -f config.yaml
  |
  v
1. Provider.Deploy() - create cluster (existing)
  |
  v
2. bootstrapGitOps() - extended
  |-- WriteAllToGit()              - foundational apps (existing)
  |-- WriteSoftwarePacksToGit()    - NEW
  |     |-- For each pack in config.software_packs:
  |     |     |-- Fetch Chart.yaml (name, version)
  |     |     |-- Fetch values.yaml (detect convention keys)
  |     |     |-- Fetch nebari-integration.yaml (if exists)
  |     |     |-- Validate required values satisfied
  |     |     |-- Merge: conventions < integration < user overrides
  |     |     |-- Generate ArgoCD Application YAML
  |     |     |-- Write to software-packs/<name>.yaml
  |     |-- Generate software-packs-root.yaml (app-of-apps)
  |     |-- Commit and push
  |
  v
3. InstallFoundationalServices() - extended
  |-- Create ArgoCD project: "software-packs" (NEW)
  |-- ApplyRootAppOfApps() (existing, foundational)
  |-- Software packs root auto-synced by ArgoCD
```

### Go Package Structure

New and modified packages:

- **`pkg/config/config.go`** - add `SoftwarePacks []SoftwarePackConfig` to `NebariConfig`
- **`pkg/softwarepack/`** - new package
  - `fetch.go` - fetch Chart.yaml, values.yaml, nebari-integration.yaml from git or registry
  - `conventions.go` - inspect values.yaml keys, build convention defaults
  - `generate.go` - render ArgoCD Application YAML from pack metadata + merged values
  - `validate.go` - check required values from nebari-integration.yaml are satisfied
- **`pkg/argocd/writer.go`** - add `WriteSoftwarePacksToGit()` function
- **`pkg/argocd/foundational.go`** - create `software-packs` ArgoCD project alongside foundational
- **`cmd/nic/deploy.go`** - call software pack generation during GitOps bootstrap

## Links

- [GitHub Issue #152](https://github.com/nebari-dev/nebari-infrastructure-core/issues/152)
- [nebari-data-science-pack](https://github.com/nebari-dev/nebari-data-science-pack) - reference software pack
