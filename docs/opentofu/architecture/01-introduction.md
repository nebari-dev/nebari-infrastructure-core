# Introduction

### 1.1 Purpose

This document describes the architectural design for Nebari Infrastructure Core (NIC) v2.0 **OpenTofu Edition**, a clean-break redesign that applies seven years of lessons learned from developing and deploying Nebari. This variant uses **OpenTofu modules** (Terraform-compatible) with the **terraform-exec** Go library to provision infrastructure, providing a balance between Go-based orchestration and battle-tested Terraform modules.

**Key Difference from Native SDK Edition:**
- Uses OpenTofu/Terraform modules for infrastructure provisioning
- Leverages terraform-exec for programmatic control from Go
- Benefits from existing Terraform module ecosystem
- Uses Terraform state backend (no custom state format)

### 1.2 Core Design Principles

1. **Opinionated by Default**: Best practices from 7 years of production Nebari deployments
2. **Complete Platform**: Kubernetes + foundational software (Keycloak, LGTM, cert-manager, Envoy Gateway, ArgoCD)
3. **Declarative Infrastructure**: OpenTofu modules define desired state
4. **Terraform Ecosystem**: Leverage existing modules and provider ecosystem
5. **Multi-Cloud Consistency**: Common platform experience across all providers
6. **Observability-First**: OpenTelemetry instrumentation and LGTM stack built-in
7. **Application-Centric**: Nebari Operator automates app registration with auth, o11y, routing
8. **GitOps Native**: ArgoCD for all foundational software deployment

### 1.3 What NIC Provides

**Complete Platform Stack:**
```
┌─────────────────────────────────────────────────────────────┐
│ Application Layer (Managed by Nebari Operator)             │
│ - User applications registered via nebari-application CRD  │
│ - Auto-configured auth, o11y, routing                      │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Foundational Software (Deployed by ArgoCD)                 │
│ ├── Keycloak (Authentication & Authorization)              │
│ ├── LGTM Stack (Loki, Grafana, Tempo, Mimir)              │
│ ├── OpenTelemetry Collector (Metrics, Logs, Traces)       │
│ ├── cert-manager (TLS Certificate Management)             │
│ ├── Envoy Gateway (Ingress & API Gateway)                 │
│ └── ArgoCD (GitOps Continuous Deployment)                 │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Kubernetes Cluster (Deployed by OpenTofu Modules)         │
│ - Production-ready configuration                           │
│ - Multi-zone, highly available                            │
│ - Observability & security best practices                 │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Cloud Infrastructure (Provisioned by OpenTofu Modules)     │
│ - VPC/Networking                                           │
│ - Managed Kubernetes (EKS/GKE/AKS/K3s)                    │
│ - Node pools & auto-scaling                               │
│ - Storage, security, IAM                                  │
└─────────────────────────────────────────────────────────────┘
```

### 1.4 Scope

**In Scope:**
- Cloud infrastructure provisioning via OpenTofu modules
- Kubernetes cluster deployment (production-ready configuration)
- Foundational software deployment (Keycloak, LGTM, cert-manager, Envoy, ArgoCD)
- Nebari Kubernetes Operator (nebari-application CRD)
- Supported platforms: AWS (EKS), GCP (GKE), Azure (AKS), On-Prem (K3s)
- Configuration via declarative YAML
- Terraform state backend management
- OpenTelemetry instrumentation throughout
- Structured logging via slog

**Out of Scope:**
- Application deployment (handled by users via ArgoCD or kubectl)
- Legacy Nebari compatibility (clean break)
- Custom state format (uses Terraform state)
- Managed database services (users provision separately)
- CI/CD pipelines (beyond ArgoCD for foundational software)

### 1.5 Why OpenTofu + terraform-exec?

**Advantages:**
- ✅ **Proven Modules**: Leverage thousands of existing Terraform modules
- ✅ **Provider Ecosystem**: All Terraform providers work with OpenTofu
- ✅ **Community Support**: Large community, battle-tested patterns
- ✅ **Terraform State**: Standard state format, tooling, backends
- ✅ **Less Code**: Reuse existing modules instead of writing SDK calls
- ✅ **Gradual Migration**: Easier for teams familiar with Terraform
- ✅ **Module Composition**: Combine community modules with custom logic

**Trade-offs:**
- ⚠️ **External Dependency**: Requires OpenTofu/Terraform binary
- ⚠️ **Performance**: Slower than native SDKs (plan/apply overhead)
- ⚠️ **Error Messages**: Less direct than native SDK errors
- ⚠️ **Debugging**: Additional layer between Go code and cloud APIs

---
