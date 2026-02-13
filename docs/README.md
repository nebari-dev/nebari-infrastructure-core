# Nebari Infrastructure Core Documentation

This directory contains all documentation for the Nebari Infrastructure Core (NIC) project.

## Documentation Structure

### [Configuration Reference](configuration/) ⭐

Auto-generated documentation for all configuration options:

- [Core Configuration](configuration/core.md) - Project name, provider, domain, certificates
- [AWS Configuration](configuration/aws.md) - EKS, VPC, node groups, EFS
- [GCP Configuration](configuration/gcp.md) - GKE, VPC, node pools, GPUs
- [Azure Configuration](configuration/azure.md) - AKS, networking, node pools
- [Local Configuration](configuration/local.md) - K3s, kind, minikube
- [Git Repository](configuration/git.md) - GitOps with ArgoCD
- [Cloudflare DNS](configuration/cloudflare.md) - DNS provider

> **Note**: Configuration docs are auto-generated from source code comments. Run `make docs` to regenerate.

### [Architecture Decision Records (ADRs)](adr/)

Lightweight records of significant architectural and design decisions made during the project. ADRs follow the [MADR](https://adr.github.io/madr/) (Markdown Any Decision Record) format.

- Captures the "why" behind decisions
- Documents alternatives considered
- Provides context for future contributors

### [Design Documentation](design-doc/)

Comprehensive design documentation covering:

- **Architecture**: Core architectural decisions, principles, and system design
- **Implementation**: Detailed implementation specifications and technical designs
- **Operations**: Testing, deployment, and operational procedures
- **Appendix**: Additional resources and reference materials (including detailed [Configuration Reference](design-doc/appendix/16-configuration-reference.md) with examples)

## When to Use Which

| Document Type | Use When |
|---------------|----------|
| **ADR** | Making a significant decision that affects architecture, technology choice, or design patterns |
| **Design Doc** | Documenting detailed system design, specifications, or implementation guides |

## Contributing

- For new architectural decisions, create an ADR in `adr/`
- For detailed design specifications, add to the appropriate section in `design-doc/`
