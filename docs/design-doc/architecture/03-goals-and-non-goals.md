# Goals and Non-Goals

### 3.1 Primary Goals

Status icons reflect current state, not original ambition:

- ✅ shipped
- 🟡 partially shipped
- ⏳ planned

**Phase 1 (MVP):**

1. ✅ Deploy production Kubernetes on **AWS (EKS)** and **Hetzner (k3s)**; ✅ local Kind clusters for development; ✅ `existing` provider to adopt a pre-provisioned cluster; ⏳ GCP and Azure providers (currently registered stubs)
2. ✅ Deploy foundational software via ArgoCD: cert-manager, cluster-issuers, certificates, Envoy Gateway, gateway-config, httproutes, postgresql, Keycloak, MetalLB (where needed), OpenTelemetry Collector, nebari-operator, nebari-landingpage
3. ✅ Nebari Operator deployed as a foundational app (operator source lives in [`nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator))
4. ✅ Working **auth** (Keycloak with OIDC SSO into ArgoCD) and **routing** (Envoy Gateway with Kubernetes Gateway API). 🟡 **Observability**: the OpenTelemetry Collector ships, but a full LGTM backend (Loki / Grafana / Tempo / Mimir) does not - that work is deferred.
5. ✅ Configuration-driven cluster provisioning, with per-provider backing tools (OpenTofu for AWS, hetzner-k3s for Hetzner, Kind for local, kubeconfig adoption for existing). State management is provider-specific; AWS uses S3 with native lockfile-based locking.
6. 🟡 OpenTelemetry instrumentation in library code (CLAUDE.md documents exemptions for `pkg/status` and byte/line-level helpers inside `pkg/tofu`; operation-granularity wrappers on `TerraformExecutor` are tracked as outstanding work)
7. ✅ A documented `Provider` interface and `InfraSettings` capability struct so adding a new cluster provider does not require changes to CLI or `pkg/argocd`

**Phase 2 (Iteration):**

1. ⏳ Full LGTM observability backend (Loki / Grafana / Tempo / Mimir)
2. ⏳ Advanced Keycloak integration (SAML, LDAP federation)
3. ⏳ Custom Grafana dashboards for NIC-deployed clusters
4. ⏳ Automated backup and restore for foundational software
5. ⏳ Multi-cluster support (deploy multiple clusters from one CLI)
6. ⏳ Cost optimization features (spot instances, autoscaling policies)
7. ⏳ Compliance profiles (HIPAA, SOC2, PCI-DSS)
8. ⏳ Auto-provisioning of Git repositories and CI workflows (consumption of an existing repo is already supported via `git_repository:`)
9. ⏳ Software pack specification - declare full stacks (databases, caching, apps) alongside foundational software
10. ⏳ Stack templates for common use cases (data science, ML platform, web apps)
11. ⏳ Out-of-tree provider plugins as described in [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md)

**Future Goals:**

1. ⏳ Service-mesh integration (Istio/Linkerd)
2. ⏳ Advanced security (OPA/Gatekeeper policies)
3. ⏳ Edge deployment support
4. ⏳ Hybrid cloud networking
5. ⏳ AI/ML workload optimizations (GPU pools, model serving)

### 3.2 Explicit Non-Goals

**Not Doing:**

- ❌ Backward compatibility with old Nebari (clean break)
- ❌ Managed database services (RDS, CloudSQL, etc.)
- ❌ User application deployment (beyond foundational software). Apps install themselves via ArgoCD with `NebariApp` CRs.
- ❌ Windows node pools (Linux only)
- ❌ Custom Kubernetes distributions. The supported distributions are EKS (AWS), k3s (Hetzner via hetzner-k3s), Kind (local dev), and any pre-existing CNCF-conformant cluster (via the `existing` provider).
- ❌ Non-standard authentication (only Keycloak)
- ❌ Forcing every provider through OpenTofu. The `Provider` interface is the contract; the backing tool is provider-specific.

---
