# Core Configuration

Core Nebari configuration options used by all providers.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [NebariConfig](#nebariconfig)
- [CertificateConfig](#certificateconfig)
- [ACMEConfig](#acmeconfig)

---

## NebariConfig

NebariConfig represents the parsed nebari-config.yaml structure

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| ProjectName | `project_name` | string | Yes |  |
| Provider | `provider` | string | Yes |  |
| Domain | `domain` | string | No |  |
| KubeContext | `kube_context` | string | No | KubeContext specifies an existing Kubernetes context to deploy to. When set, this enables "bring your own cluster" mode - the provider's infrastructure provisioning (Terraform) is skipped, but prov... |
| DNSProvider | `dns_provider` | string | No | DNS provider configuration (optional) |
| DNS | `dns` | `map[string]any` | No | Dynamic DNS config parsed by specific provider |
| GitRepository | `git_repository` | `*git.Config` | No | GitRepository configures the GitOps repository for ArgoCD bootstrap (optional) |
| Certificate | `certificate` | `*CertificateConfig` | No | Certificate configuration (optional) |

---

## CertificateConfig

CertificateConfig holds TLS certificate configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Type | `type` | string | No | Type is the certificate type: "selfsigned" or "letsencrypt" |
| ACME | `acme` | `*ACMEConfig` | No | ACME configuration for Let's Encrypt |

---

## ACMEConfig

ACMEConfig holds ACME (Let's Encrypt) configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Email | `email` | string | Yes | Email is the email address for Let's Encrypt registration |
| Server | `server` | string | No | Server is the ACME server URL (defaults to Let's Encrypt production) Use "https://acme-staging-v02.api.letsencrypt.org/directory" for testing |
