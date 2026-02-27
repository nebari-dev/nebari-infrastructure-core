# Architecture Overview

This document provides a comprehensive map of the nebari-infrastructure-core codebase, describing what lives where and how components are organized.

## Table of Contents

- [High-Level Structure](#high-level-structure)
- [Command Line Interface (cmd/nic/)](#command-line-interface-cmdnic)
- [Provider System (pkg/provider/)](#provider-system-pkgprovider)
- [DNS Provider System (pkg/dnsprovider/)](#dns-provider-system-pkgdnsprovider)
- [Configuration System (pkg/config/)](#configuration-system-pkgconfig)
- [Observability (pkg/telemetry/, pkg/status/)](#observability-pkgtelemetry-pkgstatus)
- [Supporting Files](#supporting-files)

## High-Level Structure

```
nebari-infrastructure-core/
├── cmd/nic/              # CLI application entry point
├── pkg/                  # Reusable library packages
│   ├── provider/         # Cloud provider implementations
│   ├── dnsprovider/      # DNS provider implementations
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

- **Provider Registration:** `main.go` explicitly registers all cloud and DNS providers using registry pattern
- **Telemetry Setup:** OpenTelemetry initialized in `main.go` based on environment variables
- **Logging:** Structured JSON logging via `slog` - only at this application layer
- **Signal Handling:** Context cancellation for graceful shutdown on SIGINT/SIGTERM
- **Status Display:** Uses `status.StartHandler()` to display progress updates to user

## Provider System (pkg/provider/)

**Location:** `pkg/provider/`

**Purpose:** Cloud provider abstraction and implementations. Each provider manages full lifecycle of cloud infrastructure.

### Core Provider Infrastructure

| File | Purpose |
|------|---------|
| `provider.go` | Defines `Provider` interface with Name(), ConfigKey(), Validate(), Deploy(), Reconcile(), Destroy(), GetKubeconfig(), Summary() |
| `registry.go` | Thread-safe provider registry with registration and lookup |

### AWS Provider (pkg/provider/aws/) - **Fully Implemented**

**Location:** `pkg/provider/aws/`

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
// Every function extracts provider config at entry
func (p *Provider) someFunction(ctx context.Context, clients *Clients, cfg *config.NebariConfig) error {
    awsCfg, err := extractAWSConfig(ctx, cfg)
    if err != nil {
        return err
    }
    // Use awsCfg.Region, awsCfg.NodeGroups, etc.
}
```

### GCP Provider (pkg/provider/gcp/) - **Stub**

**Location:** `pkg/provider/gcp/`

**Status:** Stub implementation - prints config as JSON

| File | Purpose |
|------|---------|
| `provider.go` | Stub provider that prints operations |
| `config.go` | GCP-specific config types: `Config`, `NodeGroup`, `Taint`, `GuestAccelerator` |

### Azure Provider (pkg/provider/azure/) - **Stub**

**Location:** `pkg/provider/azure/`

**Status:** Stub implementation - prints config as JSON

| File | Purpose |
|------|---------|
| `provider.go` | Stub provider that prints operations |
| `config.go` | Azure-specific config types: `Config`, `NodeGroup`, `Taint` |

### Local Provider (pkg/provider/local/) - **Stub**

**Location:** `pkg/provider/local/`

**Status:** Stub implementation for K3s local development

| File | Purpose |
|------|---------|
| `provider.go` | Stub provider that prints operations |
| `config.go` | Local-specific config types: `Config` |

## DNS Provider System (pkg/dnsprovider/)

**Location:** `pkg/dnsprovider/`

**Purpose:** DNS provider abstraction for managing DNS records. Separate from cloud providers to allow mixing (e.g., AWS infrastructure + Cloudflare DNS).

### Core DNS Infrastructure

| File | Purpose |
|------|---------|
| `provider.go` | Defines stateless `DNSProvider` interface: `ProvisionRecords()`, `DestroyRecords()` |
| `registry.go` | Thread-safe DNS provider registry |

### Cloudflare Provider (pkg/dnsprovider/cloudflare/)

**Location:** `pkg/dnsprovider/cloudflare/`

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
    Provider       string         `yaml:"provider"`         // Required: "aws", "gcp", "azure", "local"
    ProjectName    string         `yaml:"project_name"`     // Required: cluster name
    Domain         string         `yaml:"domain,omitempty"` // Optional: domain for ingress
    DNSProvider    string         `yaml:"dns_provider,omitempty"`
    DNS            map[string]any `yaml:"dns,omitempty"`

    // Provider-specific config captured via inline YAML
    // Access via: cfg.ProviderConfig["amazon_web_services"], etc.
    ProviderConfig map[string]any `yaml:",inline"`

    // Runtime options (not from YAML)
    DryRun  bool          `yaml:"-"`
    Force   bool          `yaml:"-"`
    Timeout time.Duration `yaml:"-"`
}
```

**Provider-Specific Configs:**
- AWS: `pkg/provider/aws/config.go` - `Config`, `NodeGroup`, `Taint`, `EFSConfig`
- GCP: `pkg/provider/gcp/config.go` - `Config`, `NodeGroup`, `Taint`, `GuestAccelerator`
- Azure: `pkg/provider/azure/config.go` - `Config`, `NodeGroup`, `Taint`
- Local: `pkg/provider/local/config.go` - `Config`

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
| `examples/local-config.yaml` | Sample local K3s configuration |

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
- **pkg/provider/** - Provider implementations (no logging, only telemetry spans)
- **pkg/config/** - Configuration parsing (provider-agnostic)
- **pkg/telemetry/** - Observability (OpenTelemetry)
- **pkg/status/** - Progress reporting (channel-based)

### 3. Explicit Dependencies
- No blank imports (`import _ "provider/aws"`)
- No init() magic
- Providers explicitly registered in `cmd/nic/main.go`
- Makes dependencies testable and visible

### 4. Provider Configuration Isolation
- Each provider owns its configuration types in `pkg/provider/<provider>/config.go`
- Central config stores provider configs as `any`
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
pkg/provider/<provider>/
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
- Creation: `pkg/provider/aws/vpc.go`
- Discovery: `pkg/provider/aws/vpc_discovery.go`
- Reconciliation: `pkg/provider/aws/vpc_reconcile.go`
- Deletion: `pkg/provider/aws/vpc_delete.go`

**"Where is configuration parsed?"**
- Parsing: `pkg/config/parser.go`
- Config types (global): `pkg/config/config.go`
- Config types (AWS): `pkg/provider/aws/config.go`

**"Where are commands defined?"**
- CLI framework: `cmd/nic/main.go`
- Deploy command: `cmd/nic/deploy.go`
- Destroy command: `cmd/nic/destroy.go`

**"Where is telemetry set up?"**
- Initialization: `pkg/telemetry/telemetry.go`
- Usage: Every function in `pkg/` starts with `tracer.Start()`

**"Where are providers registered?"**
- Registration: `cmd/nic/main.go`
- Registry implementation: `pkg/provider/registry.go`

### Finding Documentation

**"How does stateless operation work?"**
- `docs/architecture/06-stateless-operation.md`

**"What are the design decisions?"**
- `docs/architecture/` - Architectural Decision Records (ADRs)

**"How do I test?"**
- `docs/operations/` - Testing procedures

## Version Information

- **Go Version**: 1.21+
- **AWS SDK**: github.com/aws/aws-sdk-go-v2
- **CLI Framework**: github.com/spf13/cobra
- **Telemetry**: go.opentelemetry.io/otel
- **YAML Parsing**: github.com/goccy/go-yaml

---

For project overview and usage, see [README.md](README.md).
For detailed design documentation, see [docs/](docs/).
