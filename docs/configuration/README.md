# Configuration Reference

This directory contains auto-generated documentation for Nebari Infrastructure Core configuration options.

> This documentation is auto-generated from source code using `go generate`.
> To regenerate, run: `make docs` or `go generate ./cmd/docgen`

## Configuration Files

### Core Configuration

- [Core Configuration](core.md) - Main Nebari configuration (project name, provider, domain)

### Cloud Providers

- [AWS Configuration](aws.md) - Amazon Web Services (EKS) provider options
- [GCP Configuration](gcp.md) - Google Cloud Platform (GKE) provider options
- [Azure Configuration](azure.md) - Microsoft Azure (AKS) provider options
- [Local Configuration](local.md) - Local Kubernetes (K3s) provider options

### Additional Configuration

- [Cloudflare DNS](cloudflare.md) - Cloudflare DNS provider configuration
- [Git Repository](git.md) - GitOps repository configuration for ArgoCD

## Example Configuration

See the [examples](../../examples/) directory for complete configuration examples.
