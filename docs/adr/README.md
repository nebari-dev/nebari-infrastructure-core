# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the Nebari Infrastructure Core project.

## What is an ADR?

An ADR is a document that captures an important architectural decision made along with its context and consequences. We use the [MADR](https://adr.github.io/madr/) (Markdown Any Decision Record) format.

## ADR Index

| ID | Title | Status | Date |
|----|-------|--------|------|
| [ADR-0001](0001-git-provider-for-gitops-bootstrap.md) | Git Provider for GitOps Bootstrap | Proposed | 2025-01-21 |
| [ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md) | Longhorn Distributed Block Storage for AWS | Proposed | 2026-02-13 |
| [ADR-0003](0003-software-pack-codegen.md) | Software Pack Codegen via ArgoCD Application Generation | Proposed | 2026-03-12 |
| [ADR-0004](0004-out-of-tree-provider-plugins.md) | Out-of-Tree Provider Plugin Architecture | Proposed | 2026-04-15 |
| [ADR-0005](0005-nic-config-cli-surface.md) | nic config CLI surface | Proposed | 2026-06-03 |
| [ADR-0006](0006-conditional-foundational-software-helm.md) | Conditional Foundational Software via Provider-Driven Helm Installs | Proposed | 2026-06-03 |
| [ADR-0007](0007-cloudnativepg-managed-databases.md) | CloudNativePG as Foundational Database Infrastructure | Proposed | 2026-05-12 |
| [ADR-0008](0008-otel-collector-software-pack-override-point.md) | OpenTelemetry Collector Software Pack Override Point | Accepted | 2026-06-02 |
| [ADR-0009](0009-declarative-keycloak-configuration.md) | Declarative Keycloak Configuration via keycloak-config-cli | Accepted | 2026-07-15 |
| [ADR-0012](0012-crossplane-pack-infrastructure.md) | Crossplane Capability APIs for Software Pack Cloud Infrastructure | Proposed | 2026-07-20 |

## ADR Statuses

- **Proposed**: Under discussion, not yet accepted
- **Accepted**: Decision has been made and is active
- **Deprecated**: No longer applies, superseded by another decision
- **Superseded**: Replaced by a newer ADR (link to replacement)

## Creating a New ADR

1. Copy the template: `cp template.md NNNN-title-with-dashes.md`
2. Fill in all sections
3. Submit a PR for review
4. Update the index table above

## Template

See [template.md](template.md) for the MADR template.
