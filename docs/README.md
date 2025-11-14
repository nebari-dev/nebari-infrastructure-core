# Nebari Infrastructure Core (NIC) Documentation

**Version:** 2.0.0
**Status:** Design Phase

## Overview

Nebari Infrastructure Core (NIC) is a next-generation, opinionated Kubernetes deployment tool that provides a complete, production-ready platform across AWS, GCP, Azure, and On-Premises environments. NIC provisions cloud infrastructure and deploys a comprehensive foundational software stack including authentication, observability, certificate management, ingress, and GitOps continuous deployment.

**Key Highlights:**

- **Stateless Architecture**: No state files - queries cloud APIs for actual state on every run
- **Tag-Based Discovery**: All resources tagged for identification and ownership tracking
- **Native Cloud SDKs**: Direct use of aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go
- **Complete Observability**: LGTM stack (Loki, Grafana, Tempo, Mimir) with OpenTelemetry
- **Integrated Authentication**: Keycloak with OIDC/SAML support
- **GitOps Ready**: ArgoCD for continuous deployment of applications
- **Kubernetes Operator**: Custom nebari-application CRD for seamless app registration

## Documentation Structure

This documentation is organized into four main categories:

### Architecture

Core architectural decisions, principles, and system design.

1. **[Introduction](architecture/01-introduction.md)**
   Project vision, core principles, and stateless operation philosophy

2. **[System Overview](architecture/02-system-overview.md)**
   High-level architecture, component relationships, and deployment flow

3. **[Goals and Non-Goals](architecture/03-goals-and-non-goals.md)**
   Explicit scope boundaries and design constraints

4. **[Key Architectural Decisions](architecture/04-key-decisions.md)**
   Critical technical choices and their rationale

5. **[Stateless Operation & Resource Discovery](architecture/05-stateless-operation.md)**
   Tag-based resource discovery, drift detection, and stateless reconciliation

### Implementation

Detailed implementation specifications and technical designs.

6. **[Declarative Infrastructure with Native SDKs](implementation/06-declarative-infrastructure.md)**
   Infrastructure as code using cloud provider SDKs

7. **[Configuration Design](implementation/07-configuration-design.md)**
   config.yaml structure and validation

8. **[Cloud Provider Architecture](implementation/08-cloud-provider-architecture.md)**
   Multi-cloud provider abstraction and implementation patterns

9. **[DNS Provider Architecture](implementation/08-dns-provider-architecture.md)**
   Multi-cloud provider abstraction and implementation patterns

10. **[Foundational Software Stack](implementation/09-foundational-software.md)**
    Keycloak, LGTM observability, cert-manager, Envoy Gateway, ArgoCD deployment

11. **[Nebari Kubernetes Operator](implementation/10-nebari-operator.md)**
    Custom controller for nebari-application CRD and app lifecycle management

### Operations

Testing, deployment, and operational procedures.

12. **[Testing Strategy](operations/11-testing-strategy.md)**
    Unit, integration, provider, and end-to-end testing approaches

13. **[Milestones](operations/12-milestones.md)**
    Development roadmap and release planning

### Appendix

Additional resources and reference materials.

14. **[Open Questions](appendix/13-open-questions.md)**
    Unresolved design decisions and areas needing further investigation

15. **[Future Enhancements](appendix/15-future-enhancements.md)**
    Detailed specifications for v1.x features: Git automation, software stack specification, full-stack-in-one-repo

16. **[Configuration File Reference](appendix/16-configuration-reference.md)**
    Complete schema and field descriptions for config.yaml

17. **[Appendix](appendix/17-appendix.md)**
    Glossary, references, and supplementary information

### Alternatives

Alternative implementation approaches for different team needs and priorities.

**[Alternatives Overview](alternatives/README.md)** - Start here to understand available alternatives and choose the right approach for your team.

#### Available Implementations

**Native SDK Edition (Default)** - This documentation

- Direct cloud SDK usage (aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go)
- Maximum performance and control
- Custom state management
- Full OpenTelemetry instrumentation

**[OpenTofu Edition](alternatives/opentofu/architecture/01-introduction.md)**

- OpenTofu/Terraform modules via terraform-exec
- Faster development (reuse existing modules)
- Standard Terraform state and tooling
- Familiar to Terraform-experienced teams

**[Comparison: Native SDK vs OpenTofu](alternatives/comparison-native-vs-opentofu.md)**

- Feature-by-feature comparison
- Decision criteria and trade-offs
- When to choose each approach

## Quick Navigation by Topic

### For New Contributors

Start here to understand the project:

- [Introduction](architecture/01-introduction.md)
- [System Overview](architecture/02-system-overview.md)
- [Goals and Non-Goals](architecture/03-goals-and-non-goals.md)

### For Infrastructure Engineers

Deep dive into cloud infrastructure:

- [Declarative Infrastructure with Native SDKs](implementation/05-declarative-infrastructure.md)
- [Provider Architecture](implementation/08-provider-architecture.md)
- [Stateless Operation & Resource Discovery](architecture/06-stateless-operation.md)

### For Platform Engineers

Understand the foundational software stack:

- [Foundational Software Stack](implementation/09-foundational-software.md)
- [Nebari Kubernetes Operator](implementation/10-nebari-operator.md)
- [Configuration Design](implementation/07-configuration-design.md)

### For Architects

Review key technical decisions and implementation alternatives:

- [Key Architectural Decisions](architecture/04-key-decisions.md)
- [Stateless Operation & Resource Discovery](architecture/06-stateless-operation.md)
- [Alternatives Overview](alternatives/README.md) - Compare Native SDK vs OpenTofu approaches
- [Comparison: Native SDK vs OpenTofu](alternatives/comparison-native-vs-opentofu.md)
- [OpenTofu Edition Documentation](alternatives/opentofu/architecture/01-introduction.md)

### For QA Engineers

Testing and validation:

- [Testing Strategy](operations/11-testing-strategy.md)
- [Timeline and Milestones](operations/12-timeline-milestones.md)

## Core Technologies

### Infrastructure Management

- **Go**: Primary implementation language
- **Native Cloud SDKs**: aws-sdk-go-v2, google-cloud-go, azure-sdk-for-go
- **client-go**: Kubernetes operations
- **K3s**: Local/on-premises Kubernetes

### Foundational Software Stack

- **Keycloak**: Authentication and authorization (OIDC/SAML)
- **Loki**: Log aggregation
- **Grafana**: Visualization and dashboards
- **Tempo**: Distributed tracing
- **Mimir**: Long-term metrics storage
- **OpenTelemetry**: Unified telemetry collection
- **cert-manager**: Automated TLS certificate management
- **Envoy Gateway**: Kubernetes Gateway API implementation
- **ArgoCD**: GitOps continuous deployment
- **Helm**: Package management

### Kubernetes Infrastructure

- **AWS EKS**: Managed Kubernetes on AWS
- **GCP GKE**: Managed Kubernetes on GCP
- **Azure AKS**: Managed Kubernetes on Azure
- **K3s**: Lightweight Kubernetes for local/on-prem

## Design Principles

1. **Stateless Operation**: Query cloud APIs for actual state; no state files to manage
2. **Tag-Based Discovery**: All resources tagged with `nic.nebari.dev/*` for ownership tracking
3. **Declarative Everything**: Specify desired state; NIC reconciles to match
4. **Native Cloud SDKs**: Direct SDK usage for maximum control and flexibility
5. **Automatic Drift Detection**: Compare desired vs actual state on every run
6. **Cloud-Agnostic Abstractions**: Unified interface across AWS, GCP, Azure, On-Prem
7. **Built-in Observability**: OpenTelemetry instrumentation throughout
8. **GitOps Ready**: ArgoCD for application deployment
9. **Security by Default**: Keycloak authentication, cert-manager for TLS
10. **Operator Pattern**: Kubernetes-native app lifecycle management

## Getting Started

### Prerequisites

- Go 1.21+
- kubectl
- Cloud provider CLI (aws-cli, gcloud, or az)
- Valid cloud credentials

### Installation

```bash
# Clone repository
git clone https://github.com/nebari-dev/nebari-infrastructure-core.git
cd nebari-infrastructure-core

# Build NIC
go build -o nic cmd/nic/main.go

# Verify installation
./nic version
```

### Basic Usage

```bash
# Validate configuration
./nic validate -f config.yaml

# Deploy infrastructure and foundational software
./nic deploy -f config.yaml

# Check status (queries cloud APIs for actual state)
./nic status -f config.yaml

# Destroy infrastructure
./nic destroy -f config.yaml
```

## Development

### Running Tests

```bash
# Unit tests
go test ./... -v

# Integration tests
go test ./... -tags=integration -v

# Provider tests (requires cloud credentials)
go test ./... -tags=provider -v

# Coverage report
go test ./... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Code Quality

```bash
# Format code
go fmt ./...

# Lint (requires golangci-lint)
golangci-lint run

# Static analysis
go vet ./...
```

See [Testing Strategy](operations/11-testing-strategy.md) for comprehensive testing guidelines.

## Project Status

**Current Phase**: Design and Architecture
**Target Release**: Q2 2025

See [Timeline and Milestones](operations/12-timeline-milestones.md) for detailed roadmap.

## Contributing

We welcome contributions! Key areas:

- Provider implementations (AWS, GCP, Azure, Local)
- Foundational software integration
- Kubernetes operator development
- Documentation improvements
- Testing and validation

Please read through the architecture and implementation documentation before contributing.

## Support and Feedback

- **Issues**: [GitHub Issues](https://github.com/nebari-dev/nebari-infrastructure-core/issues)
- **Discussions**: [GitHub Discussions](https://github.com/nebari-dev/nebari-infrastructure-core/discussions)
- **Slack**: [Nebari Community](https://nebari.dev/community)

## License

Apache License 2.0 - See LICENSE file for details.

## Related Projects

- **[Nebari](https://github.com/nebari-dev/nebari)**: Original Nebari platform (Terraform-based)
- **[ArgoCD](https://argo-cd.readthedocs.io/)**: GitOps continuous deployment
- **[Keycloak](https://www.keycloak.org/)**: Identity and access management
- **[Grafana LGTM Stack](https://grafana.com/oss/)**: Observability platform
- **[cert-manager](https://cert-manager.io/)**: Certificate management
- **[Envoy Gateway](https://gateway.envoyproxy.io/)**: Kubernetes Gateway API

---

**Note**: This is v2.0 of Nebari Infrastructure Core, representing a complete redesign with no backward compatibility with the original Terraform-based Nebari. See [Introduction](architecture/01-introduction.md) for details on the clean-break approach.
