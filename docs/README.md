# Nebari Infrastructure Core Documentation

This directory contains all documentation for the Nebari Infrastructure Core (NIC) project.

## Documentation Structure

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
- **Appendix**: Additional resources and reference materials

### Guides

- [CLI reference](cli-reference.md)
- [Local development with kind](local-kind-development.md)
- [Custom TLS certificates](custom-tls-certificate.md)
- [Resource sizing](resource-sizing.md) - default requests/limits for foundational software, what each component scales with, and how to tune them

## When to Use Which

| Document Type | Use When |
|---------------|----------|
| **ADR** | Making a significant decision that affects architecture, technology choice, or design patterns |
| **Design Doc** | Documenting detailed system design, specifications, or implementation guides |

## Contributing

- For new architectural decisions, create an ADR in `adr/`
- For detailed design specifications, add to the appropriate section in `design-doc/`
