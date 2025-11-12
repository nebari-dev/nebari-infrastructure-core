# System Overview

### 2.1 High-Level Architecture

**NIC Deployment Flow:**

```
┌─────────────────────────────────────────────────────────────┐
│ 1. User defines nebari-config.yaml                         │
│    - Cloud provider (aws/gcp/azure/local)                  │
│    - Cluster size and node pools                           │
│    - Foundational software configuration                   │
│    - Domain and TLS settings                               │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. NIC CLI parses config and plans deployment              │
│    $ nic deploy -f nebari-config.yaml                      │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. Cloud Infrastructure Provisioning (Native SDKs)         │
│    ├── VPC/Network (subnets, security groups, NAT)        │
│    ├── Managed Kubernetes (EKS/GKE/AKS/K3s)               │
│    ├── Node Pools (general, compute, gpu)                 │
│    ├── Storage (EFS/Filestore/Azure Files)                │
│    └── IAM (service accounts, roles, policies)            │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. Kubernetes Bootstrap                                    │
│    ├── Namespaces (nebari-system, monitoring, ingress)    │
│    ├── Storage Classes (persistent volumes)               │
│    ├── RBAC (cluster roles, service accounts)             │
│    ├── Network Policies (namespace isolation)             │
│    └── Priority Classes (workload prioritization)         │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 5. ArgoCD Deployment (Helm)                                │
│    - Installed in nebari-system namespace                  │
│    - Configured with foundational-software repo            │
│    - Sets up app-of-apps pattern                          │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 6. Foundational Software (ArgoCD Applications)             │
│    ├── cert-manager (TLS automation)                       │
│    ├── Envoy Gateway (ingress controller)                 │
│    ├── OpenTelemetry Collector (telemetry pipeline)       │
│    ├── Mimir (metrics storage)                            │
│    ├── Loki (log aggregation)                             │
│    ├── Tempo (trace storage)                              │
│    ├── Grafana (visualization)                            │
│    └── Keycloak (authentication)                          │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 7. Nebari Operator Deployment                              │
│    - Installed via ArgoCD                                  │
│    - Watches nebari-application CRD                        │
│    - Registers apps with Keycloak, Envoy, o11y            │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 8. Platform Ready                                          │
│    ✅ Kubernetes cluster running                           │
│    ✅ Foundational software operational                    │
│    ✅ Auth, o11y, routing configured                       │
│    ✅ Users can deploy applications                        │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Component Breakdown

**NIC CLI (`cmd/nic`):**
- Command-line interface for platform management
- Commands: `deploy`, `destroy`, `status`, `validate`, `plan`, `upgrade`
- Explicit provider registration (no blank imports)
- OpenTelemetry tracing for all operations
- Structured logging via slog

**Provider Implementations (`pkg/provider`):**
- `aws/` - EKS, VPC, EC2, EFS via aws-sdk-go-v2
- `gcp/` - GKE, VPC, Filestore via google-cloud-go
- `azure/` - AKS, VNet, Azure Files via azure-sdk-for-go
- `local/` - K3s, local storage via k3s API

**State Management (`pkg/state`):**
- Custom JSON state format (not Terraform state)
- Backends: S3, GCS, Azure Blob, local filesystem
- Locking: DynamoDB, Cloud Storage metadata, Blob lease
- Drift detection and repair
- Automatic state migration

**Kubernetes Management (`pkg/kubernetes`):**
- Bootstrap resources (namespaces, RBAC, storage classes)
- ArgoCD installation via Helm
- Foundational software ArgoCD applications
- Uses client-go for all Kubernetes operations

**Foundational Software (`pkg/foundational`):**
- ArgoCD application definitions
- Configuration templates for each component
- Health checks and readiness gates
- Dependency ordering (cert-manager first, then Envoy, etc.)

**Nebari Operator (`pkg/operator`):**
- Kubernetes operator built with controller-runtime
- Reconciles nebari-application CRD
- Integrates with Keycloak, Envoy Gateway, Grafana
- Automatic OAuth client creation, route configuration, dashboard provisioning

### 2.3 Why This Architecture?

| Design Choice | Rationale |
|---------------|-----------|
| **Native SDKs vs Terraform** | Direct control, better error messages, faster execution, no HCL layer |
| **Custom State vs Terraform State** | Simpler format, no Terraform dependency, optimized for our use case |
| **ArgoCD for Foundational Software** | GitOps best practices, dependency management, declarative updates |
| **Operator for App Registration** | Automates repetitive tasks, reduces human error, consistent integration |
| **LGTM Stack vs Custom** | Industry-standard, proven at scale, unified Grafana Labs ecosystem |
| **Envoy Gateway vs Others** | Kubernetes Gateway API, future-proof, advanced routing features |
| **Helm for ArgoCD Only** | Minimize Helm usage, ArgoCD handles rest via manifests |
| **OpenTelemetry Built-In** | Observability from day one, vendor-neutral, industry standard |

---
