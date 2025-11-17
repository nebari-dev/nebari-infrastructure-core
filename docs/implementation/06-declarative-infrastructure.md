# Declarative Infrastructure with Native SDKs

### 5.1 Declarative Programming Model

**Core Concept:** Users declare desired state, NIC reconciles actual state to match.

**Reconciliation Formula:**

```
Desired State (config.yaml)
    ∩
Actual State (Cloud APIs + K8s APIs)
    =
Actions Needed (create/update/delete)
```

**Declarative Guarantees:**

1. **Idempotency**: Running `nic deploy` multiple times with same config → no changes
2. **Convergence**: Eventually reaches desired state despite transient errors
3. **Deterministic**: Same config → same infrastructure (given same initial state)
4. **Drift Detection**: Detects manual changes and can repair

### 5.2 Reconciliation Loop

**High-Level Algorithm:**

```go
func Reconcile(ctx context.Context, config Config) error {
    // 1. Parse desired state from config
    desired, err := ParseDesiredState(config)
    if err != nil {
        return fmt.Errorf("parsing config: %w", err)
    }

    // 2. Query actual state from cloud/k8s
    actual, err := QueryActualState(ctx)
    if err != nil {
        return fmt.Errorf("querying actual state: %w", err)
    }

    // 3. Calculate diff
    diff := CalculateDiff(desired, actual)

    // 4. Apply changes
    for _, change := range diff.Changes {
        if err := ApplyChange(ctx, change); err != nil {
            return fmt.Errorf("applying %s: %w", change, err)
        }
    }

    return nil
}
```

**Change Types:**

- `Create`: Resource doesn't exist, needs to be created
- `Update`: Resource exists but properties differ
- `Delete`: Resource exists but not in desired state
- `NoOp`: Resource matches desired state

### 5.3 Idempotency

**Scenario 1: Fresh Deployment**

```bash
$ nic deploy -f config.yaml
→ Creates: VPC, EKS cluster, node pools, foundational software
→ State: All resources created

$ nic deploy -f config.yaml  # Run again
→ Creates: Nothing (all resources already exist)
→ State: No changes
```

**Scenario 2: Configuration Change**

```yaml
# Change node pool size from 3 to 5
node_pools:
  - name: general
    min_size: 3 # was 3
    max_size: 5 # was 3
```

```bash
$ nic deploy -f config.yaml
→ Updates: Node pool auto-scaling group (3 → 5 nodes)
→ State: Node pool updated
```

**Scenario 3: Drift Repair**

```bash
# User manually deletes a node pool outside of NIC
$ aws eks delete-nodegroup --cluster nebari-prod --nodegroup general

$ nic deploy -f config.yaml
→ Creates: Node pool 'general' (drift detected and repaired)
→ State: Node pool recreated
```

### 5.4 Native SDK Usage

**AWS Example (EKS Cluster):**

```go
import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/eks"
)

func (p *AWSProvider) ensureEKSCluster(ctx context.Context, desired ClusterSpec) error {
    ctx, span := tracer.Start(ctx, "ensureEKSCluster")
    defer span.End()

    // Check if cluster exists
    describeOut, err := p.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
        Name: aws.String(desired.Name),
    })

    if err != nil {
        // Cluster doesn't exist, create it
        slog.InfoContext(ctx, "creating EKS cluster", "name", desired.Name)

        _, err := p.eksClient.CreateCluster(ctx, &eks.CreateClusterInput{
            Name:    aws.String(desired.Name),
            Version: aws.String(desired.KubernetesVersion),
            RoleArn: aws.String(desired.RoleARN),
            ResourcesVpcConfig: &types.VpcConfigRequest{
                SubnetIds:        desired.SubnetIDs,
                SecurityGroupIds: desired.SecurityGroupIDs,
            },
            Tags: desired.Tags,
        })

        if err != nil {
            return fmt.Errorf("creating cluster: %w", err)
        }

        // Wait for cluster to be active
        waiter := eks.NewClusterActiveWaiter(p.eksClient)
        if err := waiter.Wait(ctx, &eks.DescribeClusterInput{
            Name: aws.String(desired.Name),
        }, 15*time.Minute); err != nil {
            return fmt.Errorf("waiting for cluster active: %w", err)
        }

        return nil
    }

    // Cluster exists, check if update needed
    actual := describeOut.Cluster
    if *actual.Version != desired.KubernetesVersion {
        slog.InfoContext(ctx, "updating cluster version",
            "from", *actual.Version,
            "to", desired.KubernetesVersion)

        _, err := p.eksClient.UpdateClusterVersion(ctx, &eks.UpdateClusterVersionInput{
            Name:    aws.String(desired.Name),
            Version: aws.String(desired.KubernetesVersion),
        })

        return err
    }

    slog.InfoContext(ctx, "cluster up to date", "name", desired.Name)
    return nil
}
```

**GCP Example (GKE Cluster):**

```go
import (
    container "cloud.google.com/go/container/apiv1"
    containerpb "cloud.google.com/go/container/apiv1/containerpb"
)

func (p *GCPProvider) ensureGKECluster(ctx context.Context, desired ClusterSpec) error {
    ctx, span := tracer.Start(ctx, "ensureGKECluster")
    defer span.End()

    client, err := container.NewClusterManagerClient(ctx)
    if err != nil {
        return fmt.Errorf("creating GKE client: %w", err)
    }
    defer client.Close()

    clusterName := fmt.Sprintf("projects/%s/locations/%s/clusters/%s",
        p.projectID, desired.Region, desired.Name)

    // Check if cluster exists
    getReq := &containerpb.GetClusterRequest{
        Name: clusterName,
    }

    _, err = client.GetCluster(ctx, getReq)
    if err != nil {
        // Cluster doesn't exist, create it
        slog.InfoContext(ctx, "creating GKE cluster", "name", desired.Name)

        createReq := &containerpb.CreateClusterRequest{
            Parent: fmt.Sprintf("projects/%s/locations/%s", p.projectID, desired.Region),
            Cluster: &containerpb.Cluster{
                Name:             desired.Name,
                InitialNodeCount: desired.NodeCount,
                NodeConfig: &containerpb.NodeConfig{
                    MachineType: desired.MachineType,
                    OauthScopes: []string{
                        "https://www.googleapis.com/auth/cloud-platform",
                    },
                },
            },
        }

        op, err := client.CreateCluster(ctx, createReq)
        if err != nil {
            return fmt.Errorf("creating cluster: %w", err)
        }

        // Wait for operation to complete
        // (GKE operations are eventually consistent)
        slog.InfoContext(ctx, "waiting for cluster creation", "operation", op.Name)

        return nil
    }

    slog.InfoContext(ctx, "cluster exists", "name", desired.Name)
    return nil
}
```

**Kubernetes Example (Namespace):**

```go
import (
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func (k *K8sManager) ensureNamespace(ctx context.Context, name string, labels map[string]string) error {
    ctx, span := tracer.Start(ctx, "ensureNamespace")
    defer span.End()

    // Check if namespace exists
    _, err := k.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})

    if err != nil {
        // Namespace doesn't exist, create it
        slog.InfoContext(ctx, "creating namespace", "name", name)

        ns := &corev1.Namespace{
            ObjectMeta: metav1.ObjectMeta{
                Name:   name,
                Labels: labels,
            },
        }

        _, err := k.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
        return err
    }

    // Namespace exists, check if labels need updating
    // (implementation omitted for brevity)

    return nil
}
```

### 5.5 Error Handling and Rollback

**Partial Failure Strategy:**

- NIC attempts to apply all changes
- Failures are logged but don't stop entire deployment
- User can re-run `nic deploy` to retry failures

**Automatic Rollback:**

- For critical resources (VPC, cluster), rollback on failure
- For non-critical resources (dashboards), log error and continue
- User can manually rollback via `nic destroy` if needed

**Error Types:**

```go
type InfrastructureError struct {
    Resource string
    Action   string // "create", "update", "delete"
    Cause    error
    Retryable bool
}
```

### 5.6 Convergence and Eventual Consistency

**Cloud Provider Delays:**

- EKS cluster creation: 10-15 minutes
- GKE cluster creation: 5-10 minutes
- AKS cluster creation: 10-15 minutes
- Node pools: 3-5 minutes

**NIC Handling:**

- Uses waiter patterns (e.g., `eks.NewClusterActiveWaiter`)
- Polls with exponential backoff
- Timeout after reasonable period (15 minutes for clusters)
- Records intermediate state (e.g., "cluster creating")

**Eventual Consistency:**

- Some resources (LoadBalancers, DNS) propagate slowly
- NIC records expected state immediately
- Health checks verify actual availability
- Retries if health checks fail

---
