# ADR-0003: Separate Binary for Local Development

## Status

Proposed

## Date

2026-03-13

## Context

Running the full Nebari foundational software stack on a local K3s cluster currently consumes approximately 6GB of RAM. This makes local development and evaluation impractical on most developer laptops, where users typically have 16GB total and need RAM for their IDE, browser, and other tools.

The foundational stack today deploys unconditionally:

| Component | Approximate RAM | Purpose |
|-----------|----------------|---------|
| ArgoCD | 500MB-1GB | GitOps reconciliation |
| Keycloak + PostgreSQL | 1-1.5GB | SSO/identity management |
| Envoy Gateway | 100-200MB | API gateway/routing |
| Cert-Manager | 100-200MB | TLS certificate management |
| MetalLB | 100MB | Load balancing (local only) |
| OpenTelemetry Collector | 200-500MB | Metrics/logs/traces |
| Nebari Operator | 100-200MB | NebariApp CRD reconciliation |
| K3s control plane | 500MB-1GB | Kubernetes |

For a single-user local environment, much of this is unnecessary. Multi-user authentication (Keycloak), GitOps reconciliation (ArgoCD), and production observability (OpenTelemetry) serve team/production use cases that don't apply when one person is running Nebari on their laptop.

The goal is to provide a realistic local install option that runs within 3-4GB total RAM while maintaining compatibility with the software pack ecosystem.

## Decision Drivers

- **Memory budget**: Must run comfortably within 3-4GB total, leaving room for workloads like JupyterHub alongside the base platform
- **Zero-config startup**: A data scientist evaluating Nebari should be able to start a local environment without writing a YAML config file
- **Software pack compatibility**: Local and cloud deployments must consume the same software pack format so packs work across both targets
- **UX clarity**: Local development and cloud infrastructure orchestration are different tasks for different audiences - the tooling should reflect that
- **Testability**: Software packs need automated CI validation, and a lightweight local environment is the cheapest way to provide that
- **Maintenance burden**: Whatever we build must not create excessive divergence between local and cloud code paths

## Considered Options

1. **Separate binary (`nic-local`)** - Dedicated CLI for local development with K3d, minimal foundational stack, direct Helm installs
2. **Profile mode in `nic`** - Add local profile/flags to the existing binary that conditionally strips services
3. **Lighter middleware in `nic`** - Replace ArgoCD with Flux for local, make Keycloak optional, keep single binary

## Decision Outcome

Chosen option: **Separate binary (`nic-local`)**, because the local and cloud deployment paths serve fundamentally different users with different requirements. `nic` is built around infrastructure orchestration - OpenTofu execution, state backends, provider credentials, ArgoCD GitOps. None of that applies when a data scientist wants to try Nebari on their laptop. Forcing local mode into `nic`'s execution model means either degrading the cloud UX with conditional paths everywhere, or giving local users a confusing experience full of concepts they don't need. A separate binary lets each tool be good at its job.

### Consequences

**Good:**
- Base memory footprint drops to ~600-900MB, well within the 3-4GB target
- Zero-config experience: `nic-local start` just works
- Software pack CI tests become cheap and fast (K3d cluster in GitHub Actions)
- Clear separation of concerns - neither tool compromises its UX for the other
- On-ramp for new users: try locally, graduate to cloud deployment with `nic`

**Bad:**
- Two binaries to maintain, release, and document
- Shared code (software pack loading, convention wiring) must be factored into shared packages
- Risk of drift between local and cloud if the shared interface (software packs, NebariApp CRs) isn't well-defined

## Options Detail

### Option 1: Separate Binary (`nic-local`) - Recommended

A dedicated CLI that manages a local Nebari environment using K3d (K3s-in-Docker). The binary lives in this repository at `cmd/nic-local/` and shares Go packages with `nic` for software pack handling.

**CLI interface:**

```
nic-local start             # Creates K3d cluster, installs foundational services
nic-local stop              # Stops the K3d cluster (preserves state)
nic-local destroy           # Tears down everything
nic-local install <pack>    # Installs a software pack
nic-local remove <pack>     # Removes a software pack
nic-local status            # Shows what's running
```

**Foundational stack (local):**

| Component | RAM | Notes |
|-----------|-----|-------|
| K3d (K3s control plane) | 300-400MB | Traefik disabled, SQLite backend |
| Envoy Gateway | 100-200MB | Same as cloud for routing parity |
| Cert-Manager | 100MB | Self-signed certificates only |
| Nebari Operator | 100-200MB | `KEYCLOAK_ENABLED=false`, routing + TLS only |
| **Base total** | **600-900MB** | Estimated, not yet measured |

Note: These memory figures are estimates based on published resource requests and typical observed usage for each component. They need to be validated with a running K3d cluster before committing to the 3-4GB target. The wide ranges reflect variance across different workload patterns and K3s configurations.

**What's removed vs cloud and why:**

| Component | Why it's removed |
|-----------|-----------------|
| ArgoCD | No GitOps reconciliation needed for single-user. The CLI applies Helm charts directly. |
| Keycloak + PostgreSQL | No multi-user SSO needed. Apps use native auth (e.g., JupyterHub DummyAuthenticator) or no auth. |
| OpenTelemetry Collector | Production observability is unnecessary for local development. |
| MetalLB | K3d handles port exposure via `extraPortMappings` and K3s ServiceLB. Note: MetalLB is currently deployed on all providers due to [issue #65](https://github.com/nebari-dev/nebari-infrastructure-core/issues/65), which should be fixed independently. |

**Why K3d over Kind:**

K3d wraps K3s, which combines control plane components (API server, scheduler, controller-manager) into a single process and uses SQLite instead of etcd for cluster state. Kind uses kubeadm, which runs each control plane component as a separate static pod including a full etcd instance. The consolidated architecture is the primary source of K3d's memory savings (~200MB less than Kind). K3d provides the same developer experience as Kind (Docker-based lifecycle, port mappings) with a smaller footprint, and works identically in GitHub Actions for CI.

K3d allows disabling the default Traefik ingress, so we install Envoy Gateway instead to maintain parity with the cloud routing stack:

```bash
k3d cluster create nebari --k3s-arg="--disable=traefik@server:0"
```

**Nebari Operator in local mode:**

The operator's code supports running without Keycloak. Setting `KEYCLOAK_ENABLED=false` skips Keycloak provider initialization - the provider is never instantiated, no admin credentials are loaded, no OAuth client provisioning runs. The operator continues to:

- Watch NebariApp CRDs
- Create/update HTTPRoutes via Envoy Gateway
- Manage TLS certificates via cert-manager
- Report status conditions (RoutingReady, TLSReady, Ready)

This means software packs that create NebariApp CRs work identically on both local and cloud - the operator reconciles them into working routes regardless of deployment target.

**Prerequisite:** While the operator code has this capability (confirmed via code review of the `KEYCLOAK_ENABLED` flag in `internal/config/auth.go`), it has not been integration-tested in a Keycloak-free environment. Validating this is a prerequisite for this work - specifically, that the operator starts cleanly with `KEYCLOAK_ENABLED=false` and reconciles NebariApp CRs through to `Ready` status with only routing and TLS (no auth).

**Software pack installation flow:**

When a user runs `nic-local install <pack>`:

1. Fetch pack metadata (chart repo, version, values template, NebariApp spec)
2. Apply convention-based value wiring (domain=nebari.local, storageClass=local-path, auth=disabled)
3. Run `helm install` with the wired values
4. The Helm chart creates the workload Deployment/Service and a NebariApp CR
5. The operator reconciles the NebariApp CR into an HTTPRoute
6. The app is accessible at the configured hostname

This is the local equivalent of what `nic` does on the cloud side via issue #152 (software pack codegen into ArgoCD Applications). Same pack definition, different rendering backend.

**Authentication strategy for local:**

For single-user local, gateway-level OIDC enforcement is unnecessary. Software packs set `auth.enabled: false` in their NebariApp CR for the local profile. Apps that need some form of login (JupyterHub, conda-store) use their native authentication mechanisms (token-based, simple password, DummyAuthenticator).

If a user wants SSO locally (advanced use case), the operator supports `generic-oidc` provider - they can point at an external OIDC provider (Google, GitHub) without needing Keycloak.

**Software pack authoring requirement:** Packs that create NebariApp CRs must support running with `auth.enabled: false`. This is a convention that needs to be documented as part of the software pack authoring guidelines. Packs should treat gateway-level auth as optional and support a local profile where the application itself handles authentication (or runs unauthenticated). The NebariApp CR's `auth` field is already optional in the operator's CRD - packs just need to not require it.

**Cluster lifecycle (stop/start):**

`nic-local stop` pauses the K3d cluster (Docker containers stopped, volumes preserved). `nic-local start` on an existing cluster resumes it - all installed packs, Helm releases, and cluster state survive the restart. No re-installation needed. `nic-local destroy` deletes the cluster and all volumes. This maps to K3d's native `k3d cluster stop`/`k3d cluster start`/`k3d cluster delete` commands.

**Shared code between `nic` and `nic-local`:**

```
pkg/
  softwarepack/       # Shared: pack loading, convention wiring, validation
    pack.go           # Pack metadata types and loading
    conventions.go    # Convention-based value wiring
    render.go         # Render to Helm args or ArgoCD Application
  provider/           # Used by nic only (cloud infrastructure)
  argocd/             # Used by nic only (GitOps bootstrap)
  tofu/               # Used by nic only (OpenTofu execution)

cmd/
  nic/                # Cloud infrastructure CLI
  nic-local/          # Local development CLI
```

The `pkg/softwarepack/` package is the shared contract. Both tools load pack definitions the same way, apply the same convention detection, then diverge only at the rendering step: `nic` produces ArgoCD Application manifests, `nic-local` produces Helm install arguments.

**Typical local deployment memory profile:**

| Component | RAM |
|-----------|-----|
| Base platform | ~600-900MB |
| Landing page | ~50-100MB |
| JupyterHub (DummyAuth, single user) | ~500MB-1GB |
| conda-store | ~300-500MB |
| **Typical total** | **~1.5-2.5GB** |

Well within the 3-4GB target with room for the notebooks and environments the user actually wants to run.

**Pros:**
- Achieves ~600-900MB base footprint, well under the 3-4GB target
- Zero-config: `nic-local start` with no YAML file
- Clean UX tailored to the local use case
- Software packs work unchanged across local and cloud
- Ideal for CI smoke testing of software packs
- Clear on-ramp: local evaluation leads naturally to cloud deployment

**Cons:**
- Two binaries to build, test, release, and document
- Shared packages (`pkg/softwarepack/`) must be carefully designed to serve both consumers
- Users must understand which tool to use (mitigated by clear naming and docs)

### Option 2: Profile Mode in `nic`

Add a `--profile local` flag or infer local mode from `provider: local` in `config.yaml`. Conditionally skip ArgoCD, Keycloak, and OTel during deploy. The operator runs with `KEYCLOAK_ENABLED=false`. Software packs are installed via a new `nic install-pack` command that does direct Helm installs in local mode.

**Pros:**
- Single binary to maintain and release
- Users learn one tool
- Shared code is implicit (same binary)

**Cons:**
- A `nic init --local` could generate a minimal config, but the user still interacts with concepts designed for cloud orchestration (deploy command, provider selection, state backend config even if defaulted). The abstraction leaks.
- Feature flags and conditional paths throughout the deploy pipeline - every foundational service install needs "if local, skip this" guards
- `nic deploy` becomes two very different code paths behind one command, making both harder to reason about and test
- `nic install-pack` behaves completely differently depending on profile (Helm install vs ArgoCD Application codegen), which is surprising to users and increases the testing surface

### Option 3: Lighter Middleware

Keep the single `nic` binary. Replace ArgoCD with Flux for local deployments (smaller footprint). Make Keycloak optional via config. Keep the operator. This is the smallest code change.

**Pros:**
- Minimal code changes to `nic`
- Still one binary
- Flux provides some GitOps reconciliation if wanted

**Cons:**
- Flux saves only ~200-400MB over ArgoCD, likely landing at 4-5GB total, above the 3-4GB target
- Introduces a third deployment backend (ArgoCD for cloud, Flux for local, plus direct Helm for packs) increasing maintenance burden
- Still requires a config file
- Doesn't address the fundamental UX mismatch between infrastructure orchestration and local development
- Software pack installation path is unclear - do packs go through Flux?

## CI Strategy

A lightweight local environment enables cheap, fast CI for software pack validation. The test validates that a pack's Helm chart produces healthy pods and that the operator correctly reconciles NebariApp CRs.

**Pack smoke test (runs on every pack PR, ~3-5 minutes):**

```yaml
jobs:
  smoke-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Create K3d cluster
        run: |
          k3d cluster create nebari --k3s-arg="--disable=traefik@server:0"
      - name: Install foundational services
        run: |
          helm install cert-manager jetstack/cert-manager --set crds.enabled=true --wait
          helm install envoy-gateway envoyproxy/gateway-helm --wait
          helm install nebari-operator nebari/nebari-operator --set keycloak.enabled=false --wait
      - name: Install pack under test
        run: |
          helm install data-science ./charts/nebari-data-science-pack -f test/values-local.yaml
      - name: Verify
        run: |
          kubectl wait nebariapp --all --for=condition=Ready --timeout=300s
          kubectl wait pods --all -A --for=condition=Ready --timeout=300s
      - name: Teardown
        if: always
        run: k3d cluster delete nebari
```

This test uses plain Helm - no `nic` or `nic-local` required. It validates the pack itself: chart renders correctly, NebariApp CRs are well-formed, operator reconciles them, pods start. How the pack was installed (ArgoCD, `nic-local`, or direct Helm) is irrelevant to pack correctness.

**Drift risk:** The pack smoke test installs foundational services manually via Helm, not through `nic-local start`. This means the smoke test environment could diverge from what `nic-local` actually produces (different chart versions, different default values). To mitigate this, `nic-local` should export its foundational service definitions (chart versions, values) as a testable artifact, and the smoke test should source its Helm install parameters from that same source of truth. Alternatively, the pack smoke test could just call `nic-local start` directly - the cost is a slightly slower test (~1 minute overhead) but eliminates drift entirely.

**Separate tests for each tool's integration:**

| Test | What it validates | Trigger | Cost |
|------|-------------------|---------|------|
| Pack smoke test | Helm chart + NebariApp CRs + operator | Every pack PR | ~3 min |
| `nic-local` integration | CLI lifecycle (start/install/remove/destroy) | Every `nic-local` PR | ~5 min |
| `pkg/softwarepack` unit tests | Pack loading, convention wiring, validation | Every PR touching shared code | ~10 sec |
| `nic` cloud integration | Infrastructure provisioning + ArgoCD + GitOps | Release/manual | ~20 min |

## Prerequisites

Before implementation, these items need to be validated:

1. **Operator without Keycloak**: Integration test the nebari-operator with `KEYCLOAK_ENABLED=false` in a K3d cluster. Confirm it starts cleanly, reconciles NebariApp CRs to `Ready`, and creates HTTPRoutes without errors.
2. **Memory baseline**: Measure actual RAM consumption of the proposed local stack (K3d + Envoy Gateway + cert-manager + operator) to confirm the 600-900MB estimate.
3. **Software pack authoring guidelines**: Define the convention that packs must support `auth.enabled: false` and document the local profile value wiring expectations.

## Links

- [Issue #65: MetalLB deployed on all providers](https://github.com/nebari-dev/nebari-infrastructure-core/issues/65) - Bug where MetalLB is unconditionally deployed
- [Issue #152: Software pack codegen](https://github.com/nebari-dev/nebari-infrastructure-core/issues/152) - Cloud-side software pack installation via ArgoCD Application generation
- [PR #155: Landing page with NebariApp CRs](https://github.com/nebari-dev/nebari-infrastructure-core/pull/155) - Landing page integration that depends on NebariApp CRD reconciliation
- [K3d documentation](https://k3d.io/) - K3s-in-Docker for local Kubernetes clusters
- [Nebari Operator](https://github.com/nebari-dev/nebari-operator) - NebariApp CRD controller with pluggable auth providers
