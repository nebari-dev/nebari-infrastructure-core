# Appendix

## 17.1 Glossary

| Term              | Definition                                            |
| ----------------- | ----------------------------------------------------- |
| **NIC**           | Nebari Infrastructure Core - this project             |
| **LGTM**          | Loki, Grafana, Tempo, Mimir - observability stack (planned; not yet deployed by NIC) |
| **CRD**           | Custom Resource Definition - Kubernetes API extension |
| **HTTPRoute**     | Kubernetes Gateway API resource for HTTP routing      |
| **OIDC**          | OpenID Connect - authentication protocol              |
| **OTLP**          | OpenTelemetry Protocol - telemetry data format        |
| **ArgoCD**        | GitOps continuous deployment tool                     |
| **cert-manager**  | Kubernetes certificate management                     |
| **Envoy Gateway** | Kubernetes Gateway API implementation                 |
| **Keycloak**      | Open-source identity and access management            |
| **NebariApp**     | CRD reconciled by the Nebari Operator (developed out-of-tree at `nebari-dev/nebari-operator`) |
| **InfraSettings** | Provider-shaped capability struct returned by `provider.InfraSettings(cfg)`; the seam that lets CLI / `pkg/argocd` avoid branching on provider name |

## 17.2 Decision Log

| Date | Decision | Rationale |
| ---- | -------- | --------- |
| 2025-01-30 | Clean break from old Nebari | Seven years of lessons; avoid legacy complexity |
| 2025-01-30 | OpenTofu for the AWS provider via `terraform-exec` | Battle-tested EKS module, broad ecosystem familiarity |
| 2025-01-30 | Deploy foundational software via ArgoCD | GitOps best practices, dependency management via sync waves |
| 2025-01-30 | Deploy the Nebari Operator (developed out-of-tree) for app integration | Automate auth/routing for `NebariApp` CRs; keep NIC focused on infrastructure |
| 2025-01-30 | Envoy Gateway for ingress | Future-proof, Kubernetes Gateway API |
| 2026-?? | `provider.InfraSettings` for provider-shaped capabilities | Avoid `switch` on provider name in CLI/library code; new providers don't require edits elsewhere |
| 2026-?? | Hetzner provider via `hetzner-k3s` binary (no tofu) | The `Provider` interface is the contract; each provider picks the right tool |
| 2026-04-15 | [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md): Out-of-tree provider plugins | Smaller core binary, supported path for private (e.g., ASCOT DNS) integrations |

The specific commit dates for the 2026 entries can be reconstructed from git history; the entries above are placeholders for the decisions themselves.

## 17.3 Success Criteria

**Current alpha-line success (today's bar):**

- ✅ AWS and Hetzner cluster providers functional
- ✅ Local Kind workflow via `make localkind-up`
- ✅ `existing` provider for adopting clusters NIC didn't provision
- ✅ Foundational stack syncing via ArgoCD: cert-manager, Envoy Gateway, Keycloak (+ postgresql), MetalLB (conditional), OpenTelemetry Collector, Nebari Operator, Nebari Landing Page
- ✅ NIC instrumented with OpenTelemetry (with documented exemptions; operation-granularity wrappers on `TerraformExecutor` are tracked as outstanding work)
- ✅ Unit tests + lint + race + coverage in CI

**v1.0 success (planned):**

- ⏳ GCP and Azure providers functional (or replaced by out-of-tree plugins per ADR-0004)
- ⏳ LGTM observability backend deployed by NIC
- ⏳ Documented upgrade paths between releases
- ⏳ End-to-end test coverage across providers
- ⏳ AWS cluster deploy under 20 minutes from a fresh account

**User success criteria:**

- ✅ One command to deploy: `nic deploy -f config.yaml`
- ✅ One CR per app to register with the platform: `NebariApp`
- ✅ Auth and routing wired automatically by the operator
- ⏳ Grafana dashboards immediately available (depends on LGTM)
- ⏳ End-to-end troubleshooting via traces/logs/metrics (depends on LGTM)

## 17.4 Risks and Mitigations

| Risk | Impact | Mitigation |
| ---- | ------ | ---------- |
| Cloud API changes | High | Pinned SDK versions; integration tests against LocalStack; monitor API deprecations |
| Kubernetes version skew | Medium | Test against N, N-1, N-2; document supported versions per provider |
| ArgoCD application failures | High | Sync waves enforce ordering; ArgoCD self-heal handles drift; manual `argocd app sync` as override |
| State corruption (AWS) | Critical | S3 versioning enabled; native lockfile-based locking; validation at parse time |
| Certificate expiration | Medium | cert-manager auto-renewal; alerts via OpenTelemetry Collector (backend pending) |
| Keycloak downtime | High | Configurable replica count; external Postgres backing store; backup/restore is roadmap |
| Operator bugs | Medium | Operator is out-of-tree at `nebari-dev/nebari-operator` with its own test surface; NIC pins a known-good version |
| Stuck S3 lockfile after Ctrl-C | Medium | Known issue [#63](https://github.com/nebari-dev/nebari-infrastructure-core/issues/63); `nic unlock` tracked at [#64](https://github.com/nebari-dev/nebari-infrastructure-core/issues/64) |

## 17.5 References

- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Envoy Gateway](https://gateway.envoyproxy.io/)
- [`nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator) - out-of-tree operator that reconciles `NebariApp` CRs
- `nebari-dev/eks-cluster/aws` v0.4.0 (OpenTofu Registry) - upstream Terraform module used by NIC's AWS provider; see `pkg/provider/aws/templates/main.tf`
- [`hetzner-k3s`](https://github.com/vitobotta/hetzner-k3s) - binary used by NIC's Hetzner provider
- [ADR-0004: Out-of-Tree Provider Plugin Architecture](../../adr/0004-out-of-tree-provider-plugins.md)

---

**End of Design Document**
