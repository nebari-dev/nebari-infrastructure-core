# Nebari Infrastructure Core - Architecture Walkthrough

A Go CLI for declarative cloud infrastructure management using native cloud SDKs. Stateless, idempotent, tag-based resource discovery.

---

## Core Design

- **No state files** — queries cloud APIs on every run for actual state
- **Tag-based discovery** — all resources tagged `nic.nebari.dev/cluster-name` and `nic.nebari.dev/managed-by=nic`
- **Reconciliation loop** — discover → diff → create/update/delete
- **Native SDKs / Terraform** — aws-sdk-go-v2 for AWS; Azure uses OpenTofu against the Track A module `nebari-dev/terraform-azurerm-aks-cluster` plus the Azure SDK (`armcontainerservice`) for kubeconfig retrieval. GCP pending.
- **Explicit registration** — no blank imports or `init()` magic; providers registered in `main.go`

---

## Directory Structure

```
cmd/nic/                     # CLI layer (logging happens here)
├── main.go                  # Provider registration, telemetry setup
├── deploy.go, destroy.go    # Command implementations
└── validate.go, version.go

pkg/                         # Library code (no logging, OTel spans only)
├── config/
│   ├── config.go            # NebariConfig struct (provider configs as interface{})
│   └── parser.go            # Lenient YAML parsing, UnmarshalProviderConfig helper

├── provider/
│   ├── provider.go          # Provider interface
│   ├── registry.go          # Thread-safe registry
│   └── aws/
│       ├── provider.go      # Reconcile/Destroy orchestration
│       ├── config.go        # AWS-specific types (Config, NodeGroup, EFSConfig)
│       ├── client.go        # SDK client initialization
│       ├── interfaces.go    # Client interfaces for mocking
│       ├── vpc*.go          # VPC lifecycle (discovery, reconcile, delete)
│       ├── eks*.go          # EKS cluster lifecycle
│       ├── nodegroups*.go   # Node group lifecycle
│       ├── iam*.go          # IAM role management
│       ├── efs.go           # EFS storage
│       ├── tags.go          # Tag helpers
│       └── state.go         # State structs (VPCState, ClusterState, etc.)

├── status/                  # Progress messaging (Info, Progress, Success, Error)
└── telemetry/               # OTel tracer setup
```

Files organized by **resource** (vpc, eks, nodegroups), not by operation. Each resource's full lifecycle is colocated.

---

## Provider Interface

```go
type Provider interface {
    Name() string
    Validate(ctx context.Context, config *config.NebariConfig) error
    Deploy(ctx context.Context, config *config.NebariConfig) error
    Reconcile(ctx context.Context, config *config.NebariConfig) error  // Deprecated - see issue #44
    Destroy(ctx context.Context, config *config.NebariConfig) error
    GetKubeconfig(ctx context.Context, config *config.NebariConfig) ([]byte, error)
    Summary(config *config.NebariConfig) map[string]string
}
```

`Deploy()` is idempotent - running it multiple times with the same config produces the same result. Use `--dry-run` to preview changes.

---

## Reconciliation Flow (AWS)

```
Reconcile()
├── Initialize clients (EC2, EKS, IAM, EFS)
├── VPC: DiscoverVPC() → reconcileVPC() (create if nil, error if CIDR changed)
├── IAM: Ensure cluster role + node role exist
├── EKS: DiscoverCluster() → reconcileCluster() (create/update version/logging/endpoint)
├── Node Groups: DiscoverNodeGroups() → reconcile each in parallel, delete orphans
└── EFS: DiscoverEFS() → reconcileEFS() (if enabled)
```

### Discovery Pattern

Returns `nil` if not found (triggers creation), otherwise returns state struct:

```go
func DiscoverVPC(ctx, clients, clusterName) (*VPCState, error) {
    vpcs := ec2.DescribeVpcs(filter: tag nic.nebari.dev/cluster-name = clusterName)
    if len(vpcs) == 0 {
        return nil, nil
    }
    return &VPCState{...}, nil
}
```

### Reconcile Pattern

```go
func reconcileVPC(ctx, clients, config, actual *VPCState) (*VPCState, error) {
    if actual == nil {
        return createVPC(...)
    }
    // Immutable check
    if actual.CIDR != config.CIDR {
        return nil, fmt.Errorf("cannot change VPC CIDR (requires destroy/recreate)")
    }
    // Mutable updates via API
    return actual, nil
}
```

**Immutable fields** (error if changed): VPC CIDR, EKS KMS key, node group instance type, EFS encryption
**Mutable fields** (update in place): K8s version, scaling config, labels, taints, logging

---

## Destroy Flow

Reverse dependency order:

```
Destroy()
├── Node Groups (depend on EKS)
├── EFS mount targets, then EFS (depend on VPC subnets)
├── EKS Cluster (depends on VPC)
├── VPC (NAT gateways → EIPs → subnets → IGW → route tables → VPC)
├── IAM Roles (detach policies first)
└── Orphan cleanup (3 passes, tag-based search)
```

`--force` continues on errors, collects all failures, reports at end.

---

## Configuration

Provider-specific configs stored as `any` in `NebariConfig`, concrete types in provider packages:

```go
// pkg/config/config.go
type NebariConfig struct {
    ProjectName    string         `yaml:"project_name"`
    Provider       string         `yaml:"provider"`
    ProviderConfig map[string]any `yaml:",inline"` // Captures provider-specific config
    // ...
}
// Access provider config: cfg.ProviderConfig["amazon_web_services"]

// pkg/provider/aws/config.go
type Config struct {
    Region            string      `yaml:"region"`
    KubernetesVersion string      `yaml:"kubernetes_version"`
    NodeGroups        []NodeGroup `yaml:"node_groups"`
    // ...
}
```

Extraction pattern (call at function entry):

```go
func someAWSFunction(ctx context.Context, cfg *config.NebariConfig) error {
    awsCfg, err := extractAWSConfig(ctx, cfg)  // Uses UnmarshalProviderConfig
    if err != nil {
        return err
    }
    // Use awsCfg.Region, awsCfg.NodeGroups, etc.
}
```

---

## Instrumentation

Every function follows this pattern:

```go
func SomeFunction(ctx context.Context, ...) error {
    ctx, span := otel.Tracer("nebari-infrastructure-core").Start(ctx, "pkg.SomeFunction")
    defer span.End()

    span.SetAttributes(attribute.String("cluster", name))

    if err != nil {
        span.RecordError(err)
        return err
    }
    return nil
}
```

- `OTEL_EXPORTER=console|otlp|both|none`
- `OTEL_ENDPOINT=localhost:4317`

Logging (`slog`) only in `cmd/nic/`; library code uses OTel spans and `status.Info/Progress/Success/Error`.

---

## Testing

Client interfaces in `interfaces.go` enable mocking:

```go
type EC2ClientAPI interface {
    DescribeVpcs(ctx, input) (*ec2.DescribeVpcsOutput, error)
    CreateVpc(ctx, input) (*ec2.CreateVpcOutput, error)
    // ...
}
```

Focus tests on:
- Decision points (create vs update vs error)
- Immutable field validation
- Error paths
- Registry concurrent access

---

## Adding a Provider

1. `pkg/provider/<name>/config.go` — provider-specific config types
2. `pkg/provider/<name>/provider.go` — implement `Provider` interface
3. Add `interface{}` field to `NebariConfig`
4. Register in `main.go`: `registry.Register(ctx, "name", newprovider.NewProvider())`
5. Example config in `examples/`
6. Tests

See `pkg/provider/aws/` for a native-SDK reference implementation, and `pkg/provider/azure/` for a Terraform-module-driven reference (OpenTofu wrapping an external module, with Azure SDK used only for kubeconfig retrieval and tag-based cleanup).

---

## Key Patterns

| Pattern | Implementation |
|---------|----------------|
| Stateless | No state file; discover via tagged API queries |
| Idempotent | Reconcile compares actual→desired; no-op if equal |
| Loose coupling | Resources pass IDs, not internal state |
| Explicit deps | Provider registration in `main.go`, no `init()` |
| Config extraction | `extractAWSConfig()` at function entry |
| Observability | OTel spans on all functions, logging at CLI layer only |
