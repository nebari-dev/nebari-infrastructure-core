# Goals and Non-Goals

### 3.1 Primary Goals

**v1.0 Goals (MVP - 6 months):**
1. ✅ Deploy production-ready Kubernetes on AWS, GCP, Azure, Local
2. ✅ Deploy all foundational software via ArgoCD
3. ✅ Nebari Operator with basic nebari-application CRD support
4. ✅ Working auth (Keycloak), o11y (LGTM), routing (Envoy)
5. ✅ Custom state management with S3/GCS/Azure Blob backends
6. ✅ OpenTelemetry instrumentation throughout NIC
7. ✅ Comprehensive documentation and examples

**v1.x Goals (Iteration - 6-12 months):**
1. Advanced Keycloak integration (SAML, LDAP federation)
2. Custom Grafana dashboards for NIC-deployed clusters
3. Automated backup and restore for foundational software
4. Multi-cluster support (deploy multiple clusters)
5. Cost optimization features (spot instances, autoscaling)
6. Compliance profiles (HIPAA, SOC2, PCI-DSS)

**v2.0+ Goals (Future):**
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
