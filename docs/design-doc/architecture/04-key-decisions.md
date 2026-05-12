# Key Architectural Decisions

### 4.1 Decision: Unified Deployment (Not Staged)

**Context:** Old Nebari had six or more stages (terraform-state, infrastructure, kubernetes-initialize, ingress, keycloak, etc.).

**Decision:** NIC deploys everything from `nic deploy -f config.yaml` in one workflow.

**Rationale:**

- Eliminates stage-dependency complexity
- Faster deployment (parallel operations where possible)
- Easier to reason about (one command)
- Clearer error messages (no inter-stage state issues)
- ArgoCD handles application-level dependencies via sync waves

### 4.2 Decision: Per-Provider Backing Tools (Not "OpenTofu Everywhere")

**Context:** Different cluster providers have different idiomatic tooling. EKS has excellent Terraform support. Hetzner Cloud has a purpose-built tool (`hetzner-k3s`) that handles bootstrap better than tofu would. Kind is configured via a CLI flag and a YAML file.

**Decision:** The `provider.Provider` interface is the abstraction. Each provider implementation chooses the right backing tool for its environment:

- **AWS:** OpenTofu, via the `terraform-exec` Go library, running the upstream `nebari-dev/eks-cluster` registry module
- **Hetzner:** the `hetzner-k3s` binary, talking directly to the Hetzner Cloud API
- **Local:** Kind, driven by `make localkind-up`. The local provider itself is a thin adapter; the CLI is responsible for cluster creation.
- **Existing:** no IaC at all - the provider reads `kubeconfig` and `context` from config and adopts the cluster

This direction is documented in [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md), which proposes formalizing the abstraction as out-of-tree gRPC plugins so that private and org-specific providers (e.g., OpenTeams' internal ASCOT DNS provider) have a supported integration path that isn't "fork NIC."

**Rationale:**

- Honest about reality: not every provider fits a Terraform module
- Each provider can use its strongest available tool
- The `Provider` interface, not Terraform, is the contract
- Future-proof for out-of-tree plugins (ADR-0004)

### 4.3 Decision: terraform-exec for the AWS Provider

**Context:** How to invoke OpenTofu from Go for the AWS provider.

**Decision:** Use HashiCorp's `terraform-exec` library wrapped by `pkg/tofu.TerraformExecutor`.

**Implementation Pattern (real shape from `pkg/tofu/tofu.go`):**

```go
type TerraformExecutor struct {
    *tfexec.Terraform
    workingDir string
    appFs      afero.Fs
}

func (te *TerraformExecutor) Apply(ctx context.Context, opts ...tfexec.ApplyOption) error {
    ctx = signalSafeContext(ctx)
    return te.streamThroughStatus(ctx, func(w io.Writer) error {
        return te.ApplyJSON(ctx, w, opts...)
    })
}
```

Two things to note:

1. The wrapper calls `ApplyJSON`/`PlanJSON`/`InitJSON`/`DestroyJSON` and streams output through the **status channel** attached to `ctx` (see [System Overview §2.4](02-system-overview.md#24-the-status-channel-pkg--cmd-seam)). Library code does not call `slog` - that translation happens in `cmd/nic/status_handler.go`.
2. `Setup(ctx, templates fs.FS, tfvars any)` (also in `pkg/tofu/tofu.go`) handles binary acquisition via `tofudl` with caching at `~/.cache/nic/tofu/`, extraction of embedded templates, and `terraform.tfvars.json` writing. Callers do not look up tofu in `PATH`.

See [Terraform-Exec Integration](../implementation/08-terraform-exec-integration.md).

### 4.4 Decision: Terraform State in S3 with Native Lockfile-Based Locking (AWS)

**Context:** The AWS provider needs to track infrastructure state across deployments.

**Decision:** Standard Terraform S3 backend with `use_lockfile = true`. No DynamoDB table is involved.

**State Backend Configuration (real `pkg/provider/aws/templates/backend.tf`):**

```hcl
terraform {
  backend "s3" {
    encrypt      = true
    use_lockfile = true
  }
}
```

Bucket and key are populated dynamically at `tofu init` time. The bucket name is deterministic: `nic-tfstate-<project>-<region>-<8-hex-of-account-id-hash>`. NIC auto-creates the bucket (`pkg/provider/aws/state.go:ensureStateBucket`) with versioning and public-access-block enabled.

**Non-AWS providers manage state in tool-specific ways:**

- Hetzner: `hetzner-k3s` writes a cluster state file; the location is configured via the tool's own settings
- Local: Kind manages its own cluster lifecycle
- Existing: no NIC-owned state

See [State Management](05-state-management.md).

### 4.5 Decision: ArgoCD for Foundational Software

**Context:** How to deploy and manage foundational software (cert-manager, Envoy Gateway, Keycloak, etc.).

**Decision:** NIC installs ArgoCD first via the **embedded Helm Go SDK** (`helm.sh/helm/v3/pkg/action`, wrapped in `pkg/helm`), then renders ArgoCD `Application` manifests into a Git repository for ArgoCD to sync.

**Rationale:**

- GitOps best practices: declarative, version-controlled, self-healing
- Sync waves manage cross-app dependencies (cert-manager before things that need certs, etc.)
- ArgoCD itself can be installed without a separately-installed Helm CLI

**Deployment Order (real apps under `pkg/argocd/templates/apps/`):**

```
1. ArgoCD (installed by NIC via the Helm Go SDK)
   ↓
2. App-of-apps root.yaml, then individual apps via sync waves:
   ├── cert-manager + cluster-issuers + certificates
   ├── Envoy Gateway + gateway-config + httproutes
   ├── postgresql + Keycloak
   ├── metallb + metallb-config (only when InfraSettings.NeedsMetalLB)
   ├── opentelemetry-collector
   ├── nebari-operator (Kustomized from nebari-dev/nebari-operator)
   └── nebari-landingpage
```

A full LGTM stack (Loki / Grafana / Tempo / Mimir) is not deployed today; that is roadmap work.

### 4.6 Decision: Nebari Operator Is Out-of-Tree

**Context:** Applications integrate with auth and routing via a `NebariApp` CRD.

**Decision:** The Nebari Operator is its own product, developed in [`nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator). NIC deploys it as a foundational ArgoCD application via Kustomize (`pkg/argocd/templates/manifests/nebari-operator/kustomization.yaml`).

**Rationale:**

- The operator has its own release cadence and CRD schema
- NIC is an infrastructure tool; the operator is an application-integration tool
- Keeps NIC's surface area focused on cluster provisioning and bootstrap

NIC passes `InfraSettings.KeycloakBasePath` and `InfraSettings.HTTPSPort` into the operator's Kustomize patch so it routes correctly per provider. NIC does not implement the reconciliation logic; that lives upstream.

### 4.7 Decision: OpenTelemetry in Library Code, slog in the CLI

**Context:** Need observability for NIC itself, without coupling library code to a specific logging backend.

**Decision:**

- All new functions in `pkg/` are wrapped in OpenTelemetry trace spans, with the documented exemptions in [`CLAUDE.md`](../../../CLAUDE.md) (e.g., per-line writers in `pkg/status` and byte/line helpers in `pkg/tofu`).
- Library code never calls `slog`. User-visible progress goes through the status channel; `cmd/nic/status_handler.go` is the only translator into structured logs.
- Exporters are configurable via `OTEL_EXPORTER` (`console` default, `otlp`, `both`, `none`) and `OTEL_ENDPOINT`.

**Pattern:**

```go
func SomeFunction(ctx context.Context, ...) error {
    tracer := otel.Tracer("nebari-infrastructure-core")
    ctx, span := tracer.Start(ctx, "package.FunctionName")
    defer span.End()

    span.SetAttributes(attribute.String("key", value))

    if err != nil {
        span.RecordError(err)
        return err
    }
    return nil
}
```

### 4.8 Decision: Single Go Binary with Embedded Templates (Today); Out-of-Tree Plugins (Tomorrow)

**Context:** How to package and distribute NIC.

**Decision (today):** Single Go binary. AWS templates are embedded via `go:embed` from `pkg/provider/aws/templates/`. OpenTofu itself is downloaded on first use into `~/.cache/nic/tofu/` and reused thereafter.

**Decision (planned, [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md)):** Move providers (cluster, DNS, cert, git, software) to out-of-tree gRPC plugins discovered at runtime. The current in-tree layout is the bootstrap target; the plugin architecture is the long-term direction.

---
