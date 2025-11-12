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

### 4.2 Decision: Stateless Operation (No State Files)

**Context:** Need to track infrastructure state across deployments.

**Decision:** No state files. Query cloud provider APIs and Kubernetes APIs for actual state on every run.

**Rationale:**
- **No state drift**: Cloud APIs are always the source of truth
- **No state corruption**: Can't corrupt what doesn't exist
- **No state backends**: No S3 buckets, DynamoDB tables, or locking complexity
- **No state migration**: Version upgrades don't require state format changes
- **Automatic drift detection**: Every run compares desired vs actual state
- **Simpler mental model**: Query → Compare → Reconcile
- **Easier debugging**: `nic status` just queries cloud APIs

**How It Works:**
```
Every `nic deploy` run:
1. Parse nebari-config.yaml (desired state)
2. Query cloud APIs for resources with NIC tags (actual state)
3. Compare desired vs actual (automatic drift detection)
4. Apply changes to reconcile differences
5. Done (no state file written)
```

**Resource Discovery via Tags:**
All NIC-managed resources are tagged for discovery:
```go
tags := map[string]string{
    "nic.nebari.dev/managed-by":    "nic",
    "nic.nebari.dev/cluster-name":  "nebari-prod",
    "nic.nebari.dev/resource-type": "vpc|cluster|node-pool|...",
    "nic.nebari.dev/version":       "1.0.0",
    "nic.nebari.dev/config-hash":   "sha256:abc123...",
}
```

**Trade-offs:**
- ✅ Advantages: No state management complexity, automatic drift detection
- ⚠️ Slower: +30-60 seconds for cloud API queries per run
- ⚠️ Requires cloud access: Can't plan changes offline

See [Stateless Operation & Resource Discovery](06-stateless-operation.md) for complete details.

### 4.3 Decision: Declarative Semantics with Native SDKs

**Context:** How to provision infrastructure without Terraform?

**Decision:** Implement declarative reconciliation using cloud provider SDKs directly.

**Rationale:**
- Full control over API calls
- Better error messages (no Terraform layer)
- Faster execution (no plan/apply overhead)
- Easier debugging (stack traces in Go)
- Idempotent by design
- Can implement advanced retry logic

**Implementation Pattern:**
```go
// Declarative reconciliation loop
func (p *AWSProvider) Reconcile(ctx context.Context, desired DesiredState) error {
    actual, err := p.Query(ctx)
    if err != nil {
        return fmt.Errorf("querying actual state: %w", err)
    }

    diff := calculateDiff(desired, actual)

    for _, change := range diff.Changes {
        if err := p.applyChange(ctx, change); err != nil {
            return fmt.Errorf("applying change %s: %w", change.ID, err)
        }
    }

    return nil
}
```

### 4.4 Decision: ArgoCD for Foundational Software

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
1. ArgoCD (Helm chart via NIC)
   ↓
2. ArgoCD Applications (via NIC)
   ├── cert-manager (first, for TLS)
   ├── Envoy Gateway (depends on cert-manager)
   ├── OpenTelemetry Collector
   ├── Mimir, Loki, Tempo (parallel)
   ├── Grafana (depends on Mimir/Loki/Tempo)
   ├── Keycloak (depends on Envoy for ingress)
   └── Nebari Operator (last, depends on all)
```

### 4.5 Decision: Nebari Kubernetes Operator

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

### 4.6 Decision: OpenTelemetry Throughout

**Context:** Need comprehensive observability for NIC itself.

**Decision:** Instrument all NIC code with OpenTelemetry (traces, metrics, logs).

**Rationale:**
- Debugging deployment issues
- Performance monitoring
- Vendor-neutral (can export to any backend)
- Unified observability story (NIC uses same stack it deploys)
- Compliance with industry standards

**Implementation:**
- Every provider function wrapped in trace span
- Structured logging via slog with trace context
- Custom metrics for resource counts, deployment time, errors
- Export to deployed LGTM stack

### 4.7 Decision: Compiled Providers with Explicit Registration

**Context:** How to structure provider plugins?

**Decision:** All providers compiled into single binary, registered explicitly in `main()`.

**Rationale:**
- No blank imports (`import _ "..."`) anti-pattern
- Easy to test and debug
- Fast startup (no plugin RPC overhead)
- Simple deployment (single binary)
- Clear dependency graph

**Future:** May add RPC plugins (go-plugin) in v2.0+ for community providers.

---
