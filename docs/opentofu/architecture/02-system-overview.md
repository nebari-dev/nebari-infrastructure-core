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
│ 2. NIC CLI translates config to Terraform variables        │
│    $ nic deploy -f nebari-config.yaml                      │
│    - Generates .tfvars files from nebari-config.yaml       │
│    - Selects appropriate provider module                   │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. OpenTofu Module Execution (via terraform-exec)         │
│    - NIC calls: tofu.Init(), tofu.Plan(), tofu.Apply()    │
│    - Modules provision:                                    │
│      ├── VPC/Network (subnets, security groups, NAT)      │
│      ├── Managed Kubernetes (EKS/GKE/AKS/K3s)             │
│      ├── Node Pools (general, compute, gpu)               │
│      ├── Storage (EFS/Filestore/Azure Files)              │
│      └── IAM (service accounts, roles, policies)          │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. Kubernetes Bootstrap (via Kubernetes provider in Tofu) │
│    - Terraform kubernetes provider creates:                │
│      ├── Namespaces (nebari-system, monitoring, ingress)  │
│      ├── Storage Classes (persistent volumes)             │
│      ├── RBAC (cluster roles, service accounts)           │
│      ├── Network Policies (namespace isolation)           │
│      └── Priority Classes (workload prioritization)       │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 5. ArgoCD Deployment (Helm provider in Tofu)              │
│    - Installed in nebari-system namespace                  │
│    - Configured with foundational-software repo            │
│    - Sets up app-of-apps pattern                          │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 6. Foundational Software (ArgoCD Applications via Tofu)   │
│    - Terraform kubernetes_manifest resources create:       │
│      ├── cert-manager ArgoCD Application                  │
│      ├── Envoy Gateway ArgoCD Application                 │
│      ├── OpenTelemetry Collector ArgoCD Application       │
│      ├── Mimir, Loki, Tempo ArgoCD Applications           │
│      ├── Grafana ArgoCD Application                       │
│      ├── Keycloak ArgoCD Application                      │
│      └── Nebari Operator ArgoCD Application               │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 7. Wait for Foundational Software Ready (NIC monitors)    │
│    - NIC uses Kubernetes client-go to wait for readiness  │
│    - Polls ArgoCD ApplicationStatus                       │
│    - Verifies all components healthy                      │
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
- Translates nebari-config.yaml → Terraform variables
- Orchestrates terraform-exec calls
- OpenTelemetry tracing for all operations
- Structured logging via slog

**OpenTofu Modules (`terraform/modules/`):**
- `aws/` - EKS, VPC, EFS modules
- `gcp/` - GKE, VPC, Filestore modules
- `azure/` - AKS, VNet, Azure Files modules
- `local/` - K3s setup (shell provisioners or existing K3s modules)
- `kubernetes/` - Shared Kubernetes resources (namespaces, RBAC, storage classes)
- `argocd/` - ArgoCD installation via Helm provider
- `foundational-apps/` - ArgoCD Application manifests via kubernetes provider

**Terraform-Exec Wrapper (`pkg/tofu`):**
- Go wrapper around terraform-exec library
- Provides simplified API for NIC operations
- Handles working directory setup
- Manages Terraform state backend configuration
- Captures and processes Terraform output
- Error handling and retry logic

**State Management (`pkg/state`):**
- Uses Terraform state backends (S3, GCS, Azure Blob, local)
- Wrapper for state backend configuration
- State locking via Terraform native mechanisms
- Drift detection via `terraform plan`
- No custom state format needed

**Kubernetes Management (`pkg/kubernetes`):**
- Wait for ArgoCD application readiness
- Health checks for foundational software
- Uses client-go for monitoring (not provisioning)
- Kubeconfig generation from Terraform outputs

**Nebari Operator (`pkg/operator`):**
- Kubernetes operator built with controller-runtime
- Deployed via Terraform kubernetes_manifest
- Reconciles nebari-application CRD
- Integrates with Keycloak, Envoy Gateway, Grafana

### 2.3 Why This Architecture?

| Design Choice | Rationale |
|---------------|-----------|
| **OpenTofu vs Terraform** | Open-source, community-driven, no licensing concerns, API-compatible |
| **terraform-exec** | Official Go library, programmatic control, clean API |
| **Terraform State** | Standard format, mature backends, existing tooling |
| **Module Composition** | Reuse community modules, faster development, proven patterns |
| **ArgoCD for Foundational Software** | GitOps best practices, dependency management, declarative updates |
| **Operator for App Registration** | Automates repetitive tasks, reduces human error, consistent integration |
| **LGTM Stack** | Industry-standard, proven at scale, unified Grafana Labs ecosystem |
| **Envoy Gateway** | Kubernetes Gateway API, future-proof, advanced routing features |

---
