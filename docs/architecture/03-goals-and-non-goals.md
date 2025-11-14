# Goals and Non-Goals

### 3.1 Primary Goals

**Phase 1 Goals (MVP):**
1. ✅ Deploy production-ready Kubernetes on AWS, GCP, Azure, Local
2. ✅ Deploy all foundational software via ArgoCD
3. ✅ Nebari Operator with basic nebari-application CRD support
4. ✅ Working auth (Keycloak), o11y (LGTM), routing (Envoy)
5. ✅ Stateless operation with tag-based resource discovery
6. ✅ OpenTelemetry instrumentation throughout NIC
7. ✅ Comprehensive documentation and examples

**Phase 2 Goals (Iteration):**
1. Advanced Keycloak integration (SAML, LDAP federation)
2. Custom Grafana dashboards for NIC-deployed clusters
3. Automated backup and restore for foundational software
4. Multi-cluster support (deploy multiple clusters)
5. Cost optimization features (spot instances, autoscaling)
6. Compliance profiles (HIPAA, SOC2, PCI-DSS)
7. **Git repository provisioning** (GitHub/GitLab) with auto-generated CI/CD workflows
8. **Software stack specification** - Deploy complete stacks (databases, caching, apps) alongside foundational software
^ I'm very curious to see what this will look like.  It seems like we'll have to create a generic way for applications to deploy databases, redis, and anything else they might need.
9. **Full-stack-in-one-repo** - Define platform + applications + config in single version-controlled repository
10. **Stack templates** - Pre-built configurations for common use cases (data science, ML platform, web apps)

**Future Goals:**
1. Service mesh integration (Istio/Linkerd)
2. Advanced security (OPA/Gatekeeper policies)
3. Edge deployment support
4. Hybrid cloud networking
5. AI/ML workload optimizations (GPU pools, model serving)

### 3.2 Explicit Non-Goals

**Not Doing:**
- ❌ Backward compatibility with old Nebari (clean break)
- ❌ Supporting Terraform-based deployments
- ❌ Managed database services (RDS/CloudSQL/etc.)
- ❌ Application deployment (beyond foundational software)
- ❌ Windows node pools (Linux only)
- ❌ Bare-metal Kubernetes (except K3s)
- ❌ Custom Kubernetes distributions (stick to EKS/GKE/AKS/K3s)
- ❌ Non-standard authentication (only Keycloak)

---
