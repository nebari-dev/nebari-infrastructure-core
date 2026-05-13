# Timeline and Milestones

Status legend:

- ✅ shipped
- 🟡 partially shipped
- ⏳ planned, not started or in progress

The repo's current release line is `v0.1.0-alpha.*` (see recent tags and `pkg/argocd/templates/manifests/nebari-operator/kustomization.yaml`). v1.0.0 has not shipped.

## 13.1 Phase 1: Foundation

**Goals**: A working `Provider` abstraction, a real cluster provisioner, and the cluster-level bits of state and config.

| Deliverable | Status |
|-------------|--------|
| `Provider` interface and `InfraSettings` capability struct (`pkg/provider/provider.go`) | ✅ |
| Unified provider registry (`pkg/registry.Registry` with `ClusterProviders` + `DNSProviders`) | ✅ |
| AWS cluster provider (EKS via upstream `nebari-dev/eks-cluster` module, EFS, node groups) | ✅ |
| Hetzner cluster provider (via `hetzner-k3s` binary) | ✅ |
| Local cluster provider (Kind stub, driven by `make localkind-up`) | ✅ |
| `existing` cluster provider (adopt a kubeconfig) | ✅ |
| GCP cluster provider | ⏳ (registered as stub) |
| Azure cluster provider | ⏳ (registered as stub) |
| `pkg/tofu` wrapper with streaming JSON output through the status channel | ✅ |
| AWS S3 state backend with `use_lockfile = true` and auto-managed bucket lifecycle | ✅ |
| NIC CLI (`deploy`, `destroy`, `validate`, `kubeconfig`, `version`) | ✅ |
| `status` / `plan` / `state` / `unlock` subcommands | ⏳ |
| Integration tests against LocalStack via `make test-integration-local` | ✅ |
| CI: unit tests + lint + race + coverage upload | ✅ |

## 13.2 Phase 2: Foundational Software

**Goals**: GitOps bootstrap and the opinionated platform stack.

| Deliverable | Status |
|-------------|--------|
| ArgoCD install via embedded Helm Go SDK (`pkg/helm`) | ✅ |
| GitOps repo bootstrap (`pkg/argocd`, `pkg/git`) | ✅ |
| `file://` GitOps repos for local development | ✅ |
| Cert-manager + cluster-issuers + initial Certificates | ✅ |
| Envoy Gateway + gateway-config + httproutes | ✅ |
| PostgreSQL + Keycloak (Codecentric keycloakx chart) | ✅ |
| MetalLB + metallb-config (conditional on `InfraSettings.NeedsMetalLB`) | ✅ |
| OpenTelemetry Collector | ✅ |
| Nebari Landing Page | ✅ |
| Nebari Operator (Kustomized from `nebari-dev/nebari-operator`) | ✅ |
| Full LGTM backend (Loki, Grafana, Tempo, Mimir, Promtail) | ⏳ |

## 13.3 Phase 3: Operator Integration

**Goals**: Apps integrate via the `NebariApp` CRD.

The Nebari Operator is developed out-of-tree at [`nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator). This phase's status from NIC's perspective:

| Deliverable | Status |
|-------------|--------|
| Operator deployed by NIC via ArgoCD + Kustomize | ✅ |
| Operator version pinned in the Kustomize manifest | ✅ |
| `InfraSettings.KeycloakBasePath` and `HTTPSPort` propagated via Kustomize patches | ✅ |
| Operator-side reconciliation of `NebariApp` CRs | upstream-owned |
| Operator-side `SecurityPolicy` and OIDC plumbing | upstream-owned |
| Grafana dashboard provisioning | ⏳ (depends on LGTM stack) |

## 13.4 Phase 4: Multi-Cloud Parity

**Goals**: Make the secondary cluster providers real.

| Deliverable | Status |
|-------------|--------|
| GCP provider (GKE, VPC, Filestore) | ⏳ |
| Azure provider (AKS, VNet, Azure Files) | ⏳ |
| Provider parity tests | ⏳ |
| Multi-cloud CI workflows | ⏳ |

Note: [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md) (Proposed, 2026-04-15) re-frames the multi-cloud roadmap. Out-of-tree provider plugins would let GCP, Azure, and third-party providers ship independently. The in-tree path above is the original plan; the plugin path is the proposed direction.

## 13.5 Phase 5: Observability

**Goals**: NIC observes itself, and clusters get a real telemetry backend.

| Deliverable | Status |
|-------------|--------|
| OpenTelemetry instrumentation in library code | 🟡 (per `CLAUDE.md` exemptions for `pkg/status` and byte/line helpers in `pkg/tofu`; operation-granularity `TerraformExecutor` wrappers tracked as outstanding work) |
| Status-channel seam between `pkg/` and `cmd/` (`pkg/status`, `cmd/nic/status_handler.go`) | ✅ |
| OTLP exporter wiring (`OTEL_EXPORTER=otlp`, `OTEL_ENDPOINT=...`) | ✅ |
| LGTM backend deployed on cluster | ⏳ |
| Grafana dashboards for NIC operations | ⏳ |

## 13.6 Phase 6: Production Hardening

**Goals**: GA-readiness items.

| Deliverable | Status |
|-------------|--------|
| Documented upgrade paths between alpha releases | ⏳ |
| Comprehensive end-to-end testing across providers | ⏳ |
| Backup and restore for foundational software | ⏳ |
| Compliance profiles (HIPAA, SOC2, PCI-DSS) | ⏳ |
| v1.0.0 release | ⏳ |

## 13.7 Known Issues Tracked

A few of the open issues that affect the picture above:

- [#63](https://github.com/nebari-dev/nebari-infrastructure-core/issues/63) Ctrl-C during destroy leaves OpenTofu state locked (bug)
- [#64](https://github.com/nebari-dev/nebari-infrastructure-core/issues/64) Add `nic unlock` command for stuck state locks (enhancement)
- [#65](https://github.com/nebari-dev/nebari-infrastructure-core/issues/65) MetalLB deployed on AWS (bug; `InfraSettings.NeedsMetalLB` fix)
- [#66](https://github.com/nebari-dev/nebari-infrastructure-core/issues/66) Pipe OpenTofu output through slog + pretty-print option (enhancement)
- [#241](https://github.com/nebari-dev/nebari-infrastructure-core/issues/241) Avoid redundant `tofu init` / module downloads during deploy (perf)
- [#300](https://github.com/nebari-dev/nebari-infrastructure-core/issues/300) Audit and rewrite design docs against current code (this audit)
