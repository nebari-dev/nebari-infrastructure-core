# Architecture Overview

This document provides a comprehensive map of the nebari-infrastructure-core codebase, describing what lives where and how components are organized.

## Table of Contents

- [High-Level Structure](#high-level-structure)
- [Command Line Interface (cmd/nic/)](#command-line-interface-cmdnic)
- [Cluster Provider System (pkg/providers/cluster/)](#cluster-provider-system-pkgproviderscluster)
- [DNS Provider System (pkg/providers/dns/)](#dns-provider-system-pkgprovidersdns)
- [Repository Provider System (pkg/providers/repository/)](#repository-provider-system-pkgprovidersrepository)
- [Configuration System (pkg/config/)](#configuration-system-pkgconfig)
- [Observability (pkg/telemetry/, pkg/status/)](#observability-pkgtelemetry-pkgstatus)
- [Supporting Files](#supporting-files)

## High-Level Structure

```
nebari-infrastructure-core/
├── cmd/nic/              # CLI application entry point
├── pkg/                  # Reusable library packages
│   ├── providers/        # Provider implementations
│   │   ├── cluster/      # Cloud/cluster provider implementations
│   │   ├── dns/          # DNS provider implementations
│   │   └── repository/   # GitOps repository provider implementations
│   ├── git/              # Concrete go-git client (used by pkg/nic)
│   ├── config/           # Configuration parsing and types
│   ├── telemetry/        # OpenTelemetry setup
│   └── status/           # Status reporting system
├── docs/                 # Comprehensive design documentation
├── examples/             # Sample configuration files
├── .github/              # GitHub Actions CI/CD workflows
└── scripts/              # Development and testing scripts
```

## Command Line Interface (cmd/nic/)

**Location:** `cmd/nic/`

**Purpose:** CLI entry point with Cobra command definitions. This is the application layer where logging and user interaction happens.

### Files:

| File | Purpose |
|------|---------|
| `main.go` | Application entry point. Registers providers, initializes telemetry, sets up signal handling, configures logging |
| `deploy.go` | `nic deploy` command implementation. Orchestrates provider deployment with progress tracking |
| `destroy.go` | `nic destroy` command implementation. Handles infrastructure teardown with confirmation prompts |
| `validate.go` | `nic validate` command implementation. Pre-flight config validation |
| `version.go` | `nic version` command implementation. Shows version and registered providers |
| `reconcile.go` | `nic reconcile` command implementation. Reconciles actual vs desired state |

### Key Patterns:

- **Provider Registration:** `main.go` explicitly registers all cloud, DNS, and repository providers using registry pattern
- **Telemetry Setup:** OpenTelemetry initialized in `main.go` based on environment variables
- **Logging:** Structured JSON logging via `slog` - only at this application layer
- **Signal Handling:** Context cancellation for graceful shutdown on SIGINT/SIGTERM
- **Status Display:** Uses `status.StartHandler()` to display progress updates to user

## Cluster Provider System (pkg/providers/cluster/)

**Location:** `pkg/providers/cluster/`

**Purpose:** Cloud provider abstraction and implementations. Each provider manages full lifecycle of cloud infrastructure.

### Core Provider Infrastructure

| File | Purpose |
|------|---------|
| `provider.go` | Defines `Provider` interface with Name(), Validate(), Deploy(), Reconcile(), Destroy(), GetKubeconfig(), Summary() |
| `registry.go` | Thread-safe provider registry with registration and lookup |

### AWS Provider (pkg/providers/cluster/aws/) - **Fully Implemented**

**Location:** `pkg/providers/cluster/aws/`

**Status:** Complete native AWS SDK implementation with full reconciliation logic

#### Core Files

| File | Purpose |
|------|---------|
| `provider.go` | Main provider implementation. Orchestrates Reconcile/Destroy flows |
| `config.go` | AWS-specific configuration types: `Config`, `NodeGroup`, `Taint`, `EFSConfig` |
| `state.go` | State structs returned from discovery: `VPCState`, `ClusterState`, `NodeGroupState`, `EFSState` |
| `client.go` | AWS SDK client initialization (EC2, EKS, IAM, EFS, STS) |
| `interfaces.go` | Interfaces for mocking AWS SDK clients in tests |

#### VPC Management

| File | Purpose |
|------|---------|
| `vpc.go` | VPC creation: subnets, internet gateway, NAT gateways, route tables, security groups |
| `vpc_discovery.go` | Discovers existing VPC and all networking components by NIC tags |
| `vpc_reconcile.go` | Reconciles VPC: validates immutable fields, creates missing components |
| `vpc_delete.go` | Deletes VPC in correct order: endpoints → NAT gateways → IGW → subnets → VPC |

#### EKS Cluster Management

| File | Purpose |
|------|---------|
| `eks.go` | Creates EKS cluster with version, endpoint access, logging configuration |
| `eks_discovery.go` | Discovers existing EKS cluster and OIDC provider by NIC tags |
| `eks_reconcile.go` | Reconciles cluster: validates immutable fields, updates version/logging/endpoint access |
| `eks_delete.go` | Deletes EKS cluster with timeout handling |

#### Node Group Management

| File | Purpose |
|------|---------|
| `nodegroups.go` | Creates node groups with instance types, scaling, labels, taints |
| `nodegroups_discovery.go` | Discovers all node groups for a cluster |
| `nodegroups_reconcile.go` | Reconciles node groups: creates missing, updates mutable fields, deletes orphans |
| `nodegroups_delete.go` | Deletes node groups in parallel with timeout handling |

#### IAM Management

| File | Purpose |
|------|---------|
| `iam.go` | Creates IAM roles and policies for EKS cluster and node groups |
| `iam_delete.go` | Deletes IAM roles and policies with proper dependency ordering |

#### Storage Management

| File | Purpose |
|------|---------|
| `efs.go` | EFS filesystem lifecycle: create, discover, reconcile, delete with mount targets |

#### Supporting Files

| File | Purpose |
|------|---------|
| `tags.go` | NIC tag management: `nic.nebari.dev/cluster-name`, `nic.nebari.dev/managed-by` |
| `kubeconfig.go` | Generates kubeconfig for EKS cluster access |
| `dry_run.go` | Dry-run mode that prints planned changes without executing |
| `nodegroups_test.go` | Unit tests for node group conversion and reconciliation logic |

#### AWS Provider Patterns

**Discovery Pattern:**
```go
// Returns nil if resource doesn't exist (triggers creation)
// Returns *State if found (enables reconciliation)
func (p *Provider) discoverVPC(ctx context.Context, clients *Clients, clusterName string) (*VPCState, error)
```

**Reconciliation Pattern:**
```go
// Compares desired vs actual state
// Errors on immutable field changes
// Updates mutable fields
// Creates missing resources
func (p *Provider) reconcileVPC(ctx context.Context, clients *Clients, cfg *config.NebariConfig, actual *VPCState) (*VPCState, error)
```

**Config Extraction Pattern:**
```go
// Every function extracts provider config at entry via cfg.Cluster.ProviderConfig()
func (p *Provider) someFunction(ctx context.Context, clients *Clients, cfg *config.NebariConfig) error {
    awsCfg, err := extractAWSConfig(ctx, cfg)
    if err != nil {
        return err
    }
    // Use awsCfg.Region, awsCfg.NodeGroups, etc.
}
```

### GCP Provider (pkg/providers/cluster/gcp/) - **Stub**

**Location:** `pkg/providers/cluster/gcp/`

**Status:** Stub implementation - prints config as JSON

| File | Purpose |
|------|---------|
| `provider.go` | Stub provider that prints operations |
| `config.go` | GCP-specific config types: `Config`, `NodeGroup`, `Taint`, `GuestAccelerator` |

### Azure Provider (pkg/providers/cluster/azure/) - **Fully Implemented**

**Location:** `pkg/providers/cluster/azure/`

**Status:** Complete implementation that drives the `nebari-dev/terraform-azurerm-aks-cluster` Terraform module via OpenTofu. Implements all `Provider` methods: `Validate`, `Deploy`, `Destroy`, `GetKubeconfig`, `Summary`, and `InfraSettings`.

**Module sourcing:** Consumes the published `nebari-dev/aks-cluster/azurerm` module from the OpenTofu Registry, pinned by version in `templates/main.tf`.

**State backend:** Uses the `azurerm` backend with a bootstrapped storage account/container for Terraform state (see `state_backend.go`).

**Discovery:** Tag-based cleanup driven by `state.go` (`nic.nebari.dev/cluster-name`, `nic.nebari.dev/managed-by`), mirroring the AWS provider's stateless model.

**Kubeconfig:** Fetched via the Azure SDK (`armcontainerservice`) rather than read from Terraform state, so it works even when the local tfstate is absent.

| File | Purpose |
|------|---------|
| `provider.go` | Provider implementation: orchestrates Validate/Deploy/Destroy/Summary/InfraSettings over OpenTofu |
| `config.go` | Azure-specific config types: `Config`, `NodeGroup`, `Taint` |
| `state.go` | Tag-based discovery and orphan cleanup helpers |
| `state_backend.go` | Bootstraps the `azurerm` Terraform backend (resource group, storage account, container) |
| `tofu.go` | OpenTofu invocation: init/plan/apply/destroy against the external module |
| `kubeconfig.go` | Retrieves cluster admin kubeconfig via `armcontainerservice` |
| `cleanup.go` | Post-destroy resource sweep (tag-based) |
| `interfaces.go` | Azure SDK client interfaces for mocking |
| `templates/` | Rendered Terraform root module that wraps the external module |
| `examples/azure-config.yaml` | Working config (at repo root `examples/`) |

### Local Provider (pkg/providers/cluster/local/)

**Location:** `pkg/providers/cluster/local/`

**Status:** NIC-managed kind (Kubernetes-in-Docker) cluster for local development

**Purpose:** Creates and tears down a local kind cluster as part of `nic deploy`/`nic destroy` (cluster named after `project_name`). Installs MetalLB so the gateway gets a LoadBalancer IP, deriving the address pool from the kind Docker network so it is routable. To deploy onto a pre-existing cluster instead, use the `existing` provider.

| File | Purpose |
|------|---------|
| `provider.go` | Provider implementation: create/destroy the kind cluster, fetch kubeconfig, derive the MetalLB pool for `InfraSettings` |
| `kind.go` | kind cluster lifecycle via `sigs.k8s.io/kind` (create/delete/list, gitops mount, address-pool derivation) |
| `config.go` | Local-specific config types: `Config`, `KindConfig`, `KindMount`, `MetalLBConfig` |

## DNS Provider System (pkg/providers/dns/)

**Location:** `pkg/providers/dns/`

**Purpose:** DNS provider abstraction for managing DNS records. Separate from cloud providers to allow mixing (e.g., AWS infrastructure + Cloudflare DNS).

### Core DNS Infrastructure

| File | Purpose |
|------|---------|
| `provider.go` | Defines stateless `Provider` interface: `ProvisionRecords()`, `DestroyRecords()` |
| `registry.go` | Thread-safe DNS provider registry |

### Cloudflare Provider (pkg/providers/dns/cloudflare/)

**Location:** `pkg/providers/dns/cloudflare/`

**Status:** Implemented using `cloudflare-go/v4` SDK

| File | Purpose |
|------|---------|
| `provider.go` | ProvisionRecords/DestroyRecords with idempotent ensure-record logic |
| `client.go` | `CloudflareClient` interface for testability |
| `sdk_client.go` | Real cloudflare-go v4 SDK adapter |
| `config.go` | Cloudflare-specific config (zone_name) |
| `provider_test.go` | 18 table-driven tests with mock client |

**Environment Variables:**
- `CLOUDFLARE_API_TOKEN` - Cloudflare API token (never in config files)

**Known limitation:** Changing the `domain` in config and redeploying creates new records but does not clean up the old domain's records. See [DNS Provider Architecture](docs/design-doc/implementation/09-dns-provider-architecture.md#orphaned-records-on-domain-change).

## Repository Provider System (pkg/providers/repository/)

**Location:** `pkg/providers/repository/`

**Purpose:** GitOps repository provider abstraction. Providers resolve (or create) the repository that ArgoCD syncs foundational software from, and return a typed `Source` describing how to reach it. The `repository:` config block is required and follows the same provider-name-as-key pattern as `cluster:` and `dns:`. See [ADR-0007](docs/adr/0007-repository-provider-abstraction.md) for the design rationale.

### Core Repository Infrastructure

| File | Purpose |
|------|---------|
| `provider.go` | Defines the `Provider` interface (`Name()`, `Validate()`, `Provision()`) and the sealed `Source` (`LocalSource`, `RemoteSource`) and `Auth` (`TokenAuth`, `SSHKeyAuth`) contracts |
| `provider_test.go` | Source accessor contracts and the `ArgoCDAuth()` read/push fallback |

The package imports only `pkg/config`: it is free of go-git and Kubernetes so out-of-tree providers stay lightweight. Credentials inside a `Source` are already resolved from their environment variables and exist only in memory; they are never serialized.

### Existing Provider (pkg/providers/repository/existing/)

**Location:** `pkg/providers/repository/existing/`

Resolves a pre-existing remote repository (SSH or HTTPS) into a `RemoteSource`. The repository must already exist; this provider does not create one.

| File | Purpose |
|------|---------|
| `provider.go` | `Provision` resolves push (and optional ArgoCD read) credentials from env vars into a `RemoteSource` |
| `config.go` | `Config` with `url`, `branch`, `path`, and tagged-union `auth`/`argocd_auth` blocks validated as exactly-one-of token/ssh |
| `config_test.go`, `provider_test.go` | Validation table tests, env resolution, and Provision paths |

**Environment Variables:** names are user-configured (`auth: token: {env: GIT_TOKEN}` or `auth: ssh: {env: GIT_SSH_PRIVATE_KEY}`); the config carries only the names, never the secrets.

### Local Provider (pkg/providers/repository/local/)

**Location:** `pkg/providers/repository/local/`

Provisions a directory on disk as a `LocalSource` - the zero-dependency, no-network option for local/dev clusters. NIC commits to it in place and ArgoCD's repo-server reads it via a hostPath mount, so it requires a cluster provider with `SupportsLocalGitOps` (e.g. kind); `pkg/nic` rejects incompatible pairings before the GitOps bootstrap runs.

| File | Purpose |
|------|---------|
| `provider.go` | `Provision` creates the directory (default: per-project dir under the OS temp dir) and returns a `LocalSource` |
| `config.go` | `Config` with optional absolute `path` and `branch` |
| `provider_test.go` | Default-path derivation, directory creation, and validation |

### Git Client (pkg/git/)

**Location:** `pkg/git/`

Not a provider: a concrete go-git-backed `Client` that `pkg/nic` drives after type-switching the `Source`. Acquisition is explicit - `Init(ctx, dir)` opens or initializes a local repository in place, while `ValidateAuth`/`Clone` authenticate and clone a remote into a managed temp dir - and `Commit`/`Push` are separate so local repositories never push. go-git stays sealed inside this package; `git.Auth` values are built via `NewAuthToken`/`NewSSHKeyAuth` and the zero value means anonymous.

## Configuration System (pkg/config/)

**Location:** `pkg/config/`

**Purpose:** Configuration parsing with lenient validation and provider config marshaling.

| File | Purpose |
|------|---------|
| `config.go` | `NebariConfig` struct with global fields and `any` provider config fields |
| `parser.go` | YAML parsing, validation, `UnmarshalProviderConfig()` helper for type conversion |

### Configuration Architecture

**Central Config (pkg/config/config.go):**
```go
type NebariConfig struct {
    ProjectName string            `yaml:"project_name"`         // Required: cluster name
    Domain      string            `yaml:"domain,omitempty"`     // Optional: domain for ingress
    Cluster     *ClusterConfig    `yaml:"cluster,omitempty"`    // Nested: cluster.aws.region
    DNS         *DNSConfig        `yaml:"dns,omitempty"`        // Nested: dns.cloudflare.zone_name
    Repository  *RepositoryConfig `yaml:"repository,omitempty"` // Required: repository.existing.url

    // Runtime options (not from YAML)
    DryRun  bool          `yaml:"-"`
    Force   bool          `yaml:"-"`
    Timeout time.Duration `yaml:"-"`
}

// ClusterConfig, DNSConfig, and RepositoryConfig share the nested-key pattern.
// Access via: cfg.Cluster.ProviderName(), cfg.Cluster.ProviderConfig()
type ClusterConfig struct {
    Providers map[string]any `yaml:",inline"`
}
```

**Provider-Specific Configs:**
- AWS: `pkg/providers/cluster/aws/config.go` - `Config`, `NodeGroup`, `Taint`, `EFSConfig`
- GCP: `pkg/providers/cluster/gcp/config.go` - `Config`, `NodeGroup`, `Taint`, `GuestAccelerator`
- Azure: `pkg/providers/cluster/azure/config.go` - `Config`, `NodeGroup`, `Taint`
- Local: `pkg/providers/cluster/local/config.go` - `Config`

**Type Conversion:**
```go
// Converts any to concrete provider type via YAML round-trip
func UnmarshalProviderConfig(ctx context.Context, providerConfig any, target any) error
```

## Observability (pkg/telemetry/, pkg/status/)

### Telemetry (pkg/telemetry/)

**Location:** `pkg/telemetry/`

**Purpose:** OpenTelemetry tracing setup and lifecycle management

| File | Purpose |
|------|---------|
| `telemetry.go` | Initializes OTLP, console, or dual exporters based on env vars |

**Environment Variables:**
- `OTEL_EXPORTER`: "console" (default), "otlp", "both", "none"
- `OTEL_ENDPOINT`: OTLP endpoint (default: "localhost:4317")

**Instrumentation Pattern:**
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

### Status Reporting (pkg/status/)

**Location:** `pkg/status/`

**Purpose:** Real-time progress updates sent from providers to CLI

| File | Purpose |
|------|---------|
| `status.go` | Context-based status channel for sending updates from pkg/ to cmd/ |

**Usage Pattern:**
```go
// In provider code (pkg/)
status.Send(ctx, status.NewUpdate(status.LevelProgress, "Creating VPC").
    WithResource("vpc").
    WithAction("creating"))

// In CLI code (cmd/)
ctx, cleanup := status.StartHandler(ctx, statusLogHandler())
defer cleanup()
```

## Supporting Files

### Documentation

| Location | Purpose |
|----------|---------|
| `README.md` | Project overview, quick start, commands, configuration examples |
| `ARCHITECTURE.md` | This file - maps codebase structure and file locations |
| `docs/` | Detailed design documentation organized by topic |
| `docs/architecture/` | Architectural decision records |
| `docs/implementation/` | Implementation specifications |
| `docs/operations/` | Testing and deployment procedures |

### Configuration Examples

| Location | Purpose |
|----------|---------|
| `examples/aws-config.yaml` | Sample AWS configuration with all options |
| `examples/aws-config-with-dns.yaml` | AWS config with Cloudflare DNS |
| `examples/gcp-config.yaml` | Sample GCP configuration |
| `examples/azure-config.yaml` | Sample Azure configuration |
| `examples/local-config.yaml` | Sample local kind configuration |

### Development Tools

| Location | Purpose |
|----------|---------|
| `Makefile` | Build, test, lint, and release targets |
| `go.mod`, `go.sum` | Go module dependencies |
| `.env.example` | Template for local environment variables (secrets) |
| `.gitignore` | Excludes .env, binaries, coverage reports |
| `.pre-commit-config.yaml` | Pre-commit hooks: go fmt, vet, golangci-lint, tests |

### CI/CD

| Location | Purpose |
|----------|---------|
| `.github/workflows/test.yml` | Runs tests on PRs and pushes |
| `.github/workflows/lint.yml` | Runs golangci-lint on PRs |
| `.github/workflows/release.yml` | Creates GitHub releases with binaries |

### Scripts

| Location | Purpose |
|----------|---------|
| `scripts/build.sh` | Multi-platform build script |
| `scripts/install-tools.sh` | Installs golangci-lint and other dev tools |

## Key Architectural Principles

### 1. Stateless Operation
- No state files on disk
- Every operation queries cloud APIs for actual state
- Tag-based resource discovery: `nic.nebari.dev/cluster-name`, `nic.nebari.dev/managed-by`

### 2. Separation of Concerns
- **cmd/nic/** - Application layer (logging, user interaction, signal handling)
- **pkg/providers/cluster/** - Provider implementations (no logging, only telemetry spans)
- **pkg/config/** - Configuration parsing (provider-agnostic)
- **pkg/telemetry/** - Observability (OpenTelemetry)
- **pkg/status/** - Progress reporting (channel-based)

### 3. Explicit Dependencies
- No blank imports (`import _ "provider/aws"`)
- No init() magic
- Providers explicitly registered in `cmd/nic/main.go`
- Makes dependencies testable and visible

### 4. Provider Configuration Isolation
- Each provider owns its configuration types in `pkg/providers/cluster/<provider>/config.go`
- Central config uses nested `cluster: <provider>:` format (same pattern as `dns: <provider>:`)
- Provider name via `cfg.Cluster.ProviderName()`, raw config via `cfg.Cluster.ProviderConfig()`
- Type conversion via `config.UnmarshalProviderConfig()` helper
- Providers extract their config at function entry with `extractConfig()` pattern

### 5. Reconciliation-Based
- Discover actual state from cloud APIs
- Compare with desired state from config
- Reconcile differences:
  - **Immutable fields changed** → Error (requires destroy/recreate)
  - **Mutable fields changed** → Update via API
  - **Resources missing** → Create
  - **Resources orphaned** → Delete

### 6. Observable by Default
- Every function wrapped in OpenTelemetry span
- Span attributes for debugging: resource IDs, regions, versions
- Error recording in spans
- Structured logging only at application layer

## File Organization Patterns

### Provider Package Structure
```
pkg/providers/cluster/<provider>/
├── provider.go               # Provider interface implementation
├── config.go                 # Provider-specific configuration types
├── <resource>.go             # Resource creation logic
├── <resource>_discovery.go   # Resource discovery from cloud APIs
├── <resource>_reconcile.go   # Resource reconciliation logic
├── <resource>_delete.go      # Resource deletion logic
├── state.go                  # State structs returned from discovery
└── *_test.go                 # Unit tests (table-driven)
```

### Command Package Structure
```
cmd/nic/
├── main.go              # Entry point, provider registration, telemetry
├── <command>.go         # Each command in separate file
└── shared.go            # Shared helpers (status handler, etc.)
```

### Test Organization
- Unit tests alongside implementation files: `*_test.go`
- Table-driven tests preferred
- Mock interfaces defined in `interfaces.go` files
- Integration tests (future): separate `integration/` directory

## Navigation Tips

### Finding Code by Functionality

**"Where is VPC creation?"**
- Creation: `pkg/providers/cluster/aws/vpc.go`
- Discovery: `pkg/providers/cluster/aws/vpc_discovery.go`
- Reconciliation: `pkg/providers/cluster/aws/vpc_reconcile.go`
- Deletion: `pkg/providers/cluster/aws/vpc_delete.go`

**"Where is configuration parsed?"**
- Parsing: `pkg/config/parser.go`
- Config types (global): `pkg/config/config.go`
- Config types (AWS): `pkg/providers/cluster/aws/config.go`

**"Where are commands defined?"**
- CLI framework: `cmd/nic/main.go`
- Deploy command: `cmd/nic/deploy.go`
- Destroy command: `cmd/nic/destroy.go`

**"Where is telemetry set up?"**
- Initialization: `pkg/telemetry/telemetry.go`
- Usage: Every function in `pkg/` starts with `tracer.Start()`

**"Where are providers registered?"**
- Registration: `cmd/nic/main.go`
- Registry implementation: `pkg/registry/registry.go`

### Finding Documentation

**"How does stateless operation work?"**
- `docs/architecture/06-stateless-operation.md`

**"What are the design decisions?"**
- `docs/architecture/` - Architectural Decision Records (ADRs)

**"How do I test?"**
- `docs/operations/` - Testing procedures

## Version Information

- **Go Version**: 1.26+
- **AWS SDK**: github.com/aws/aws-sdk-go-v2
- **CLI Framework**: github.com/spf13/cobra
- **Telemetry**: go.opentelemetry.io/otel
- **YAML Parsing**: github.com/goccy/go-yaml

---

For project overview and usage, see [README.md](README.md).
For detailed design documentation, see [docs/](docs/).
