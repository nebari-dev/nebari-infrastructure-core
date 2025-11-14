# Appendix

### 14.1 Glossary

| Term              | Definition                                            |
| ----------------- | ----------------------------------------------------- |
| **NIC**           | Nebari Infrastructure Core - this project             |
| **LGTM**          | Loki, Grafana, Tempo, Mimir - observability stack     |
| **CRD**           | Custom Resource Definition - Kubernetes API extension |
| **HTTPRoute**     | Kubernetes Gateway API resource for HTTP routing      |
| **OIDC**          | OpenID Connect - authentication protocol              |
| **OTLP**          | OpenTelemetry Protocol - telemetry data format        |
| **ArgoCD**        | GitOps continuous deployment tool                     |
| **cert-manager**  | Kubernetes certificate management                     |
| **Envoy Gateway** | Modern ingress controller using Gateway API           |
| **Keycloak**      | Open-source identity and access management            |

### 14.2 Decision Log

| Date       | Decision                                  | Rationale                                    |
| ---------- | ----------------------------------------- | -------------------------------------------- |
| 2025-01-30 | Clean break from old Nebari               | 7 years of lessons, avoid legacy complexity  |
| 2025-01-30 | Use native SDKs instead of Terraform      | Better control, errors, performance          |
| 2025-01-30 | Deploy foundational software via ArgoCD   | GitOps best practices, dependency management |
| 2025-01-30 | Build Nebari Operator for app integration | Automate repetitive auth/o11y/routing tasks  |
| 2025-01-30 | Use LGTM stack for observability          | Industry standard, proven at scale           |
| 2025-01-30 | Use Envoy Gateway for ingress             | Future-proof, Gateway API, advanced features |

### 14.3 Success Criteria

**v1.0 Success Criteria:**

1. ✅ Deploy production Kubernetes on AWS, GCP, Azure, Local
2. ✅ All 9 foundational components deploy via ArgoCD
3. ✅ Nebari Operator automates app integration (auth, o11y, routing)
4. ✅ NIC fully instrumented with OpenTelemetry
5. ✅ Documentation complete (user guides, API reference)
6. ✅ All provider tests passing
7. ✅ Performance: AWS cluster deployment <20 minutes

**User Success Criteria:**

- ✅ User can deploy platform with one command: `nic deploy`
- ✅ User can register app with one CRD: `NebariApplication`
- ✅ User gets auth, o11y, routing automatically (no manual config)
- ✅ User can access Grafana dashboards immediately
- ✅ User can troubleshoot via traces/logs/metrics

### 14.4 Risks and Mitigations

| Risk                            | Impact   | Mitigation                                                                       |
| ------------------------------- | -------- | -------------------------------------------------------------------------------- |
| **Cloud API changes**           | High     | Pin SDK versions, comprehensive integration tests, monitor API deprecations      |
| **Kubernetes version skew**     | Medium   | Test against multiple K8s versions (N, N-1, N-2), document supported versions    |
| **ArgoCD application failures** | High     | Health checks, retry logic, rollback capability, manual override option          |
| **State corruption**            | Critical | Atomic writes, backups before writes, state versioning, validation before save   |
| **Certificate expiration**      | Medium   | cert-manager auto-renewal, monitoring alerts, runbook for manual renewal         |
| **Keycloak downtime**           | High     | HA deployment (2+ replicas), external database, backup/restore procedures        |
| **Operator bugs**               | Medium   | Thorough testing, dry-run mode, status reporting, manual CRD delete escape hatch |

### 14.5 References

- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [Grafana LGTM Stack](https://grafana.com/oss/)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [Envoy Gateway](https://gateway.envoyproxy.io/)
- [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)

---

**End of Design Document**
