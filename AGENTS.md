# AGENTS.md

Guidance for contributors and AI coding agents working in this repository.

This file follows the [AGENTS.md](https://agents.md) convention and is read by Claude Code, Codex, Cursor, Aider, Jules, and other agent tooling, as well as by humans.

## Project Overview

**Nebari Infrastructure Core (NIC)** is a standalone Go CLI that provisions Kubernetes clusters for Nebari and bootstraps foundational software on top of them.

NIC is organized around pluggable **providers**. A provider is a small Go interface with one implementation per backend; each provider is free to use whatever tool fits the backend best (OpenTofu, a vendor CLI, a Kubernetes-native installer, a REST API). The CLI never branches on provider names - it depends only on provider interfaces.

The codebase currently has two provider categories in tree:

- **Cluster providers** (`pkg/providers/cluster/`) - bring up the Kubernetes cluster
- **DNS providers** (`pkg/providers/dns/`) - manage DNS records pointing at the cluster's load balancer

More categories (certificate issuers, git hosting, software installers) are planned. See **[ADR-0004: Out-of-Tree Provider Plugin Architecture](docs/adr/0004-out-of-tree-provider-plugins.md)** for the direction this is heading.

### Cluster Providers

| Provider | Backing tool | Status |
| --- | --- | --- |
| `aws` | OpenTofu, using the [`terraform-aws-eks-cluster`](https://github.com/nebari-dev/terraform-aws-eks-cluster) module with `.tf` templates embedded under `pkg/providers/cluster/aws/templates/` and driven via `terraform-exec` | Primary, in active use |
| `hetzner` | [`hetzner-k3s`](https://github.com/vitobotta/hetzner-k3s) binary; NIC downloads and caches a pinned release with checksum verification | Active development |
| `existing` | Bring-your-own kubeconfig context. Validates an existing context; performs no provisioning | Working |
| `local` | Kind. `nic deploy` creates the Kind cluster (reusing it if one already exists) and bootstraps it; `nic destroy` deletes it | Working |
| `azure` | OpenTofu, using the [`terraform-azurerm-aks-cluster`](https://github.com/nebari-dev/terraform-azurerm-aks-cluster) module with `.tf` templates embedded under `pkg/providers/cluster/azure/templates/` | Implemented end-to-end |
| `gcp` | Stub implementation only | Not implemented |

### DNS Providers

| Provider | Backing tool | Status |
| --- | --- | --- |
| `cloudflare` | Cloudflare API (`pkg/providers/dns/cloudflare/`) | Working |

### Core Architecture Principles

1. **Provider abstractions first.** Each pluggable category lives behind a small Go interface; CLI code depends only on interfaces.
2. **Each provider picks the right tool.** No assumption that everything goes through OpenTofu.
3. **OpenTelemetry instrumented.** Every Go function in `pkg/` (with documented exemptions below) is wrapped in trace spans.
4. **Structured logging at the app layer only.** `slog` JSON logs live in `cmd/nic`; `pkg/` libraries surface progress via the status channel, never via direct logging.
5. **GitOps for software.** ArgoCD manages everything that runs on the cluster after the cluster itself is provisioned.

## Common Development Commands

### Building

```bash
make build                    # go build -o nic ./cmd/nic
make build-all                # All platforms (linux/darwin/windows x amd64/arm64)
make install                  # go install to $GOPATH/bin
```

### Testing

```bash
make test                     # alias for test-unit
make test-unit                # go test -v -short ./...
make test-integration         # testcontainers-based, requires Docker
make test-all                 # unit + integration
make test-coverage            # coverage report
make test-race                # race detector
go test ./pkg/providers/cluster/aws -v # single package
```

### Code Quality

```bash
make fmt                      # gofmt -s -w
make vet                      # go vet
make lint                     # golangci-lint run
make pre-commit               # run pre-commit checks
```

### Local Kind Cluster

```bash
make build                                     # build the nic binary
./nic deploy -f examples/local-config.yaml     # create the Kind cluster + deploy Nebari
./nic destroy -f examples/local-config.yaml    # tear the Kind cluster down
```

See `docs/local-kind-development.md` for the full workflow.

### Running NIC

NIC resolves its config file in this order: `--file/-f` flag → `NIC_CONFIG_PATH` env var → `./config.yaml` (auto-discovery). See `cmd/nic/config_discovery.go`.

```bash
./nic version
./nic validate -f config.yaml
./nic deploy -f config.yaml
./nic destroy -f config.yaml

# With OpenTelemetry tracing
OTEL_EXPORTER=console ./nic deploy -f config.yaml
OTEL_EXPORTER=otlp OTEL_ENDPOINT=localhost:4317 ./nic deploy -f config.yaml
```

## High-Level Architecture

### Component Structure

```
cmd/nic/                # CLI entry point - thin cobra commands over pkg/nic
  ├── main.go           # CLI setup, telemetry init, .env loading via godotenv
  ├── deploy.go         # Deploy command
  ├── destroy.go        # Destroy command
  ├── validate.go       # Validate command
  ├── kubeconfig.go     # Kubeconfig command
  ├── version.go        # Version command
  └── config_discovery.go # Resolve config file path

pkg/
  ├── nic/              # Orchestration + programmatic entrypoint; cmd/nic is a thin wrapper over this
  │   ├── client.go     # Client construction + default provider registry
  │   ├── deploy.go     # Deploy orchestration (provider -> argocd -> dns -> endpoint)
  │   ├── destroy.go    # Destroy orchestration
  │   ├── validate.go   # Validate orchestration
  │   ├── kubeconfig.go # Kubeconfig retrieval
  │   └── status.go     # status.Update -> slog translation (StartSlogHandler)
  ├── provider/         # Cluster provider interface + implementations
  │   ├── provider.go   # Provider interface, InfraSettings, DeployOptions
  │   ├── aws/          # OpenTofu-backed; templates/ holds embedded .tf files
  │   ├── azure/        # OpenTofu-backed (terraform-azurerm-aks-cluster); embedded templates/
  │   ├── hetzner/      # hetzner-k3s-backed; downloads + caches the binary
  │   ├── existing/     # Bring-your-own kubeconfig
  │   ├── local/        # Kubeconfig validator for Kind clusters
  │   └── gcp/          # Stub
  ├── dnsprovider/      # DNS provider interface + implementations
  │   ├── provider.go   # DNSProvider interface
  │   └── cloudflare/   # Cloudflare API implementation
  ├── registry/         # Unified registry holding both cluster and DNS providers
  ├── storage/          # Cluster-agnostic storage installers
  │   └── longhorn/     # Helm-based Longhorn install/uninstall, shared across providers
  ├── tofu/             # terraform-exec wrapper (used by the AWS and Azure cluster providers)
  ├── argocd/           # ArgoCD bootstrap and foundational-apps templating
  ├── config/           # YAML config parsing/validation
  ├── git/              # Git config types and client used by ArgoCD GitOps repo
  ├── helm/             # Helm helpers
  ├── kubeconfig/       # Kubeconfig file helpers
  ├── endpoint/         # Post-deploy LB endpoint discovery + DNS hints
  ├── status/           # In-process status channel (pkg -> cmd seam)
  └── telemetry/        # OpenTelemetry tracer setup
```

### The Cluster `Provider` Interface

`pkg/providers/cluster/provider.go` is the single seam between `cmd/nic` and any cluster-specific code:

```go
type Provider interface {
    Name() string
    Validate(ctx, projectName, *config.ClusterConfig) error
    Deploy(ctx, projectName, *config.ClusterConfig, DeployOptions) error
    Destroy(ctx, projectName, *config.ClusterConfig, DestroyOptions) error
    GetKubeconfig(ctx, projectName, *config.ClusterConfig) ([]byte, error)
    Summary(*config.ClusterConfig) map[string]string
    InfraSettings(*config.ClusterConfig) InfraSettings
}
```

`InfraSettings` describes Kubernetes-level capabilities the rest of NIC needs to know about. Current fields: `StorageClass`, `NeedsMetalLB`, `LoadBalancerAnnotations`, `MetalLBAddressPool`, `KeycloakBasePath`, `HTTPSPort`, `EFSStorageClass`, `SupportsLocalGitOps`.

Cluster-shaped branching anywhere outside the cluster provider package itself **must** go through `InfraSettings` - never `cfg.Cluster.ProviderName() == "..."` switches in CLI or library code. The same pattern is followed by `dnsprovider.DNSProvider`, and is intended to scale to certificate, git hosting, and installer categories.

### The `DNSProvider` Interface

`pkg/providers/dns/provider.go`:

```go
type DNSProvider interface {
    Name() string
    ProvisionRecords(ctx, domain string, dnsConfig map[string]any, lbEndpoint string) error
    DestroyRecords(ctx, domain string, dnsConfig map[string]any) error
}
```

DNS providers are stateless - domain and config are passed to each call. `cloudflare` is the only implementation today.

### The Provider Registry

`pkg/registry/registry.go` holds both provider categories behind one thread-safe struct:

```go
type Registry struct {
    ClusterProviders *ProviderList[provider.Provider]
    DNSProviders     *ProviderList[dnsprovider.DNSProvider]
}
```

CLI commands resolve providers through the registry; config validation pulls the lists of valid names from it. New categories will be added the same way.

### Execution Flow (AWS example)

```
nic deploy -> nic.Client.Deploy (pkg/nic) orchestrates:
  -> cluster provider aws.Deploy
                -> pkg/tofu.Setup(embeddedTemplates, tfvars)
                -> tfexec.Init/Plan/Apply
                -> outputs (kubeconfig, VPC, EFS, ...)
                -> post-apply addons: AWS Load Balancer Controller,
                   Cluster Autoscaler (if enabled), Longhorn (if enabled)
  -> pkg/argocd.Bootstrap(kubeconfig, InfraSettings)
  -> (if DNS configured) dnsprovider.ProvisionRecords
  -> pkg/endpoint discovers LB and prints DNS records
```

### Execution Flow (Hetzner example)

```
nic deploy -> nic.Client.Deploy (pkg/nic) orchestrates:
  -> cluster provider hetzner.Deploy
                -> ensure hetzner-k3s binary in cache (download + SHA256 verify)
                -> generate cluster.yaml
                -> exec hetzner-k3s create -c cluster.yaml
                -> kubeconfig written
  -> pkg/argocd.Bootstrap(kubeconfig, InfraSettings{NeedsMetalLB: true, LoadBalancerAnnotations: ...})
  -> dnsprovider.ProvisionRecords
  -> pkg/endpoint discovers LB and prints DNS records
```

### Configuration Architecture

```go
// pkg/config/config.go
type NebariConfig struct {
    ProjectName   string             `yaml:"project_name"`
    Domain        string             `yaml:"domain,omitempty"`
    Cluster       *ClusterConfig     `yaml:"cluster,omitempty"`
    DNS           *DNSConfig         `yaml:"dns,omitempty"`
    GitRepository *git.Config        `yaml:"git_repository,omitempty"`
    Certificate   *CertificateConfig `yaml:"certificate,omitempty"`
}
```

Provider blocks nest under their category with the provider name as the map key:

```yaml
cluster:
  aws:
    region: us-west-2
    kubernetes_version: "1.34"
    # ...

dns:
  cloudflare:
    zone_name: example.com
```

The `config` package does **not** know about provider-specific fields. Each provider unmarshals its own slice of `ProviderConfig` into a typed struct (e.g., `pkg/providers/cluster/aws/config.go`, `pkg/providers/cluster/hetzner/config.go`).

See `examples/` for full configs: `aws-config.yaml`, `aws-config-with-dns.yaml`, `azure-config.yaml`, `hetzner-config.yaml`, `local-config.yaml`, `existing-config.yaml`.

### State Management

State backends are provider-defined.

- **AWS:** standard Terraform remote state. The AWS provider's templates set up the backend; see `pkg/providers/cluster/aws/state.go` and `templates/backend.tf`.
- **Azure:** Terraform remote state in an Azure Storage Account backend; see `pkg/providers/cluster/azure/state.go`, `state_backend.go`, and `templates/backend.tf`.
- **Hetzner:** `hetzner-k3s` writes its own state file plus a kubeconfig.
- **Existing / local:** no state of their own - they consume an external kubeconfig.

### Secrets Management

**Critical:** Secrets are NEVER stored in `config.yaml`.

- Create `.env` for local development (gitignored).
- Copy from `.env.example` template.
- Automatically loaded by `godotenv` in `cmd/nic/main.go`.

```bash
AWS_ACCESS_KEY_ID=your_key_here
AWS_SECRET_ACCESS_KEY=your_secret_here
HCLOUD_TOKEN=your_hetzner_token
CLOUDFLARE_API_TOKEN=your_token_here
GIT_SSH_PRIVATE_KEY=...
```

### OpenTelemetry Instrumentation

**All new Go functions in `pkg/` must follow this pattern:**

```go
func SomeFunction(ctx context.Context, ...) error {
    tracer := otel.Tracer("nebari-infrastructure-core")
    ctx, span := tracer.Start(ctx, "package.FunctionName")
    defer span.End()

    span.SetAttributes(attribute.String("key", "value"))

    if err != nil {
        span.RecordError(err)
        return err
    }
    return nil
}
```

**Environment variables (`pkg/telemetry/telemetry.go`):**
- `OTEL_EXPORTER`: `none` (default), `console`, `otlp`, or `both`
- `OTEL_ENDPOINT`: OTLP endpoint (default: `localhost:4317`)

**Exemptions:**
- `pkg/status` is the in-process status channel. Per-line writers and helpers there are intentionally not span-instrumented; spans at that granularity would dwarf the operations they describe.
- Inside `pkg/tofu`, the byte/line-level helpers (`streamThroughStatus`, `jsonLineMapper`, `mapStatusLevel`, the `status.Writer` methods) are similarly exempt. Operation-granularity wrapper methods on `TerraformExecutor` (`Init`, `Plan`, `Apply`, `Destroy`, `Output`) should still be span-instrumented; this is tracked as outstanding work.
- New code in any other `pkg/` package must be instrumented as described above.

### Logging Convention

**Application layer only:**
- Use `slog.Info()` / `slog.Error()` in `cmd/nic/` commands.
- Do **not** log in `pkg/` library code; emit spans and status updates instead.

**Inside a `RunE`, do not `slog.Error` an error you also return**. Record it on the span (`span.RecordError(err)`) and return it (wrapped where useful); `main()` logs returned errors exactly once, so logging *and* returning duplicates the report (see #326).

**The status channel is the seam.** `pkg/` code surfaces user-visible progress by sending `status.Update`s through the channel attached to ctx (see `pkg/status`). Translation of updates into slog records lives in `pkg/nic/status.go` (`SlogHandler` / `StartSlogHandler`); `cmd/nic` wires it up via `nic.StartSlogHandler` and remains the only layer that emits logs. When wrapping a subprocess that emits structured output (e.g. `tofu -json`, `hetzner-k3s`), use `status.NewWriter` with a `LineMapper` that produces one `Update` per line; the full structured event should ride through as `Update.Metadata[status.MetadataKeyPayload]` so handlers can decode any sub-field without the producer enumerating them.

## Key Development Patterns

### Adding a New Cluster Provider

1. Create `pkg/providers/cluster/<name>/`.
2. Implement the `Provider` interface (`Name`, `Validate`, `Deploy`, `Destroy`, `GetKubeconfig`, `Summary`, `InfraSettings`).
3. Choose the right backing tool. Embed templates with `//go:embed` if you need files (see `pkg/providers/cluster/aws/templates/`).
4. Register the provider with the `registry.Registry` built in `pkg/nic/registry.go`.
5. Populate `InfraSettings` so `pkg/argocd` and the CLI can configure software without knowing about your provider. Add new fields to `InfraSettings` (not provider-name switches) if you need to express a new capability.
6. Add an `examples/<name>-config.yaml`.
7. Cover the provider with table-driven unit tests; integration tests gated on credentials.

### Adding a New DNS Provider

1. Create `pkg/providers/dns/<name>/`.
2. Implement the `DNSProvider` interface (`Name`, `ProvisionRecords`, `DestroyRecords`).
3. Register with the `registry.Registry`.
4. Add to `examples/` (e.g., update `aws-config-with-dns.yaml`).

### Adding a New Configuration Field

1. Decide whether the field is generic (top-level on `NebariConfig`) or provider-specific (decoded by the provider from `ProviderConfig()`).
2. If generic, add it to `pkg/config/config.go` with a `yaml` tag and validation.
3. If provider-specific, decode it inside the provider's `config.go` (use `config.UnmarshalProviderConfig`).
4. Plumb it through to the backing tool.
5. Update example configs in `examples/`.
6. Add tests.

### Error Handling Convention

```go
if err != nil {
    span.RecordError(err)
    // slog only at cmd/nic layer
    return fmt.Errorf("descriptive context: %w", err)
}
```

## Testing Strategy

### Unit Tests
- Use **table-driven tests** for Go unit tests.
- Functions should accept interfaces and return concrete types where possible (improves mockability).
- **Never disable tests to get them passing** - fix the underlying issue.

### Integration Tests
- `make test-integration` runs the AWS provider's integration tests (build tag `integration`) using [testcontainers](https://golang.testcontainers.org/); requires Docker.

### Provider Tests (real cloud)
- Deploy against real cloud APIs only on significant changes. Expensive - run sparingly.

## Important Conventions

### Abstraction Boundaries

**Critical:** Code must respect package boundaries.

- **CLI commands (`cmd/nic/`)** depend only on provider interfaces (`provider.Provider`, `dnsprovider.DNSProvider`), never on specific implementations.
- **Provider implementations** do not import each other - they are independent.
- **Config package** does not know about provider-specific types - it uses `map[string]any` with per-provider runtime unmarshaling.
- Provider-specific types belong in their respective packages (e.g., `pkg/providers/cluster/aws/config.go`).
- **Cluster-shaped capabilities flow through `InfraSettings`.** When the CLI or `pkg/argocd` needs to branch on a cluster-provider-specific capability (MetalLB requirement, Keycloak context path, HTTPS port, local GitOps support, etc.), add a field to `InfraSettings` and set it in each provider's `InfraSettings()` method. Do not introduce `cfg.Cluster.ProviderName() == "..."` switches in CLI or library code - those become architectural debt that make adding a new provider require changes across the codebase. Existing examples: `NeedsMetalLB`, `StorageClass`, `KeycloakBasePath`, `HTTPSPort`, `EFSStorageClass`, `LoadBalancerAnnotations`, `SupportsLocalGitOps`.

**Why this matters:**
- Adding a new provider should not require changes to CLI commands or the config package.
- Follows the Open/Closed Principle: open for extension, closed for modification.
- Makes testing easier - the CLI can be tested with mock providers.
- Prevents circular dependencies.
- The same pattern scales to DNS, certificate, git, and installer categories (see ADR-0004).

**Code smell:** A switch statement on cluster provider names in CLI code means you've crossed the abstraction boundary. Add a method to the interface or a field to `InfraSettings` instead.

References:
- [Clean Architecture - Uncle Bob](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [Package Oriented Design - William Kennedy](https://www.ardanlabs.com/blog/2017/02/package-oriented-design.html)

### Thread Safety
- Infrastructure operations are serialized per cluster provider (Terraform handles locking; `hetzner-k3s` is single-shot).
- NIC itself is single-threaded for infrastructure operations.

### Context Propagation
- Always pass `ctx` through the call chain.
- Use `ctx` for OpenTelemetry span creation.
- Respect `ctx` cancellation signals.

### Idempotency
- The AWS provider relies on OpenTofu state comparison; running `nic deploy` twice on AWS with the same config shows no changes in the plan.
- Other providers handle idempotency per their backing tool's semantics.

## Documentation

- **`docs/README.md`** - Documentation overview and navigation
- **`docs/adr/`** - Architectural Decision Records. Notable:
  - **ADR-0001** - Git provider for GitOps bootstrap
  - **ADR-0002** - Longhorn for distributed block storage on AWS
  - **ADR-0003** - Software pack codegen
  - **ADR-0004** - Out-of-tree provider plugin architecture (Proposed)
- **`docs/design-doc/`** - Living design docs (architecture / implementation / operations / appendix)
- **`docs/cli-reference.md`** - CLI command reference
- **`docs/local-kind-development.md`** - Local Kind workflow
- **`docs/plans/`** - In-flight implementation plans

## Dependencies

Core libraries (see `go.mod`):
- `github.com/spf13/cobra` - CLI framework
- `github.com/hashicorp/terraform-exec` - OpenTofu/Terraform execution (AWS cluster provider)
- `go.opentelemetry.io/otel` - Distributed tracing
- `gopkg.in/yaml.v3` - YAML parsing
- `github.com/joho/godotenv` - .env file parsing
- `k8s.io/client-go` - Kubernetes client

Runtime dependencies (per cluster provider):
- **AWS:** OpenTofu binary in `PATH` (NIC will also download into a cache if needed)
- **Hetzner:** none - NIC downloads and caches a pinned `hetzner-k3s` release
- **Local:** a container runtime (Docker or Podman). NIC embeds the kind Go library, so the `kind` CLI is not required. Run `nic deploy -f examples/local-config.yaml` and the local provider creates the Kind cluster
- **Existing:** an existing kubeconfig with a working context

## Pre-Commit Checklist

Run before every commit:

1. **Unit tests:** `make test-unit` (or `go test -v -short ./...`)
2. **Linting:** `make lint`
3. **Formatting:** `make fmt`
4. **Vet:** `make vet`
5. **OpenTelemetry instrumentation** in new `pkg/` functions (see exemptions above)
6. **Logging convention:** `slog` usage only in `cmd/nic`, not in `pkg/`
7. **Abstraction boundary:** no provider-name switches outside `pkg/providers/cluster/` or `pkg/providers/dns/`

Integration tests (`make test-integration`) should pass before merging changes that touch provider code.
