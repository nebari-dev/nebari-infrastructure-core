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

- [Git Repository Configuration](git.md) - Configuration options for GitOps repository integration with ArgoCD.
- [Cloudflare DNS Configuration](cloudflare.md) - Configuration options for Cloudflare DNS provider.
