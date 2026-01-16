# Key Architectural Decisions

### 4.1 Decision: Unified Deployment (Not Staged)

**Context:** Old Nebari had 6+ stages (terraform-state, infrastructure, kubernetes-initialize, ingress, keycloak, etc.)

**Decision:** NIC deploys everything in one unified workflow.

**Rationale:**

- Eliminates stage dependency complexity
- Faster deployment (parallel operations where possible)
- Easier to reason about (one command: `nic deploy`)
- Clearer error messages (no inter-stage state issues)
- ArgoCD handles application-level dependencies

**Alternatives Considered:**

| Alternative | Rejected Because |
|-------------|------------------|
| Keep staged approach | Complexity, state management issues, slower |
| Makefile-based orchestration | Not portable, hard to debug, limited error handling |
| Ansible playbooks | YAML hell, imperative, no true state management |

### 4.2 Decision: OpenTofu/Terraform Modules for Infrastructure

**Context:** Need to provision cloud infrastructure (VPC, EKS/GKE/AKS, storage) reliably.

**Decision:** Use OpenTofu/Terraform modules orchestrated via the terraform-exec Go library.

**Rationale:**

- **Battle-tested modules**: Leverage existing, proven Terraform modules
- **Community ecosystem**: Access to thousands of maintained modules
- **Familiar to teams**: Most infrastructure engineers know Terraform/HCL
- **Standard tooling**: Works with terraform-docs, tfsec, Atlantis, etc.
- **Faster development**: Reuse modules instead of writing SDK code from scratch
- **Standard state format**: Terraform state is well-understood and tooling-rich

**How It Works:**

```
Every `nic deploy` run:
1. Parse config.yaml (desired state)
2. Convert config to Terraform variables
3. Run terraform-exec: init, plan, apply
4. OpenTofu provisions infrastructure via provider plugins
5. State file updated in remote backend
6. Go CLI waits for cluster readiness
7. Deploy foundational software via ArgoCD
```

**Trade-offs:**

- **External dependency**: Requires OpenTofu/Terraform binary installed
- **State management**: Must configure and manage state backends
- **Debugging layers**: Errors pass through Go → terraform-exec → OpenTofu → Cloud API

See [State Management](05-state-management.md) for state backend configuration.

### 4.3 Decision: terraform-exec for Go Orchestration

**Context:** How to invoke OpenTofu from the Go CLI?

**Decision:** Use HashiCorp's terraform-exec library for programmatic OpenTofu control.

**Rationale:**

- Official Go library for Terraform/OpenTofu execution
- Type-safe interface for init, plan, apply, destroy
- Structured output parsing (JSON plan output)
- Well-maintained and documented
- Supports both Terraform and OpenTofu binaries

**Implementation Pattern:**

```go
// terraform-exec wrapper with OpenTelemetry instrumentation
func (e *Executor) Apply(ctx context.Context, varFiles []string) error {
    ctx, span := tracer.Start(ctx, "Executor.Apply")
    defer span.End()

    slog.InfoContext(ctx, "applying infrastructure changes")

    var opts []tfexec.ApplyOption
    for _, vf := range varFiles {
        opts = append(opts, tfexec.VarFile(vf))
    }

    if err := e.tf.Apply(ctx, opts...); err != nil {
        span.RecordError(err)
        return fmt.Errorf("terraform apply: %w", err)
    }

    return nil
}
```

See [Terraform-Exec Integration](../implementation/08-terraform-exec-integration.md) for complete implementation.

### 4.4 Decision: Terraform State with Remote Backends

**Context:** Need to track infrastructure state across deployments.

**Decision:** Use standard Terraform state files stored in remote backends (S3, GCS, Azure Blob).

**Rationale:**

- **Standard format**: Terraform state is industry-standard
- **Team collaboration**: Remote backends support concurrent access with locking
- **State versioning**: Backends like S3 support versioning for recovery
- **Drift detection**: `terraform plan` compares state with actual infrastructure
- **Ecosystem integration**: Works with Atlantis, Terraform Cloud, etc.

**State Backend Configuration:**

```hcl
# AWS Backend (S3 + DynamoDB for locking)
terraform {
  backend "s3" {
    bucket         = "nebari-prod-terraform-state"
    key            = "nic/terraform.tfstate"
    region         = "us-west-2"
    encrypt        = true
    dynamodb_table = "nebari-prod-terraform-locks"
  }
}
```

**Trade-offs:**

- **Setup required**: Must create and configure state backend resources
- **State drift risk**: State file can diverge from actual infrastructure
- **Sensitive data**: State files contain credentials and must be secured
- **Locking complexity**: Lock conflicts require manual resolution

See [State Management](05-state-management.md) for complete backend configuration.

### 4.5 Decision: ArgoCD for Foundational Software

**Context:** How to deploy and manage foundational software (Keycloak, LGTM, etc.)?

**Decision:** Deploy ArgoCD first via Helm, then use ArgoCD applications for all other foundational software.

**Rationale:**

- GitOps best practices (declarative, version-controlled)
- Automatic sync and health checks
- Dependency management (app-of-apps pattern)
- Self-healing (detects and fixes drift)
- Rollback capability
- Clear audit trail (Git history)

**Deployment Order:**

```
1. ArgoCD (Helm chart via Terraform helm provider)
   ↓
2. ArgoCD Applications (via Terraform kubernetes provider)
   ├── cert-manager (first, for TLS)
   ├── Envoy Gateway (depends on cert-manager)
   ├── OpenTelemetry Collector
   ├── Mimir, Loki, Tempo (parallel)
   ├── Grafana (depends on Mimir/Loki/Tempo)
   ├── Keycloak (depends on Envoy for ingress)
   └── Nebari Operator (last, depends on all)
```

### 4.6 Decision: Nebari Kubernetes Operator

**Context:** Applications need to integrate with auth, o11y, and routing.

**Decision:** Build a Kubernetes operator that watches `nebari-application` CRDs and automates integration.

**Rationale:**

- Reduces manual configuration (no more copy-paste YAML)
- Consistent integration across all apps
- Self-service for developers
- Automatic updates when foundational software changes
- Native Kubernetes workflow

**Example CRD Usage:**

```yaml
apiVersion: nebari.dev/v1alpha1
kind: NebariApplication
metadata:
  name: jupyter-hub
  namespace: jupyter
spec:
  displayName: "JupyterHub"
  routing:
    domain: jupyter.example.com
    enableTLS: true
    paths:
      - path: /
        service: jupyterhub
        port: 8000
  authentication:
    enabled: true
    allowedGroups:
      - data-scientists
      - admins
  observability:
    metrics:
      enabled: true
      port: 9090
      path: /metrics
    logs:
      enabled: true
    traces:
      enabled: true
    dashboards:
      - name: "JupyterHub Overview"
        source: "https://..."
```

**Operator Actions:**

1. Creates Keycloak OAuth2 client
2. Configures Envoy Gateway HTTPRoute
3. Provisions cert-manager Certificate
4. Creates Grafana Dashboard ConfigMap
5. Configures OpenTelemetry ServiceMonitor
6. Updates status with URLs and credentials

### 4.7 Decision: OpenTelemetry Throughout

**Context:** Need comprehensive observability for NIC itself.

**Decision:** Instrument all NIC code with OpenTelemetry (traces, metrics, logs).

**Rationale:**

- Debugging deployment issues
- Performance monitoring
- Vendor-neutral (can export to any backend)
- Unified observability story (NIC uses same stack it deploys)
- Compliance with industry standards

**Implementation:**

- Every Go function wrapped in trace span
- Structured logging via slog with trace context
- Custom metrics for resource counts, deployment time, errors
- Export to deployed LGTM stack

### 4.8 Decision: Go CLI with Embedded Modules

**Context:** How to package and distribute NIC?

**Decision:** Single Go binary with OpenTofu modules embedded or cloned from git.

**Rationale:**

- Easy installation (go install or download binary)
- Modules version-locked with NIC release
- Predictable behavior across environments
- No separate module download step

**Module Delivery Options:**

1. **Embedded** (default): Modules embedded in binary via Go embed
2. **Git clone**: Modules cloned from versioned git tag
3. **Local path**: Modules from local filesystem (development)

---
