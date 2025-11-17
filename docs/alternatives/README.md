# NIC Implementation Alternatives

This directory contains documentation for alternative implementation approaches to Nebari Infrastructure Core (NIC). The main NIC design uses **native cloud SDKs** (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go) for maximum control and performance. However, alternative approaches may be better suited for different teams and use cases.

## Alternative Design Approach

### 1. Native SDK Edition (Main/Default)

**Location:** Main docs (docs/architecture/, docs/implementation/)

**Approach:** Direct use of cloud provider SDKs with custom Go implementation.

**Key Characteristics:**

- Direct API calls to AWS, GCP, Azure via native SDKs
- Maximum performance and control
- Full OpenTelemetry instrumentation
- Single binary deployment

**Best For:**

- Performance-critical deployments
- Teams with deep cloud SDK expertise
- Need for fine-grained control over infrastructure operations
- Advanced observability requirements

**Documentation:** See [main docs/README.md](../README.md)

---

### 2. OpenTofu Edition

**Location:** [alternatives/opentofu/](opentofu/)

**Approach:** OpenTofu/Terraform modules orchestrated via terraform-exec Go library.

**Key Characteristics:**

- Leverages existing Terraform module ecosystem
- Standard Terraform state management
- Faster development velocity (reuse vs rebuild)
- Familiar to teams with Terraform experience
- Requires OpenTofu/Terraform binary

**Best For:**

- Teams already using Terraform/OpenTofu
- Faster development and iteration cycles
- Leveraging community Terraform modules
- Standard Terraform tooling integration (Atlantis, etc.)

**Documentation:** Start with [OpenTofu Introduction](opentofu/architecture/01-introduction.md)

---

## Decision Guide

### Quick Comparison Matrix

| Criterion             | Native SDK Edition             | OpenTofu Edition                            |
| --------------------- | ------------------------------ | ------------------------------------------- |
| **Performance**       | ⭐⭐⭐⭐⭐ Fast (direct APIs)  | ⭐⭐⭐ Slower (plan/apply overhead: 1-2min) |
| **Development Speed** | ⭐⭐⭐ Slower (write all code) | ⭐⭐⭐⭐⭐ Fast (reuse modules)             |
| **Code Volume**       | ~5000 lines provider code      | ~500 lines + modules                        |
| **Team Skills**       | Cloud SDK knowledge            | Terraform knowledge (common)                |
| **Debugging**         | ⭐⭐⭐⭐⭐ Easy (Go traces)    | ⭐⭐⭐ Harder (Terraform layer)             |
| **Observability**     | ⭐⭐⭐⭐⭐ Full OpenTelemetry  | ⭐⭐⭐ Limited (stdout parsing)             |
| **Dependencies**      | Cloud SDKs only                | + OpenTofu binary                           |
| **Deployment Size**   | ~50MB single binary            | ~150MB (binary + tofu)                      |
| **Community**         | Limited                        | ⭐⭐⭐⭐⭐ Extensive (Terraform)            |
| **State Management**  | Stateless Design               | ⭐⭐⭐⭐⭐ Standard Terraform               |

## Architecture Overview

Both editions provide the same complete platform stack:

```
Application Layer (Managed by Nebari Operator)
  → nebari-application CRD for app registration
  → Auto-configured auth, o11y, routing

Foundational Software (Deployed by ArgoCD)
  → Keycloak (Auth), LGTM Stack (Observability)
  → OpenTelemetry Collector, cert-manager, Envoy Gateway

Kubernetes Cluster (Deployed by NIC)
  → Production-ready, multi-zone, highly available

Cloud Infrastructure (Provisioned by NIC)
  → VPC, managed K8s (EKS/GKE/AKS/K3s), node pools, storage
```

**The difference is HOW infrastructure is provisioned:**

- **Native SDK:** Direct cloud SDK calls from Go
- **OpenTofu:** Terraform modules orchestrated via terraform-exec

Both deliver the same end result: a complete, production-ready Nebari platform.

### Potential Other Alternatives

- **Pulumi Edition**: Using Pulumi's Go SDK for infrastructure
- **Hybrid Edition**: OpenTofu for infrastructure + Native SDKs for operations

---
