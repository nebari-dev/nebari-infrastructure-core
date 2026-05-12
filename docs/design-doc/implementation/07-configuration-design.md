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
    GitRepository *git.Config        `yaml:"git_repository,omitempty"`
    Certificate   *CertificateConfig `yaml:"certificate,omitempty"`
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

git_repository: { ... }        # optional on local provider; required for cloud providers
certificate: { ... }           # optional, defaults to selfsigned
```

There is **no** top-level `provider:` field, **no** top-level `version:` field, **no** top-level `name:` field (use `project_name`), and **no** top-level `kubernetes:`, `node_pools:`, `tls:`, `foundational_software:`, `images:`, or `features:` blocks. If you find documentation that claims otherwise, it is out of date.

## 7.3 Cluster Provider Block

```go
type ClusterConfig struct {
    Providers map[string]any `yaml:",inline"`
}
```

Exactly one key under `cluster:`. Valid provider names (from `cmd/nic/main.go` registration): `aws`, `gcp`, `azure`, `local`, `hetzner`, `existing`. GCP and Azure are registered but their methods return "not yet implemented".

The inline map captures the provider name as the key and an opaque `any` as the value. The provider implementation is responsible for decoding the `any` into its own typed config (e.g., `pkg/provider/aws/config.go:Config` for AWS).

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
    Type string      `yaml:"type,omitempty"`   // "selfsigned" or "letsencrypt"
    ACME *ACMEConfig `yaml:"acme,omitempty"`
}

type ACMEConfig struct {
    Email  string `yaml:"email"`
    Server string `yaml:"server,omitempty"`    // staging URL for testing
}
```

`selfsigned` is the default and is appropriate for local and internal deployments. `letsencrypt` requires `acme.email` (and a publicly-routable `domain` configured via the DNS provider).

## 7.6 Git Repository Block

```go
// from pkg/git
type Config struct {
    URL    string     `yaml:"url"`              // git@..., https://..., or file://...
    Branch string     `yaml:"branch,omitempty"` // default: main
    Path   string     `yaml:"path,omitempty"`   // subdirectory for this cluster
    Auth   AuthConfig `yaml:"auth,omitempty"`
    ArgocdAuth AuthConfig `yaml:"argocd_auth,omitempty"` // optional read-only
}

type AuthConfig struct {
    SSHKeyEnv string `yaml:"ssh_key_env,omitempty"`
    TokenEnv  string `yaml:"token_env,omitempty"`
}
```

The git repository is where NIC renders ArgoCD `Application` manifests during deploy. ArgoCD then syncs from it.

- **Local file:// repos** are valid (and the default for local Kind clusters that have `InfraSettings.SupportsLocalGitOps = true`). The local provider's auto-bootstrap creates `/tmp/nebari-gitops-<project_name>` if no `git_repository:` block is provided.
- **Cloud providers** require an explicit `git_repository:` block; cluster nodes cannot see the dev machine's filesystem, so a remote (SSH or HTTPS) repo is required.
- Credentials are referenced by env-var name, never inlined. The CLI scrubs the `auth:` and `argocd_auth:` blocks from any copy of the config it writes into the GitOps repo.

## 7.7 Example Configs

Authoritative examples live under [`examples/`](../../../examples/) in the repo. Highlights:

- [`examples/aws-config.yaml`](../../../examples/aws-config.yaml) - EKS with EFS and remote GitOps repo
- [`examples/hetzner-config.yaml`](../../../examples/hetzner-config.yaml) - Hetzner k3s with `node_groups.master` and `node_groups.workers`
- [`examples/local-config.yaml`](../../../examples/local-config.yaml) - Kind cluster with optional MetalLB and `file://` GitOps repo
- [`examples/existing-config.yaml`](../../../examples/existing-config.yaml) - Adopt an existing kubeconfig
- [`examples/gcp-config.yaml`](../../../examples/gcp-config.yaml), [`examples/azure-config.yaml`](../../../examples/azure-config.yaml) - schema for the stub providers (not deployable today)

The full per-provider field reference lives in [`16-configuration-reference.md`](../appendix/16-configuration-reference.md).

## 7.8 Validation

`NebariConfig.Validate(opts ValidateOptions)` runs at parse time. `ValidateOptions` carries the set of valid cluster and DNS provider names, supplied by the caller (typically `cmd/nic` looking up names from `pkg/registry`). The config package itself doesn't know which provider names are valid, which keeps it decoupled from provider implementations.

Validation enforces:

- `project_name` is set and matches `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`
- `cluster:` is present with exactly one provider key matching `opts.ClusterProviders`
- `dns:`, if present, has exactly one provider key matching `opts.DNSProviders`
- `git_repository:`, if present, validates per `pkg/git.Config.Validate()`

Provider-specific validation (e.g., that `cluster.aws.region` is set, that node groups are non-empty) lives inside the provider's own `Validate(ctx, projectName, clusterConfig)` method.

## 7.9 Auto-Discovery

If `nic deploy` is invoked without `-f`, the CLI auto-discovers a config file in the working directory. See `cmd/nic/config_discovery.go` for the search order. Explicit `-f path/to/config.yaml` always wins.

## 7.10 Secrets

Secrets are never written into the config file. The expected pattern:

```bash
# .env (gitignored; loaded automatically by godotenv in main.go)
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
HCLOUD_TOKEN=...
CLOUDFLARE_API_TOKEN=...
GIT_SSH_PRIVATE_KEY=...
```

The `git_repository.auth.ssh_key_env` / `token_env` fields point at env-var names, not at the values. This keeps the config file safe to commit and lets the same file be used across operator machines with different credentials.
