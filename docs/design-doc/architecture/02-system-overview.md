# System Overview

### 2.1 High-Level Architecture

**NIC Deployment Flow:**

```
┌─────────────────────────────────────────────────────────────┐
│ 1. User defines nebari-config.yaml                          │
│    - cluster.<provider>: ...     (aws | hetzner |           │
│                                   local | existing)         │
│    - dns.<provider>: ...         (optional, cloudflare)     │
│    - git_repository: ...         (optional on local)        │
│    - certificate: ...            (selfsigned | letsencrypt) │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. NIC CLI parses config and dispatches to a provider       │
│    $ nic deploy -f config.yaml                              │
│    - cmd/nic parses YAML into pkg/config.NebariConfig       │
│    - Looks up the provider from pkg/registry.Registry       │
│    - Calls provider.Deploy(ctx, projectName, cluster, opts) │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. Cluster Provisioning (provider-specific)                 │
│    - AWS:     pkg/tofu.Setup → OpenTofu init/plan/apply     │
│               using embedded templates that call the        │
│               upstream nebari-dev/eks-cluster Terraform     │
│               module. State lives in S3 with native         │
│               lockfile-based locking.                       │
│    - Hetzner: shells out to the hetzner-k3s binary against  │
│               the Hetzner Cloud API. No tofu involved.      │
│    - Local:   stub - user runs `make localkind-up`, which   │
│               creates a Kind cluster and then invokes nic.  │
│    - Existing: no-op; uses kubeconfig + context from config.│
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. GitOps Bootstrap (pkg/argocd, pkg/git)                   │
│    - Renders ArgoCD app manifests into a Git repository     │
│      (remote or file://) configured via git_repository      │
│    - For providers with InfraSettings.SupportsLocalGitOps=  │
│      true (local/Kind), auto-creates a local repo if none   │
│      is configured                                          │
│    - Commits and pushes (or commits locally for file://)    │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 5. ArgoCD Install (pkg/argocd, pkg/helm)                    │
│    - NIC installs ArgoCD via the embedded Helm Go SDK       │
│      (helm.sh/helm/v3/pkg/action), not via a Terraform      │
│      helm_release resource                                  │
│    - Configures Keycloak OIDC for SSO                       │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 6. Foundational Services (ArgoCD Applications)              │
│    Manifests live under pkg/argocd/templates/apps/ and are  │
│    rendered into the GitOps repo. ArgoCD then syncs them    │
│    via a root app-of-apps:                                  │
│    ├── cert-manager + cluster-issuers + certificates        │
│    ├── Envoy Gateway + gateway-config + httproutes          │
│    ├── postgresql + Keycloak                                │
│    ├── metallb + metallb-config (only when needed)          │
│    ├── opentelemetry-collector                              │
│    ├── nebari-operator (kustomized from upstream repo)      │
│    └── nebari-landingpage                                   │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 7. DNS + Endpoint Surfacing (optional)                      │
│    - pkg/endpoint watches the Envoy Gateway Service for an  │
│      assigned load-balancer hostname or IP                  │
│    - If dns.<provider> is configured (Cloudflare today),    │
│      records are provisioned automatically                  │
│    - Otherwise the CLI prints exact A/CNAME instructions    │
└─────────────────────────────────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│ 8. Platform Ready                                           │
│    - Kubernetes cluster running (or adopted)                │
│    - Foundational software syncing via ArgoCD               │
│    - Auth and routing configured                            │
│    - Users can install NebariApp software packs             │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 Component Breakdown

The actual repository layout is captured in [`AGENTS.md`](../../../AGENTS.md). Key packages:

**`cmd/nic/` (CLI)**

- Cobra-based commands: `deploy`, `destroy`, `validate`, `kubeconfig`, `version`. There is no `status` or `plan` subcommand today.
- Reads `.env` via `godotenv` and initializes OpenTelemetry via `pkg/telemetry`.
- Owns the `slog` JSON logger. Library code (under `pkg/`) does not log.
- Owns the status-channel handler (`cmd/nic/status_handler.go`); see Section 2.4.

**`pkg/provider/` (Cluster providers)**

- `pkg/provider/provider.go` defines the `Provider` interface (`Name`, `Validate`, `Deploy`, `Destroy`, `GetKubeconfig`, `Summary`, `InfraSettings`) and the `InfraSettings` capability struct (`StorageClass`, `NeedsMetalLB`, `LoadBalancerAnnotations`, `MetalLBAddressPool`, `KeycloakBasePath`, `HTTPSPort`, `EFSStorageClass`, `SupportsLocalGitOps`).
- One sub-package per cluster provider: `aws/`, `hetzner/`, `local/`, `existing/`, plus `gcp/` and `azure/` stubs (registered, but their `Validate`/`Deploy`/`Destroy` methods emit a "(stub)" status message and return `nil` rather than provisioning anything).
- AWS-specific Terraform templates live under `pkg/provider/aws/templates/` and are embedded into the binary via `go:embed`.

**`pkg/dnsprovider/` (DNS providers)**

- `pkg/dnsprovider/provider.go` defines the `DNSProvider` interface (`Name`, `ProvisionRecords`, `DestroyRecords`).
- `pkg/dnsprovider/cloudflare/` is the only implementation today.

**`pkg/registry/` (Unified provider registry)**

- `registry.Registry` holds two `ProviderList` instances: `ClusterProviders` (a `ProviderList[provider.Provider]`) and `DNSProviders` (a `ProviderList[dnsprovider.DNSProvider]`).
- All providers are registered explicitly in `cmd/nic/main.go` `init()`. No blank imports or `init()` magic.

**`pkg/tofu/` (terraform-exec wrapper)**

- Single file `pkg/tofu/tofu.go` defines `TerraformExecutor`, which embeds `*tfexec.Terraform` plus a temp working dir and an `afero.Fs`.
- `Setup(ctx, templates fs.FS, tfvars any)` extracts embedded templates, downloads the OpenTofu binary via `tofudl` with caching at `~/.cache/nic/tofu/`, sets `TF_PLUGIN_CACHE_DIR`, writes `terraform.tfvars.json`, and returns the executor.
- `Init`, `Plan`, `Apply`, `Destroy` call the `*JSON` variants of `tfexec` and stream output through the status channel (Section 2.4). `Output` uses the standard tfexec entry point.

**`pkg/config/` (Config parsing)**

- `pkg/config/config.go` defines `NebariConfig` with fields `ProjectName`, `Domain`, `Cluster *ClusterConfig`, `DNS *DNSConfig`, `GitRepository *git.Config`, `Certificate *CertificateConfig`.
- `ClusterConfig` and `DNSConfig` both use the discriminator pattern: a single inline map keyed by provider name. Provider-specific config is opaque to the config package and is decoded by the provider itself.

**`pkg/argocd/` (ArgoCD orchestration)**

- Installs ArgoCD via the embedded Helm Go SDK (`pkg/helm`), not via a Terraform `helm_release`.
- Renders the foundational app-of-apps from templates under `pkg/argocd/templates/apps/` and `pkg/argocd/templates/manifests/`. Apps include cert-manager, cluster-issuers, certificates, envoy-gateway, gateway-config, httproutes, keycloak, postgresql, metallb, metallb-config, opentelemetry-collector, nebari-landingpage, nebari-operator, and the root app.
- The nebari-operator app references the upstream repository (`github.com/nebari-dev/nebari-operator`) via Kustomize; the operator's source code does not live in this repo.

**`pkg/dns`/`pkg/endpoint`/`pkg/git`/`pkg/helm`/`pkg/kubeconfig`/`pkg/status`/`pkg/telemetry`**

- `pkg/endpoint` waits for the Envoy Gateway `Service` to receive an LB hostname or IP, so the CLI can either provision DNS or print manual instructions.
- `pkg/git` clones, commits, and pushes the GitOps repo (including `file://` local paths).
- `pkg/helm` is a thin wrapper around `helm.sh/helm/v3/pkg/action` used by `pkg/argocd`.
- `pkg/status` is the in-process status channel used to surface user-visible progress from library code without violating the "no `slog` in `pkg/`" rule.
- `pkg/telemetry` wires up the OpenTelemetry tracer provider, with exporters selected via `OTEL_EXPORTER` (`console` default, `otlp`, `both`, `none`).

### 2.3 Why This Architecture?

| Design Choice | Rationale |
| ------------- | --------- |
| `Provider` interface as the contract (not "Terraform everywhere") | Honest about reality: only AWS uses OpenTofu; Hetzner uses its own CLI; `local` is a Kind stub. See [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md). |
| terraform-exec for AWS | Programmatic control, JSON output for status streaming, broad ecosystem familiarity. |
| Terraform state in S3 (AWS) | Industry-standard, well-supported tooling, and native lockfile-based locking (no DynamoDB table required). |
| ArgoCD for foundational software | GitOps best practices, declarative dependency management via sync waves, self-healing. |
| Embedded Helm SDK for the ArgoCD install itself | Bootstraps the GitOps controller without requiring an out-of-band Helm CLI. After ArgoCD is up, everything else is GitOps. |
| Out-of-tree Nebari Operator | The operator is its own product with its own release cadence. NIC just deploys it. |
| `InfraSettings` for provider-shaped capabilities | CLI code never switches on provider name. Providers expose capabilities (e.g., `NeedsMetalLB`, `StorageClass`, `SupportsLocalGitOps`) and the rest of the system consumes them. |
| OpenTelemetry in library code, `slog` in CLI | Library code is reusable across CLI commands and (eventually) plugins. CLI is the only layer that emits human-facing logs. |

### 2.4 The Status Channel: pkg → cmd Seam

Library code under `pkg/` is forbidden from calling `slog`. User-visible progress instead flows through the status channel attached to `ctx`:

```
pkg/* (e.g., pkg/tofu, pkg/argocd)
   │
   │  status.Update via status.NewWriter or status.Send
   ▼
ctx-attached chan status.Update
   │
   ▼
cmd/nic/status_handler.go
   │  translates each Update into slog records
   ▼
JSON logs on stderr
```

This decouples library code from any specific logging backend and keeps long-running subprocesses (e.g., `tofu apply -json`) streaming live progress without requiring the producer to enumerate every interesting field.

`pkg/status` and the byte/line-level helpers inside `pkg/tofu` (`streamThroughStatus`, `jsonLineMapper`, `mapStatusLevel`) are intentionally exempt from per-function OpenTelemetry instrumentation: spans at that granularity would dwarf the operations they describe.

---
