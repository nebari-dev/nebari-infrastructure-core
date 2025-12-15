# Nebari Infrastructure Core (NIC) Documentation

**Version:** 2.0.0
**Status:** Design Phase

## Overview

Nebari Infrastructure Core (NIC) is a next-generation, opinionated Kubernetes deployment tool that provides a complete, production-ready platform across AWS, GCP, Azure, and On-Premises environments. NIC provisions cloud infrastructure and deploys a comprehensive foundational software stack including authentication, observability, certificate management, ingress, and GitOps continuous deployment.

**Key Highlights:**

- **OpenTofu/Terraform Modules**: Infrastructure provisioning via proven Terraform modules orchestrated by terraform-exec
- **Standard State Management**: Terraform state files with remote backends (S3, GCS, Azure Blob)
- **Go CLI Orchestration**: Go CLI wraps OpenTofu execution with OpenTelemetry instrumentation
- **Complete Observability**: LGTM stack (Loki, Grafana, Tempo, Mimir) with OpenTelemetry
- **Integrated Authentication**: Keycloak with OIDC/SAML support
- **GitOps Ready**: ArgoCD for continuous deployment of applications
- **Kubernetes Operator**: Custom nebari-application CRD for seamless app registration

## Documentation Structure

This documentation is organized into four main categories:

### Architecture

Core architectural decisions, principles, and system design.

1. **[Introduction](architecture/01-introduction.md)**
   Project vision, core principles, and design philosophy

2. **[System Overview](architecture/02-system-overview.md)**
   High-level architecture, component relationships, and deployment flow

3. **[Goals and Non-Goals](architecture/03-goals-and-non-goals.md)**
   Explicit scope boundaries and design constraints

4. **[Key Architectural Decisions](architecture/04-key-decisions.md)**
   Critical technical choices and their rationale

5. **[State Management](architecture/05-state-management.md)**
   Terraform state backends, locking, and drift detection

### Implementation

Detailed implementation specifications and technical designs.

6. **[OpenTofu Module Architecture](implementation/06-opentofu-module-architecture.md)**
   Infrastructure as code using Terraform/OpenTofu modules

7. **[Configuration Design](implementation/07-configuration-design.md)**
   config.yaml structure and validation

8. **[Terraform-Exec Integration](implementation/08-terraform-exec-integration.md)**
   Go CLI orchestration of OpenTofu via terraform-exec library

9. **[DNS Provider Architecture](implementation/09-dns-provider-architecture.md)**
   DNS provider abstraction and implementation patterns

10. **[Foundational Software Stack](implementation/10-foundational-software.md)**
    Keycloak, LGTM observability, cert-manager, Envoy Gateway, ArgoCD deployment

11. **[Nebari Kubernetes Operator](implementation/11-nebari-operator.md)**
    Custom controller for nebari-application CRD and app lifecycle management

### Operations

Testing, deployment, and operational procedures.

12. **[Testing Strategy](operations/12-testing-strategy.md)**
    Unit, integration, provider, and end-to-end testing approaches

13. **[Milestones](operations/13-milestones.md)**
    Development roadmap and release planning

### Appendix

Additional resources and reference materials.

14. **[Open Questions](appendix/14-open-questions.md)**
    Unresolved design decisions and areas needing further investigation

15. **[Future Enhancements](appendix/15-future-enhancements.md)**
    Detailed specifications for v1.x features: Git automation, software stack specification, full-stack-in-one-repo

16. **[Configuration File Reference](appendix/16-configuration-reference.md)**
    Complete schema and field descriptions for config.yaml

17. **[Appendix](appendix/17-appendix.md)**
    Glossary, references, and supplementary information

## Quick Navigation by Topic

### For New Contributors

Start here to understand the project:

- [Introduction](architecture/01-introduction.md)
- [System Overview](architecture/02-system-overview.md)
- [Goals and Non-Goals](architecture/03-goals-and-non-goals.md)

### For Infrastructure Engineers

Deep dive into cloud infrastructure:

- [OpenTofu Module Architecture](implementation/06-opentofu-module-architecture.md)
- [Terraform-Exec Integration](implementation/08-terraform-exec-integration.md)
- [State Management](architecture/05-state-management.md)

### For Platform Engineers

Understand the foundational software stack:

- [Foundational Software Stack](implementation/10-foundational-software.md)
- [Nebari Kubernetes Operator](implementation/11-nebari-operator.md)
- [Configuration Design](implementation/07-configuration-design.md)

### For Architects

Review key technical decisions:

- [Key Architectural Decisions](architecture/04-key-decisions.md)
- [State Management](architecture/05-state-management.md)
- [OpenTofu Module Architecture](implementation/06-opentofu-module-architecture.md)

### For QA Engineers

Testing and validation:

- [Testing Strategy](operations/12-testing-strategy.md)
- [Milestones](operations/13-milestones.md)

## Core Technologies

### Infrastructure Management

- **Go**: CLI orchestration layer
- **OpenTofu/Terraform**: Infrastructure provisioning via HCL modules
- **terraform-exec**: Go library for programmatic OpenTofu execution
- **Terraform State**: Remote state backends (S3, GCS, Azure Blob)
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

1. **OpenTofu Modules**: Leverage battle-tested Terraform modules for infrastructure
2. **terraform-exec Orchestration**: Go CLI orchestrates OpenTofu via terraform-exec library
3. **Standard State Management**: Terraform state files with remote backends
4. **Declarative Everything**: Specify desired state; OpenTofu reconciles to match
5. **Drift Detection**: Terraform plan compares state with actual infrastructure
6. **Cloud-Agnostic Abstractions**: Unified interface across AWS, GCP, Azure, On-Prem
7. **Built-in Observability**: OpenTelemetry instrumentation throughout
8. **GitOps Ready**: ArgoCD for application deployment
9. **Security by Default**: Keycloak authentication, cert-manager for TLS
10. **Operator Pattern**: Kubernetes-native app lifecycle management

## Getting Started

### Prerequisites

- Go 1.21+
- OpenTofu 1.6+ (or Terraform 1.6+)
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

# Check status (runs terraform plan)
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

See [Testing Strategy](operations/12-testing-strategy.md) for comprehensive testing guidelines.

## Project Status

**Current Phase**: Design and Architecture
**Target Release**: Q2 2025

See [Milestones](operations/13-milestones.md) for detailed roadmap.

## Contributing

We welcome contributions! Key areas:

- OpenTofu module development (AWS, GCP, Azure, Local)
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
- **[OpenTofu](https://opentofu.org/)**: Open-source Terraform fork
- **[ArgoCD](https://argo-cd.readthedocs.io/)**: GitOps continuous deployment
- **[Keycloak](https://www.keycloak.org/)**: Identity and access management
- **[Grafana LGTM Stack](https://grafana.com/oss/)**: Observability platform
- **[cert-manager](https://cert-manager.io/)**: Certificate management
- **[Envoy Gateway](https://gateway.envoyproxy.io/)**: Kubernetes Gateway API

---

**Note**: This is v2.0 of Nebari Infrastructure Core, representing a complete redesign with no backward compatibility with the original Terraform-based Nebari. See [Introduction](architecture/01-introduction.md) for details on the clean-break approach.
