# Introduction

### 1.1 Purpose

This document describes the architectural design for Nebari Infrastructure Core (NIC), a clean-break redesign that applies seven years of lessons learned from developing and deploying Nebari. NIC is a standalone command-line tool that provisions Kubernetes clusters and bootstraps an opinionated foundational software stack on top of them.

### 1.2 Core Design Principles

1. **Opinionated by Default**: Best practices from seven years of production Nebari deployments
2. **Complete Platform**: Kubernetes plus foundational software (auth, routing, GitOps, certs)
3. **Provider Abstraction**: Each cluster provider chooses the right backing tool for its environment - OpenTofu for AWS, native CLI/SDK for Hetzner, Kind for local dev. The `provider.Provider` interface is the contract, not a single IaC tool.
4. **Declarative Infrastructure**: Declare desired state in `nebari-config.yaml`; the configured provider reconciles to match
5. **GitOps Native**: ArgoCD is the deployment mechanism for all foundational software
6. **Standard State Management (where applicable)**: AWS uses Terraform state in S3 with native lockfile-based locking; non-tofu providers manage state in tool-specific ways
7. **Observability-First**: OpenTelemetry instrumentation in library code, structured `slog` logging in the CLI layer
8. **Application-Centric**: The Nebari Operator (deployed by NIC, developed out-of-tree) automates app registration with auth and routing

### 1.3 What NIC Provides

**Layered Platform Stack:**

```
┌─────────────────────────────────────────────────────────────┐
│ Application Layer (Managed by Nebari Operator)              │
│ - User applications registered via NebariApp CRD            │
│ - Auto-configured auth and routing                          │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Foundational Software (Deployed by ArgoCD)                  │
│ ├── cert-manager + cluster-issuers (TLS automation)         │
│ ├── Envoy Gateway + gateway-config + httproutes (ingress)   │
│ ├── Keycloak + postgresql (authentication)                  │
│ ├── MetalLB + metallb-config (LB, local/bare-metal only)    │
│ ├── OpenTelemetry Collector (telemetry pipeline)            │
│ ├── Nebari Operator (NebariApp reconciler, out-of-tree)     │
│ └── Nebari Landing Page (service catalog UI)                │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Kubernetes Cluster (Provisioned by NIC)                     │
│ - Provider-specific configuration                           │
│ - Multi-AZ on cloud providers; single-node on local         │
│ - StorageClass and LB integration per provider              │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ Cluster Provider (per-provider backing tool)                │
│ - AWS: OpenTofu (EKS via nebari-dev/eks-cluster module)     │
│ - Hetzner: hetzner-k3s binary                               │
│ - Local: Kind (driven by `make localkind-up`)               │
│ - Existing: no-op adapter for pre-provisioned clusters      │
│ - GCP, Azure: stubs, not yet implemented                    │
└─────────────────────────────────────────────────────────────┘
```

A full LGTM (Loki / Grafana / Tempo / Mimir) observability backend is **not** currently deployed by NIC. Only the OpenTelemetry Collector is shipped today; building out the backend is on the roadmap (see [`13-milestones.md`](../operations/13-milestones.md)).

### 1.4 Scope

**In Scope:**

- Cloud infrastructure provisioning, by the configured cluster provider's chosen tool
- Kubernetes cluster deployment (production-ready configuration on cloud providers)
- Foundational software deployment via ArgoCD from a generated GitOps repository
- A DNS provider abstraction (currently with a Cloudflare implementation)
- Configuration via declarative YAML (`nebari-config.yaml`)
- OpenTelemetry instrumentation in library code
- Structured logging via `slog` at the CLI layer
- The Nebari Operator is deployed by NIC but developed in a separate repository (`github.com/nebari-dev/nebari-operator`)

**Out of Scope:**

- Application deployment beyond foundational software (handled by users via ArgoCD or kubectl)
- Legacy Nebari compatibility (clean break)
- Managed database services (users provision separately)
- General-purpose CI/CD pipelines (beyond ArgoCD for foundational software)
- Implementing the Nebari Operator itself (lives out-of-tree)

### 1.5 Lessons Learned from Seven Years of Nebari

**What We're Keeping:**

- Opinionated platform approach (reduces decision fatigue)
- Multi-cluster-provider support (AWS, Hetzner, local Kind today; GCP and Azure planned)
- Declarative configuration (infrastructure as code)
- Authentication-first design (Keycloak integration)
- Observability focus (telemetry instrumentation from day one)

**What We're Changing:**

| Old Nebari | NIC |
|------------|-----|
| Custom Terraform wrappers | terraform-exec orchestration, per-provider tool choice |
| Staged deployment fragmentation | Unified deployment via `nic deploy` |
| Manual app integration | Operator-automated `NebariApp` registration |
| Ad-hoc ingress | Envoy Gateway with Kubernetes Gateway API |
| Implicit dependencies | ArgoCD app-of-apps with sync waves |

**Key Insights Applied:**

| Insight | Design Impact |
| ------- | ------------- |
| Users want "batteries included" | Foundational software deployed by default |
| Auth integration is tedious | Operator automates OAuth client creation |
| Observability is an afterthought | OpenTelemetry Collector deployed by default (backend pending) |
| Certificate management is painful | cert-manager plus automated route TLS |
| Battle-tested modules beat hand-rolled IaC | AWS uses the upstream `nebari-dev/eks-cluster` module |
| Multi-cloud drift is real | The `Provider` interface enforces the consistent contract |
| GitOps reduces deployment errors | ArgoCD for all foundational components |

For the rationale behind per-provider tool choice and the planned out-of-tree plugin direction, see [ADR-0004: Out-of-Tree Provider Plugin Architecture](../../adr/0004-out-of-tree-provider-plugins.md).

---
