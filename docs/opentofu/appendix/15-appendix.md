# Appendix

### 15.1 Glossary

| Term | Definition |
|------|------------|
| **NIC** | Nebari Infrastructure Core - this project |
| **OpenTofu** | Open-source Terraform fork, API-compatible |
| **terraform-exec** | Official Go library for programmatic Terraform execution |
| **LGTM** | Loki, Grafana, Tempo, Mimir - observability stack |
| **CRD** | Custom Resource Definition - Kubernetes API extension |
| **HTTPRoute** | Kubernetes Gateway API resource for HTTP routing |
| **OIDC** | OpenID Connect - authentication protocol |
| **OTLP** | OpenTelemetry Protocol - telemetry data format |
| **Terratest** | Go library for testing Terraform modules |

### 15.2 Decision Log

| Date | Decision | Rationale |
|------|----------|-----------|
| 2025-01-30 | Use OpenTofu + terraform-exec | Faster development, community modules, team familiarity |
| 2025-01-30 | Unified deployment (single Terraform apply) | Simpler state, faster deployment, Terraform handles dependencies |
| 2025-01-30 | Use Terraform state (not custom format) | Standard format, existing tooling, proven at scale |
| 2025-01-30 | Deploy foundational software via Terraform | Infrastructure as Code for entire stack, atomic deployment |
| 2025-01-30 | Build Nebari Operator (same as Native SDK) | Provider-agnostic, automateauth/o11y/routing |

### 15.3 Success Criteria

**v1.0 Success Criteria:**
1. ✅ Deploy production Kubernetes on AWS, GCP, Azure, Local using OpenTofu
2. ✅ All infrastructure managed via Terraform modules
3. ✅ All 9 foundational components deploy via ArgoCD (triggered by Terraform)
4. ✅ Nebari Operator automates app integration
5. ✅ NIC fully instrumented with OpenTelemetry
6. ✅ Documentation complete (user guides, module reference)
7. ✅ >80% unit test coverage
8. ✅ All Terraform modules tested
9. ✅ Performance: AWS cluster deployment <25 minutes

**User Success Criteria:**
- ✅ User can deploy platform with one command: `nic deploy`
- ✅ User can register app with one CRD: `NebariApplication`
- ✅ User gets auth, o11y, routing automatically
- ✅ User can use existing Terraform knowledge
- ✅ User can inspect Terraform state with standard tools

### 15.4 Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **Terraform API changes** | High | Pin Terraform version, test upgrades thoroughly |
| **Module compatibility issues** | Medium | Vendor modules, test module updates in dev |
| **terraform-exec library changes** | Medium | Pin library version, monitor upstream changes |
| **Performance slower than expected** | Low | Profile Terraform execution, optimize modules, accept trade-off |
| **Community module bugs** | Medium | Fork modules if needed, contribute fixes upstream |
| **State corruption** | Critical | Terraform handles it well, backups, state versioning |
| **Terraform binary not found** | Low | Check binary in PATH during init, clear error message |

### 15.5 References

- [OpenTofu Documentation](https://opentofu.org/docs/)
- [terraform-exec GitHub](https://github.com/hashicorp/terraform-exec)
- [Terraform Registry](https://registry.terraform.io/)
- [Terratest](https://terratest.gruntwork.io/)
- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [Keycloak Documentation](https://www.keycloak.org/documentation)
- [Grafana LGTM Stack](https://grafana.com/oss/)

---

**End of Design Document**
