# Configuration Design

## 7.1 Principles

NIC's configuration philosophy:

1. **Single config file**: One `nebari-config.yaml` is the source of truth for a deployment.
2. **Discriminator pattern for providers**: `cluster.<provider>:` and `dns.<provider>:` use the provider name as the map key, with provider-specific config underneath. The `config` package never imports a provider package; per-provider decoding happens inside each provider.
3. **No secrets in config**: Credentials live in environment variables (typically loaded from `.env`). Config files are safe to check into a GitOps repo.
4. **Validate at parse time**: `NebariConfig.Validate(opts)` checks required fields and provider-name validity before any infrastructure call.
5. **Provider capabilities flow through `InfraSettings`**: Code outside `cmd/nic` and a provider's own package never branches on provider name; capabilities like `NeedsMetalLB` or `StorageClass` are read from `provider.InfraSettings(cfg)`.

## 7.2 Top-Level Schema

`NebariConfig` in `pkg/config/config.go`:

```go
type NebariConfig struct {
    ProjectName   string             `yaml:"project_name"`            // required
    Domain        string             `yaml:"domain,omitempty"`
    Cluster       *ClusterConfig     `yaml:"cluster,omitempty"`       // required
    DNS           *DNSConfig         `yaml:"dns,omitempty"`           // optional
    Repository    *RepositoryConfig  `yaml:"repository,omitempty"`    // required
    Certificate   *CertificateConfig `yaml:"certificate,omitempty"`
    TrustBundle   *TrustBundleConfig `yaml:"trust_bundle,omitempty"`  // enterprise CA bundle
    Backups       *BackupsConfig     `yaml:"backups,omitempty"`       // off-cluster Longhorn backups
}
```

The corresponding minimal YAML:

```yaml
project_name: my-nebari        # required, [a-zA-Z0-9][a-zA-Z0-9_-]*
domain: nebari.example.com     # optional, but needed for routable services

cluster:                       # required, exactly one provider
  aws: { ... }

dns:                           # optional, exactly one provider
  cloudflare: { ... }

repository:                    # required, exactly one provider
  existing: { ... }

certificate: { ... }           # optional, defaults to selfsigned
trust_bundle: { ... }          # optional, enterprise CA bundle (path OR inline)
backups: { ... }               # optional, off-cluster Longhorn backups
```

There is **no** top-level `provider:` field, **no** top-level `version:` field, **no** top-level `name:` field (use `project_name`), and **no** top-level `kubernetes:`, `node_pools:`, `tls:`, `foundational_software:`, `images:`, or `features:` blocks. If you find documentation that claims otherwise, it is out of date.

## 7.3 Cluster Provider Block

```go
type ClusterConfig struct {
    Providers map[string]any `yaml:",inline"`
}
```

Exactly one key under `cluster:`. Valid provider names (from `pkg/nic/registry.go`'s `defaultRegistry`): `aws`, `gcp`, `azure`, `local`, `hetzner`, `existing`. All are implemented except `gcp`, which is a registered stub (its `Deploy`/`Destroy` emit a "(stub)" status message and return `nil`, and `GetKubeconfig` returns "not yet implemented").

The inline map captures the provider name as the key and an opaque `any` as the value. The provider implementation is responsible for decoding the `any` into its own typed config (e.g., `pkg/providers/cluster/aws/config.go:Config` for AWS).

## 7.4 DNS Provider Block

Same shape as `cluster`:

```go
type DNSConfig struct {
    Providers map[string]any `yaml:",inline"`
}
```

Valid provider names today: `cloudflare`. The DNS provider implementation owns the schema for its config. See [09-dns-provider-architecture.md](09-dns-provider-architecture.md).

## 7.5 Certificate Block

```go
type CertificateConfig struct {
    Type string      `yaml:"type,omitempty"`   // "selfsigned" (default), "letsencrypt", or "existing"
    ACME *ACMEConfig `yaml:"acme,omitempty"`

    SecretName     string             `yaml:"secret_name,omitempty"`      // default: nebari-gateway-tls
    ExistingSecret *ExistingSecretRef `yaml:"existing_secret,omitempty"`  // type=existing: one of
    Files          *CertFiles         `yaml:"files,omitempty"`            //   these three
    Env            *CertEnv           `yaml:"env,omitempty"`              //   must be set
}

type ACMEConfig struct {
    Email  string `yaml:"email"`
    Server string `yaml:"server,omitempty"`    // staging URL for testing
}
```

`selfsigned` is the default and is appropriate for local and internal deployments. `letsencrypt` requires `acme.email` (and a publicly-routable `domain` configured via the DNS provider). `existing` is the bring-your-own-certificate path: it takes exactly one of `existing_secret` (reference a `kubernetes.io/tls` secret already in the cluster), `files` (PEM paths on disk), or `env` (raw PEM in env vars), and cannot be combined with `acme`. For the files/env sources NIC creates the secret directly in `envoy-gateway-system`, so the material never reaches the GitOps repo. See [`docs/custom-tls-certificate.md`](../../custom-tls-certificate.md).

## 7.5b Trust Bundle and Backups Blocks

Two further optional top-level blocks, defined in `pkg/config/trust_bundle.go` and `pkg/config/backups.go`:

- `trust_bundle:` takes exactly one of `path` (a PEM file on the operator's machine) or `inline` (the PEM text). NIC propagates it to worker-node OS trust stores via the cluster provider and into the cluster via trust-manager. A `PRIVATE KEY` block is rejected: the resolved bundle ends up in OpenTofu state and is projected cluster-wide through the GitOps repo.
- `backups.longhorn:` configures off-cluster backups against a Longhorn-native S3 or azblob target, plus the snapshot/backup RecurringJob schedules. Opt-in: an omitted block means no backups.

Field-level detail for both lives in [`16-configuration-reference.md`](../appendix/16-configuration-reference.md).

## 7.6 Repository Block

The GitOps repository follows the same provider pattern as `cluster:` and `dns:` (`RepositoryConfig` in `pkg/config/config.go`): exactly one provider key, backed by the provider implementations in `pkg/providers/repository/`. Two providers exist: `existing` (a remote repo NIC clones and pushes to) and `local` (a directory on the host, for dev clusters).

```go
// from pkg/providers/repository/existing
type Config struct {
    URL        string      `yaml:"url"`                   // git@... or https://...
    Branch     string      `yaml:"branch"`                // default: main
    Path       string      `yaml:"path"`                  // subdirectory for this cluster
    Auth       AuthConfig  `yaml:"auth"`                  // NIC's write credentials
    ArgoCDAuth *AuthConfig `yaml:"argocd_auth,omitempty"` // optional read-only; falls back to Auth
}

type AuthConfig struct {
    Token *EnvRef `yaml:"token,omitempty"` // HTTPS token auth
    SSH   *EnvRef `yaml:"ssh,omitempty"`   // SSH private-key auth
    InsecureSkipHostKeyVerification bool `yaml:"insecure_skip_host_key_verification,omitempty"`
}

type EnvRef struct {
    Env string `yaml:"env"` // name of the env var holding the secret
}

// from pkg/providers/repository/local
type Config struct {
    Path   string `yaml:"path"`   // default: ~/.nic/gitops/<project_name>
    Branch string `yaml:"branch"` // default: main
}
```

The repository is where NIC renders ArgoCD `Application` manifests during deploy. ArgoCD then syncs from it.

- **`repository.local`** provisions a directory on the host and is only valid on cluster providers with `InfraSettings.SupportsLocalGitOps = true` (currently only local Kind clusters). When `path` is omitted, NIC creates `~/.nic/gitops/<project_name>` (`config.DefaultLocalRepositoryPath`), falling back to `$TMPDIR/nebari-gitops-<project_name>` only when the home directory cannot be resolved.
- **Cloud providers** require `repository.existing`; cluster nodes cannot see the dev machine's filesystem, so a remote (SSH or HTTPS) repo is required.
- Credentials are referenced by env-var name, never inlined, so the copy of the config NIC commits into the repo (`nic-config.yaml`) is safe as-is; only a `path:`-based trust bundle is rewritten to its resolved inline form (`committedConfig` in `pkg/nic/deploy.go`).

## 7.7 Example Configs

Authoritative examples live under [`examples/`](../../../examples/) in the repo. Highlights:

- [`examples/aws-config.yaml`](../../../examples/aws-config.yaml) - EKS with EFS and remote GitOps repo
- [`examples/hetzner-config.yaml`](../../../examples/hetzner-config.yaml) - Hetzner k3s with `node_groups.master` and `node_groups.workers`
- [`examples/local-config.yaml`](../../../examples/local-config.yaml) - Kind cluster with optional `kind:` tuning, MetalLB address pool, and `file://` GitOps repo
- [`examples/custom-tls-config.yaml`](../../../examples/custom-tls-config.yaml) - `certificate.type: existing` (bring your own TLS cert)
- [`examples/longhorn-backups-config.yaml`](../../../examples/longhorn-backups-config.yaml) - EKS with a dedicated Longhorn storage pool and S3 backups
- [`examples/existing-config.yaml`](../../../examples/existing-config.yaml) - Adopt an existing kubeconfig
- [`examples/azure-config.yaml`](../../../examples/azure-config.yaml) - AKS (deployable; Azure is implemented)
- [`examples/gcp-config.yaml`](../../../examples/gcp-config.yaml) - schema for the GCP stub provider (not deployable today)

The full per-provider field reference lives in [`16-configuration-reference.md`](../appendix/16-configuration-reference.md).

## 7.8 Validation

`NebariConfig.Validate(opts ValidateOptions)` runs at parse time. `ValidateOptions` carries the sets of valid cluster, DNS, and repository provider names, supplied by the caller (typically `cmd/nic` looking up names from `pkg/registry`). The config package itself doesn't know which provider names are valid, which keeps it decoupled from provider implementations.

Validation enforces:

- `project_name` is set and matches `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`
- `cluster:` is present with exactly one provider key matching `opts.ClusterProviders`
- `dns:`, if present, has exactly one provider key matching `opts.DNSProviders`
- `repository:` is present with exactly one provider key matching `opts.RepositoryProviders`
- `trust_bundle:`, if present, has at most one of `path` / `inline`; an inline value must contain a PEM certificate and no private key (a `path:` bundle is only read at deploy/destroy time, so `Validate()` never touches disk)
- `certificate:` validates the type and, for `type: existing`, that exactly one of `existing_secret` / `files` / `env` is set
- `backups:`, if present and enabled, validates the target, schedules, and credentials against the selected cluster provider name

Provider-specific validation (e.g., that `cluster.aws.region` is set, that node groups are non-empty) lives inside the provider's own `Validate(ctx, projectName, clusterConfig)` method.

## 7.9 Auto-Discovery

If `nic deploy` is invoked without `-f`, the CLI falls back to the `NIC_CONFIG_PATH` environment variable, then to `./config.yaml` in the working directory (`resolveConfigFile` in `cmd/nic/config_discovery.go`). Explicit `-f path/to/config.yaml` always wins. In every case the resolved path is checked for readability before parsing.

## 7.10 Secrets

Secrets are never written into the config file. The expected pattern:

```bash
# .env (gitignored; loaded automatically by godotenv in main.go)
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
HETZNER_TOKEN=...           # NIC re-exports this to the hetzner-k3s subprocess as HCLOUD_TOKEN
AZURE_SUBSCRIPTION_ID=...   # mapped to ARM_SUBSCRIPTION_ID for the child tofu process
CLOUDFLARE_API_TOKEN=...
GIT_SSH_PRIVATE_KEY=...
```

The `repository.existing.auth.ssh.env` / `token.env` fields point at env-var names, not at the values. This keeps the config file safe to commit and lets the same file be used across operator machines with different credentials.
