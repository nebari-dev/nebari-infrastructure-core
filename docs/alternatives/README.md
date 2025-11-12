# NIC Implementation Alternatives

This directory contains documentation for alternative implementation approaches to Nebari Infrastructure Core (NIC). The main NIC design uses **native cloud SDKs** (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go) for maximum control and performance. However, alternative approaches may be better suited for different teams and use cases.

## Available Alternatives

### 1. Native SDK Edition (Main/Default)

**Location:** Main docs (docs/architecture/, docs/implementation/)

**Approach:** Direct use of cloud provider SDKs with custom Go implementation.

**Key Characteristics:**
- Direct API calls to AWS, GCP, Azure via native SDKs
- Custom state management and resource discovery
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

| Criterion | Native SDK Edition | OpenTofu Edition |
|-----------|-------------------|------------------|
| **Performance** | ⭐⭐⭐⭐⭐ Fast (direct APIs) | ⭐⭐⭐ Slower (plan/apply overhead: 1-2min) |
| **Development Speed** | ⭐⭐⭐ Slower (write all code) | ⭐⭐⭐⭐⭐ Fast (reuse modules) |
| **Code Volume** | ~5000 lines provider code | ~500 lines + modules |
| **Team Skills** | Cloud SDK knowledge | Terraform knowledge (common) |
| **Debugging** | ⭐⭐⭐⭐⭐ Easy (Go traces) | ⭐⭐⭐ Harder (Terraform layer) |
| **Observability** | ⭐⭐⭐⭐⭐ Full OpenTelemetry | ⭐⭐⭐ Limited (stdout parsing) |
| **Dependencies** | Cloud SDKs only | + OpenTofu binary |
| **Deployment Size** | ~50MB single binary | ~150MB (binary + tofu) |
| **Community** | Limited | ⭐⭐⭐⭐⭐ Extensive (Terraform) |
| **State Management** | Custom format | ⭐⭐⭐⭐⭐ Standard Terraform |

### When to Choose Native SDK Edition

✅ **Choose this if:**
- Performance is critical (latency-sensitive operations)
- You need maximum control over API calls and retry logic
- You want better error messages directly from cloud APIs
- Your team has deep cloud SDK expertise
- You prefer single binary deployment with no external dependencies
- You need comprehensive observability (OpenTelemetry everywhere)
- You want custom state management and resource tagging strategies

❌ **Avoid this if:**
- Your team is primarily Terraform-focused
- You want to reuse existing Terraform modules
- Development velocity is more important than runtime performance
- You need Terraform tooling integration (Atlantis, Terraform Cloud, etc.)

### When to Choose OpenTofu Edition

✅ **Choose this if:**
- Your team is more familiar with Terraform than cloud SDKs
- You want faster development by reusing community modules
- You need support for all Terraform providers (beyond AWS/GCP/Azure)
- You prefer standard Terraform state format and backends
- You want existing Terraform tooling support
- Performance overhead of 1-2 minutes for plan/apply is acceptable
- You plan to contribute back to the Terraform module ecosystem

❌ **Avoid this if:**
- Performance is critical (every second counts)
- You want to avoid external binary dependencies
- You need fine-grained control over API retry logic
- You want maximum observability with OpenTelemetry
- You prefer simpler debugging with direct Go stack traces

### Detailed Comparison

For a comprehensive feature-by-feature comparison, see:
- **[Comparison: Native SDK vs OpenTofu](comparison-native-vs-opentofu.md)**

This document includes:
- Feature comparison table
- Detailed trade-off analysis
- Decision criteria and recommendations
- Future hybrid approach possibilities

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

## Future Alternatives

The alternatives framework allows for additional implementations:

### Potential Future Alternatives
- **Pulumi Edition**: Using Pulumi's Go SDK for infrastructure
- **CDK Edition**: AWS CDK / CDK for Terraform approach
- **YAML-Based Edition**: Custom YAML DSL with provider plugins
- **Hybrid Edition**: OpenTofu for infrastructure + Native SDKs for operations

We welcome proposals for new alternatives that serve different use cases or team preferences.

## Contributing

When proposing a new alternative:

1. **Create a new directory**: `docs/alternatives/[approach-name]/`
2. **Follow the documentation structure**: architecture/, implementation/, operations/, appendix/
3. **Provide comparison documentation**: Explain trade-offs vs existing alternatives
4. **Include decision criteria**: When to choose this approach
5. **Maintain consistency**: Same end-user experience across alternatives

See existing alternatives as templates for structure and content.

## Questions?

- **Issues**: [GitHub Issues](https://github.com/nebari-dev/nebari-infrastructure-core/issues)
- **Discussions**: [GitHub Discussions](https://github.com/nebari-dev/nebari-infrastructure-core/discussions)
- **Slack**: [Nebari Community](https://nebari.dev/community)

---

**Note**: All alternatives are equal in status - there is no "preferred" approach. Choose based on your team's skills, priorities, and requirements.
