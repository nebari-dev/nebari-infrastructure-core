# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the Nebari Infrastructure Core project.

## What is an ADR?

An ADR is a document that captures an important architectural decision made along with its context and consequences. We use the [MADR](https://adr.github.io/madr/) (Markdown Any Decision Record) format.

## ADR Index

| ID | Title | Status | Date |
|----|-------|--------|------|
| [ADR-0001](0001-git-provider-for-gitops-bootstrap.md) | Git Provider for GitOps Bootstrap | Proposed | 2025-01-21 |
| [ADR-0002](0002-longhorn-distributed-block-storage-for-aws.md) | Longhorn Distributed Block Storage for AWS | Proposed | 2026-02-13 |
| [ADR-0004](0004-out-of-tree-provider-plugins.md) | Out-of-Tree Provider Plugin Architecture | Proposed | 2026-04-15 |
| [ADR-0005](0005-velero-backup-for-singleuser-pvcs.md) | Velero Backup for JupyterHub Singleuser PVCs | Proposed | 2026-05-01 |

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
