# Introduction

### 1.1 Purpose

This document describes the architectural design for Nebari Infrastructure Core (NIC) v2.0, a clean-break redesign that applies seven years of lessons learned from developing and deploying Nebari. NIC is a standalone Go-based platform that provides opinionated Kubernetes deployments with a complete foundational software stack across AWS, GCP, Azure, and on-premises environments.

### 1.2 Core Design Principles

1. **Opinionated by Default**: Best practices from 7 years of production Nebari deployments
2. **Complete Platform**: Kubernetes + foundational software (auth, o11y, routing, GitOps)
3. **Declarative Infrastructure**: Declare desired state, NIC reconciles to match
4. **Native SDKs**: Use cloud provider SDKs directly, not Terraform
5. **Stateless Operation**: No state files - query cloud APIs for actual state every run
6. **Tag-Based Discovery**: All resources tagged for identification and ownership
7. **Multi-Cloud Consistency**: Common platform experience across all providers
8. **Observability-First**: OpenTelemetry instrumentation and LGTM stack built-in
9. **Application-Centric**: Nebari Operator automates app registration with auth, o11y, routing
10. **GitOps Native**: ArgoCD for all foundational software deployment

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
│ Kubernetes Cluster (Deployed by NIC)                       │
│ - Production-ready configuration                           │
│ - Multi-zone, highly available                            │
│ - Observability & security best practices                 │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Cloud Infrastructure (Provisioned by NIC)                  │
│ - VPC/Networking                                           │
│ - Managed Kubernetes (EKS/GKE/AKS/K3s)                    │
│ - Node pools & auto-scaling                               │
│ - Storage, security, IAM                                  │
└─────────────────────────────────────────────────────────────┘
```

### 1.4 Scope

**In Scope:**
- Cloud infrastructure provisioning (VPC, managed K8s, node pools, storage, IAM)
- Kubernetes cluster deployment (production-ready configuration)
- Foundational software deployment (Keycloak, LGTM, cert-manager, Envoy, ArgoCD)
- Nebari Kubernetes Operator (nebari-application CRD)
- Supported platforms: AWS (EKS), GCP (GKE), Azure (AKS), On-Prem (K3s)
- Configuration via declarative YAML
- Stateless operation via cloud API queries
- Resource tagging for discovery and ownership tracking
- OpenTelemetry instrumentation throughout
- Structured logging via slog

**Out of Scope:**
- Application deployment (handled by users via ArgoCD or kubectl)
- Legacy Nebari compatibility (clean break)
- Terraform integration (native SDKs only)
- Managed database services (users provision separately)
- CI/CD pipelines (beyond ArgoCD for foundational software)

### 1.5 Lessons Learned from 7 Years of Nebari

**What We're Keeping:**
- ✅ Opinionated platform approach (reduces decision fatigue)
- ✅ Multi-cloud support (AWS, GCP, Azure, Local)
- ✅ Declarative configuration (infrastructure as code)
- ✅ Authentication-first design (Keycloak integration)
- ✅ Observability focus (monitoring from day one)

**What We're Changing:**
- ❌ **Terraform complexity** → ✅ Native SDKs with Go
- ❌ **Staged deployment fragmentation** → ✅ Unified deployment
- ❌ **Manual app integration** → ✅ Operator-automated registration
- ❌ **Scattered observability** → ✅ Unified LGTM stack + OpenTelemetry
- ❌ **Ad-hoc ingress** → ✅ Envoy Gateway with consistent API
- ❌ **Implicit dependencies** → ✅ ArgoCD dependency graph

**Key Insights Applied:**

| Insight | Design Impact |
|---------|---------------|
| **Users want "batteries included"** | Foundational software deployed by default |
| **Auth integration is tedious** | Operator automates OAuth client creation |
| **Observability is an afterthought** | LGTM stack + OpenTelemetry built-in |
| **Certificate management is painful** | cert-manager + automated ingress TLS |
| **Terraform state issues cause outages** | Custom state with automatic recovery |
| **Multi-cloud drift is real** | Provider abstraction enforces consistency |
| **GitOps reduces deployment errors** | ArgoCD for all foundational components |

---
