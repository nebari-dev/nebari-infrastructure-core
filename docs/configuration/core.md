# Core Configuration

Core Nebari configuration options used by all providers.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [NebariConfig](#nebariconfig)
- [CertificateConfig](#certificateconfig)
- [ACMEConfig](#acmeconfig)
- [ExistingSecretRef](#existingsecretref)
- [CertFiles](#certfiles)
- [CertEnv](#certenv)

---

## NebariConfig

NebariConfig represents the parsed nebari-config.yaml structure

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| ProjectName | `project_name` | string | Yes |  |
| Domain | `domain` | string | No |  |
| Cluster | `cluster` | `*ClusterConfig` | No | Cluster Provider configuration. Only one provider can be configured at a time. |
| DNS | `dns` | `*DNSConfig` | No | DNS provider configuration (optional). Only one provider can be configured at a time. |
| GitRepository | `git_repository` | `*git.Config` | No | GitRepository configures the GitOps repository for ArgoCD bootstrap (optional) |
| Certificate | `certificate` | `*CertificateConfig` | No | Certificate configuration (optional) |
| TrustBundle | `trust_bundle` | `*TrustBundleConfig` | No | TrustBundle, when set, propagates an enterprise CA bundle both to worker-node OS trust stores (via the cluster provider) and into the cluster via trust-manager. Required when egress is TLS-inspecte... |

---

## CertificateConfig

CertificateConfig holds TLS certificate configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Type | `type` | string | No | Type is the certificate type: "selfsigned", "letsencrypt", or "existing" |
| ACME | `acme` | `*ACMEConfig` | No | ACME configuration for Let's Encrypt |
| SecretName | `secret_name` | string | No | SecretName overrides the name of the TLS secret the gateway references. Defaults to "nebari-gateway-tls". For type=existing with existing_secret, the gateway references ExistingSecret.Name instead. |
| ExistingSecret | `existing_secret` | `*ExistingSecretRef` | No | ExistingSecret references a kubernetes.io/tls secret the user already created. Mutually exclusive with Files and Env. Only valid when Type=existing. |
| Files | `files` | `*CertFiles` | No | Files reads PEM material from disk; NIC creates the secret directly. Mutually exclusive with ExistingSecret and Env. Only valid when Type=existing. |
| Env | `env` | `*CertEnv` | No | Env reads raw PEM material from environment variables; NIC creates the secret directly. Mutually exclusive with ExistingSecret and Files. Only valid when Type=existing. |

---

## ACMEConfig

ACMEConfig holds ACME (Let's Encrypt) configuration

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Email | `email` | string | Yes | Email is the email address for Let's Encrypt registration |
| Server | `server` | string | No | Server is the ACME server URL (defaults to Let's Encrypt production) Use "https://acme-staging-v02.api.letsencrypt.org/directory" for testing |

---

## ExistingSecretRef

ExistingSecretRef references a pre-existing TLS secret.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Name | `name` | string | Yes | Name is the secret name (required). |
| Namespace | `namespace` | string | No | Namespace is the secret's namespace. Defaults to envoy-gateway-system. |

---

## CertFiles

CertFiles points at PEM cert/key files on disk.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| CertFile | `cert_file` | string | Yes |  |
| KeyFile | `key_file` | string | Yes |  |

---

## CertEnv

CertEnv names environment variables holding raw (non-base64) PEM material.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| CertEnv | `cert_env` | string | Yes |  |
| KeyEnv | `key_env` | string | Yes |  |

