# ADR-0002: Longhorn Distributed Block Storage for AWS

## Status

Proposed

## Date

2026-02-13

## Context

AWS EBS volumes are bound to a single Availability Zone. When a StatefulSet pod needs to reschedule to a different AZ (node failure, scaling event, rebalancing), it cannot access its EBS-backed PersistentVolume. This makes StatefulSets like PostgreSQL (used by Keycloak) fragile on multi-AZ EKS clusters.

EFS solves the multi-AZ problem but has poor IOPS and latency for database workloads. The current default storage class on AWS is `gp2` (EBS), which has the AZ-binding problem.

GCP and Azure do not have this issue — GCP offers regional persistent disks and Azure offers ZRS-backed managed disks, both of which handle cross-AZ natively. This decision applies to AWS only.

Current stateful workloads: PostgreSQL (for Keycloak). Planned: Mimir, Loki, Tempo (observability stack), plus user-deployed StatefulSets.

## Decision Drivers

- **Lightweight base deployment**: Nebari's default deploy should not require heavy infrastructure components
- **Low operational complexity**: Storage layer should be hard to misconfigure
- **Cross-AZ resilience**: StatefulSets must survive AZ failures without data loss
- **Small resource footprint**: Storage daemons should run on minimal node resources
- **Configurable topology**: Support both colocated (shared nodes) and dedicated storage nodes

## Considered Options

1. Rook/Ceph
2. OpenEBS Mayastor
3. Longhorn

## Decision Outcome

Chosen option: **Longhorn**, because it provides the best balance of lightweight deployment, low operational complexity, and cross-AZ replication for Kubernetes-native block storage.

Longhorn is installed via Helm inside the AWS provider's `Deploy()` method, after `tf.Apply()` completes. It is enabled by default on AWS (opt-out via `longhorn.enabled: false`).

### Consequences

**Good:**
- StatefulSets can reschedule across AZs without data loss
- Minimal resource overhead (~512MB-1GB RAM per node)
- Simple configuration with sensible defaults
- Purpose-built for Kubernetes — no adaptation layer
- CNCF Incubating project with active development

**Bad:**
- Lower raw IOPS than Ceph or Mayastor (sufficient for databases and observability, not for high-frequency workloads)
- Adds a storage replication layer that increases write latency vs raw EBS
- Storage cost multiplied by replica count (2x default)

## Options Detail

### Option 1: Rook/Ceph

Rook is a CNCF Graduated operator for Ceph, a decade-proven distributed storage system.

**Pros:**
- Most battle-tested option
- Supports block, file, and object storage (S3-compatible)
- Highly configurable (pools, placement groups, CRUSH rules)

**Cons:**
- Heaviest resource footprint: MONs (~1GB each x3), MGR (~512MB), OSDs (~1-4GB each)
- High operational complexity (monitor daemons, OSDs, placement groups)
- Highest risk of misconfiguration
- Requires Ceph expertise for debugging degraded clusters

### Option 2: OpenEBS Mayastor

OpenEBS Mayastor is a CNCF Sandbox project using NVMe-oF (SPDK-based) for high-performance block storage.

**Pros:**
- Best raw IOPS performance of the three options
- Modern NVMe-oF architecture
- Medium resource footprint

**Cons:**
- Requires HugePages (1024 x 4MB = 4GB per storage node) pre-configured on nodes
- Requires `nvme-tcp` kernel module loaded on each node
- Infrastructure-level prerequisites bleed into Terraform node group configuration (AMIs, launch templates)
- Smaller community, CNCF Sandbox maturity level

### Option 3: Longhorn (Chosen)

Longhorn is a CNCF Incubating project purpose-built for Kubernetes distributed block storage.

**Pros:**
- Lightest resource footprint (~512MB-1GB RAM per node)
- Lowest operational complexity — single Helm chart, minimal configuration surface
- Purpose-built for Kubernetes (no adaptation layer)
- Built-in zone-aware replica scheduling via `topology.kubernetes.io/zone` labels
- Native S3 backup support
- Configurable replica count per StorageClass

**Cons:**
- Lower raw IOPS than Ceph or Mayastor
- CNCF Incubating (not yet Graduated)
- Less proven at massive scale (sufficient for Nebari's workload profile)

## Detailed Design

### Deployment Flow

```
provider.Deploy():
  tf.Apply()              -> EKS cluster + EBS CSI + (optionally EFS)
  getKubeconfig()         -> from Terraform outputs
  waitForCluster()        -> at least one node ready
  installLonghorn()       -> Helm Go SDK install/upgrade
  waitForLonghorn()       -> DaemonSet + Deployments ready, StorageClass exists
  return
```

Longhorn is installed as the last step of the AWS provider's `Deploy()` method. This ensures the StorageClass exists before ArgoCD deploys any StatefulSets.

Skipped when:
- `cfg.DryRun` is true
- `cfg.IsExistingCluster()` is true
- `longhorn.enabled` is explicitly false

### Destroy Flow

```
provider.Destroy():
  getKubeconfig()          -> from Terraform state
  uninstallLonghorn()      -> Helm uninstall, wait for PV cleanup
  tf.Destroy()             -> tear down EKS
```

Longhorn must be uninstalled before the cluster is torn down. PVs backed by Longhorn can block node group deletion if not cleaned up first.

### New and Changed Packages

**`pkg/helm/` (new)** — Extract shared Helm utilities from `pkg/argocd/helm.go`:
- `NewActionConfig(kubeconfigPath, namespace)` — creates Helm action configuration
- `kubeconfigGetter` — implements Helm's RESTClientGetter interface
- `AddRepo(ctx, name, url)` — adds a Helm chart repository
- `Install(ctx, cfg InstallConfig)` — install or upgrade with idempotency

Both `pkg/argocd/` and `pkg/provider/aws/` consume this package.

**`pkg/provider/aws/config.go` (modified)** — Add LonghornConfig:
```go
type LonghornConfig struct {
    Enabled        bool              `yaml:"enabled,omitempty"`        // default: true
    ReplicaCount   int               `yaml:"replica_count,omitempty"`  // default: 2
    DedicatedNodes bool              `yaml:"dedicated_nodes,omitempty"`
    NodeSelector   map[string]string `yaml:"node_selector,omitempty"`
}
```

Default logic: A nil `*LonghornConfig` pointer means "use defaults" (enabled, 2 replicas). A non-nil pointer with `Enabled: false` means the user explicitly opted out.

**`pkg/provider/aws/longhorn.go` (new)** — Longhorn install/uninstall/wait logic and Helm values generation.

**`pkg/provider/aws/provider.go` (modified)** — `Deploy()` calls installLonghorn after tf.Apply; `Destroy()` calls uninstallLonghorn before tf.Destroy.

**`pkg/argocd/writer.go` (modified)** — `storageClassForProvider("aws")` returns `"longhorn"` instead of `"gp2"`.

**`pkg/argocd/helm.go` (modified)** — Refactored to use `pkg/helm/` for shared utilities.

### Storage Class Selection

`storageClassForProvider("aws")` returns `"longhorn"` instead of `"gp2"`. All ArgoCD application templates already consume `{{ .StorageClass }}`, so PostgreSQL and future stateful apps automatically use Longhorn with no template changes.

When Longhorn is disabled (`longhorn.enabled: false`), falls back to `gp2`.

### Configuration

Default (no longhorn section needed):
```yaml
amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.34"
  node_groups:
    user:
      instance: m7i.xlarge
      min_nodes: 0
      max_nodes: 5
```

Explicit configuration:
```yaml
amazon_web_services:
  region: us-west-2
  longhorn:
    replica_count: 3
    dedicated_nodes: true
    node_selector:
      node.longhorn.io/storage: "true"
```

Opt out:
```yaml
amazon_web_services:
  longhorn:
    enabled: false
```

### Helm Values

Standard deployment (colocated):
```yaml
persistence:
  defaultClass: true
  defaultClassReplicaCount: 2
  defaultFsType: ext4

defaultSettings:
  replicaZoneSoftAntiAffinity: true
  replicaAutoBalance: best-effort
```

Dedicated nodes deployment adds:
```yaml
defaultSettings:
  createDefaultDiskLabeledNodes: true

longhornManager:
  nodeSelector:
    node.longhorn.io/storage: "true"
  tolerations:
    - key: node.longhorn.io/storage
      operator: Exists
      effect: NoSchedule

longhornDriver:
  nodeSelector:
    node.longhorn.io/storage: "true"
  tolerations:
    - key: node.longhorn.io/storage
      operator: Exists
      effect: NoSchedule
```

### Testing Strategy

- Unit tests for config parsing and defaults (nil LonghornConfig -> enabled with 2 replicas)
- Unit tests for Helm values generation from config
- Unit tests for `storageClassForProvider` returning `"longhorn"` for AWS
- Integration test: Deploy() calls installLonghorn after tf.Apply (mock Helm client)
- Integration test: Destroy() calls uninstallLonghorn before tf.Destroy

### Follow-up Work (Separate Issue)

- Create an EFS StorageClass when `efs.enabled: true`
- When EFS is enabled, EFS becomes the default StorageClass and Longhorn becomes the explicit-use option for performance-sensitive StatefulSets (e.g., PostgreSQL)
- Update storage class selection to be config-aware

## Links

- [Longhorn documentation](https://longhorn.io/)
- [Longhorn Helm chart values](https://github.com/longhorn/longhorn/blob/master/chart/values.yaml)
- [Longhorn: restrict storage to specific nodes](https://longhorn.io/kb/tip-only-use-storage-on-a-set-of-nodes/)
- [CNCF Longhorn project page](https://www.cncf.io/projects/longhorn/)
- [MADR Format](https://adr.github.io/madr/)
