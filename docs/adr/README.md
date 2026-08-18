# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the Nebari Infrastructure Core project.

## What is an ADR?

An ADR is a document that captures an important architectural decision made along with its context and consequences. We use the [MADR](https://adr.github.io/madr/) (Markdown Any Decision Record) format.

## ADR Index

| ID | Title | Status | Date |
|----|-------|--------|------|
| [ADR-0001](0001-git-provider-for-gitops-bootstrap.md) | Git Provider for GitOps Bootstrap | Superseded by [ADR-0015](0015-repository-provider-abstraction.md) | 2025-01-21 |
| [ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md) | Longhorn Distributed Block Storage for AWS | Proposed | 2026-02-13 |
| [ADR-0003](0003-software-pack-codegen.md) | Software Pack Codegen via ArgoCD Application Generation | Proposed | 2026-03-12 |
| [ADR-0004](0004-out-of-tree-provider-plugins.md) | Out-of-Tree Provider Plugin Architecture | Proposed | 2026-04-15 |
| [ADR-0005](0005-nic-config-cli-surface.md) | nic config CLI surface | Proposed | 2026-06-03 |
| [ADR-0006](0006-conditional-foundational-software-helm.md) | Conditional Foundational Software via Provider-Driven Helm Installs | Proposed | 2026-06-03 |
| [ADR-0007](0007-cloudnativepg-managed-databases.md) | CloudNativePG as Foundational Database Infrastructure | Proposed | 2026-05-12 |
| [ADR-0008](0008-otel-collector-software-pack-override-point.md) | OpenTelemetry Collector Software Pack Override Point | Accepted | 2026-06-02 |
| [ADR-0009](0009-declarative-keycloak-configuration.md) | Declarative Keycloak Configuration via keycloak-config-cli | Accepted | 2026-07-15 |
| [ADR-0010](0010-high-security-mode.md) | High-Security Mode (Opt-In Whitelist-Everything Hardening) | Proposed | 2026-07-15 |
| [ADR-0014](0014-helm-valuefiles-overlay-seam.md) | Helm valueFiles Overlay Seam for Foundational Apps | Accepted | 2026-07-22 |
| [ADR-0015](0015-repository-provider-abstraction.md) | Repository Provider Abstraction for GitOps Bootstrap | Accepted | 2026-07-10 |
| [ADR-0016](0016-opentofu-runtime-version-policy.md) | OpenTofu Runtime Version Policy (External Binaries and Compatibility Window) | Proposed | 2026-08-12 |

## Argument maps

An ADR records a decision and its consequences. When the reasoning behind one is too tangled to follow as prose — several arguments running at once, objections to the objections, claims that only look settled — an [Argdown](https://argdown.org/) argument map is a useful complement: each claim becomes a node, each objection an arrow, and what is still open stays visibly open. The map does not replace the ADR. It shows the working the ADR concludes from, and stays alongside it so the reasoning can be revisited when the decision is.

Not every decision needs one. Reach for a map when the argument is the hard part. Each map lives in its own directory with its source, its rendered SVGs, and a README that walks the argument section by section; `make argdown` re-renders all of them.

- [storage-strategy](storage-strategy/) — Nebari storage strategy: which of Longhorn's roles it should keep, and what replaces the rest

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
