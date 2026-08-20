# Configuration Reference

This directory contains auto-generated documentation for Nebari Infrastructure Core configuration options.

> This documentation is auto-generated from source code using `go generate`.
> To regenerate, run: `make docs` or `go generate ./cmd/docgen`

## Configuration Files

### Core Configuration

- [Core Configuration](core.md) - Core Nebari configuration options used by all providers.

### Cloud Providers

- [AWS Provider Configuration](aws.md) - Configuration options specific to Amazon Web Services (EKS).
- [Azure Provider Configuration](azure.md) - Configuration options specific to Microsoft Azure (AKS).
- [Existing Cluster Configuration](existing.md) - Configuration options for attaching to an existing Kubernetes cluster.
- [GCP Provider Configuration](gcp.md) - Configuration options specific to Google Cloud Platform (GKE).
- [Hetzner Provider Configuration](hetzner.md) - Configuration options specific to Hetzner Cloud.
- [Local Provider Configuration](local.md) - Configuration options for local Kubernetes deployments.

### Additional Configuration

- [Trust Bundle Configuration](trust-bundle.md) - Enterprise CA trust-bundle propagation to worker-node OS trust stores and, via trust-manager, into the cluster.
- [Backups Configuration](backups.md) - Off-cluster backup scheduling for Longhorn volumes (S3/Azure targets, retention, keyless auth).
- [Longhorn Storage Configuration](longhorn.md) - Distributed block storage settings shared by the cloud providers, including dedicated-node scheduling.
- [Cloudflare DNS Configuration](cloudflare.md) - Configuration options for Cloudflare DNS provider.
- [Existing GitOps Repository Configuration](repository-existing.md) - Configuration options for pointing ArgoCD at a GitOps repository you already host.
- [Local GitOps Repository Configuration](repository-local.md) - Configuration options for the NIC-managed local GitOps repository ArgoCD syncs from.
