# Stateless Operation & Resource Discovery

## 6.1 Stateless Architecture Philosophy

**Core Principle:** NIC does not maintain state files. Instead, it queries cloud provider APIs and Kubernetes APIs to discover the current state of infrastructure on every run.

**Why Stateless:**

| Traditional State-Based | NIC Stateless Approach |
|------------------------|------------------------|
| ❌ State files can become out of sync with reality | ✅ Always queries actual cloud state |
| ❌ State corruption causes outages | ✅ No state to corrupt |
| ❌ Concurrent operations require locking | ✅ Eventually consistent (cloud APIs handle conflicts) |
| ❌ State backends add complexity | ✅ No backends needed |
| ❌ State migration on version upgrades | ✅ No migration needed |
| ❌ Manual drift detection | ✅ Automatic drift detection every run |
| ✅ Faster (no cloud API queries) | ⚠️ Slower (queries on every run) |
| ✅ Works offline for planning | ⚠️ Requires cloud access |

**Trade-off Accepted:** Slightly slower execution (~30-60 seconds for cloud API queries) in exchange for always-accurate state and zero state management complexity.

**How It Works:**
```
Every `nic deploy` run:
1. Parse nebari-config.yaml (desired state)
2. Query cloud APIs for resources with NIC tags (actual state)
3. Compare desired vs actual
4. Apply changes to reconcile
5. Done (no state file written)
```

---

## 6.2 Resource Tagging Strategy

**All resources created by NIC are tagged for discovery and ownership tracking.**

### 6.2.1 Standard Tag Schema

**Required Tags (All Resources):**
```go
type ResourceTags struct {
    // Identifies resource as NIC-managed
    ManagedBy string `tag:"nic.nebari.dev/managed-by" value:"nic"`

    // Cluster name from nebari-config.yaml
    ClusterName string `tag:"nic.nebari.dev/cluster-name" value:"nebari-prod"`

    // Resource type for filtering
    ResourceType string `tag:"nic.nebari.dev/resource-type" value:"vpc|cluster|node-pool|storage|..."`

    // NIC version that created the resource
    NICVersion string `tag:"nic.nebari.dev/version" value:"1.0.0"`

    // Configuration hash for detecting config changes
    ConfigHash string `tag:"nic.nebari.dev/config-hash" value:"sha256:abc123..."`
}
```

**Optional Tags:**
```go
type OptionalTags struct {
    // Node pool name (for node pools only)
    NodePoolName string `tag:"nic.nebari.dev/node-pool" value:"general"`

    // Environment (if specified in config)
    Environment string `tag:"nic.nebari.dev/environment" value:"prod|staging|dev"`

    // User-defined tags from config
    CustomTags map[string]string
}
```

### 6.2.2 Tag Examples by Resource Type

**AWS Examples:**

**VPC:**
```go
tags := map[string]string{
    "nic.nebari.dev/managed-by":     "nic",
    "nic.nebari.dev/cluster-name":   "nebari-prod",
    "nic.nebari.dev/resource-type":  "vpc",
    "nic.nebari.dev/version":        "1.0.0",
    "nic.nebari.dev/config-hash":    "sha256:abc123...",
    "Name":                          "nebari-prod-vpc",
}

// Create VPC with tags
vpcOutput, err := ec2Client.CreateVpc(ctx, &ec2.CreateVpcInput{
    CidrBlock: aws.String("10.0.0.0/16"),
    TagSpecifications: []types.TagSpecification{{
        ResourceType: types.ResourceTypeVpc,
        Tags:         awsTags(tags),
    }},
})
```

**EKS Cluster:**
```go
tags := map[string]string{
    "nic.nebari.dev/managed-by":     "nic",
    "nic.nebari.dev/cluster-name":   "nebari-prod",
    "nic.nebari.dev/resource-type":  "eks-cluster",
    "nic.nebari.dev/version":        "1.0.0",
    "nic.nebari.dev/config-hash":    "sha256:abc123...",
}

_, err := eksClient.CreateCluster(ctx, &eks.CreateClusterInput{
    Name: aws.String("nebari-prod"),
    // ... other params ...
    Tags: tags,
})
```

**Node Pool (EKS Node Group):**
```go
tags := map[string]string{
    "nic.nebari.dev/managed-by":     "nic",
    "nic.nebari.dev/cluster-name":   "nebari-prod",
    "nic.nebari.dev/resource-type":  "node-pool",
    "nic.nebari.dev/node-pool":      "general",
    "nic.nebari.dev/version":        "1.0.0",
    "nic.nebari.dev/config-hash":    "sha256:def456...",
}

_, err := eksClient.CreateNodegroup(ctx, &eks.CreateNodegroupInput{
    ClusterName:   aws.String("nebari-prod"),
    NodegroupName: aws.String("general"),
    // ... other params ...
    Tags: tags,
})
```

**GCP Examples:**

**GKE Cluster:**
```go
labels := map[string]string{
    "nic_nebari_dev_managed-by":    "nic",
    "nic_nebari_dev_cluster-name":  "nebari-prod",
    "nic_nebari_dev_resource-type": "gke-cluster",
    "nic_nebari_dev_version":       "1-0-0", // GCP labels don't allow dots
    "nic_nebari_dev_config-hash":   "sha256-abc123",
}

cluster := &containerpb.Cluster{
    Name:   "nebari-prod",
    // ... other params ...
    ResourceLabels: labels,
}
```

**Note:** GCP uses "labels" instead of "tags", and has restrictions (no dots, lowercase only). NIC converts tag names appropriately.

### 6.2.3 Config Hash Calculation

**Purpose:** Detect configuration changes without comparing all fields.

```go
func calculateConfigHash(config *Config) (string, error) {
    // Serialize config to canonical JSON
    canonical, err := json.Marshal(config)
    if err != nil {
        return "", err
    }

    // Calculate SHA-256 hash
    hash := sha256.Sum256(canonical)
    return fmt.Sprintf("sha256:%x", hash[:8]), nil // First 8 bytes for brevity
}
```

**Usage:**
- Tag all resources with config hash
- On subsequent runs, if config hash differs → configuration changed → may need resource updates
- If config hash matches → configuration unchanged → only check for drift

---

## 6.3 Resource Discovery

**NIC discovers existing infrastructure by querying cloud APIs with tag filters.**

### 6.3.1 Discovery Flow

```go
func (p *AWSProvider) DiscoverInfrastructure(ctx context.Context, clusterName string) (*DiscoveredState, error) {
    ctx, span := tracer.Start(ctx, "DiscoverInfrastructure")
    defer span.End()

    slog.InfoContext(ctx, "discovering existing infrastructure", "cluster", clusterName)

    discovered := &DiscoveredState{
        ClusterName: clusterName,
    }

    // Discover VPC
    vpc, err := p.discoverVPC(ctx, clusterName)
    if err != nil {
        return nil, fmt.Errorf("discovering VPC: %w", err)
    }
    discovered.VPC = vpc

    // Discover EKS Cluster
    cluster, err := p.discoverEKSCluster(ctx, clusterName)
    if err != nil {
        return nil, fmt.Errorf("discovering EKS cluster: %w", err)
    }
    discovered.Cluster = cluster

    // Discover Node Pools
    nodePools, err := p.discoverNodePools(ctx, clusterName)
    if err != nil {
        return nil, fmt.Errorf("discovering node pools: %w", err)
    }
    discovered.NodePools = nodePools

    // Discover Storage (EFS)
    storage, err := p.discoverStorage(ctx, clusterName)
    if err != nil {
        return nil, fmt.Errorf("discovering storage: %w", err)
    }
    discovered.Storage = storage

    slog.InfoContext(ctx, "infrastructure discovery complete",
        "vpc_found", discovered.VPC != nil,
        "cluster_found", discovered.Cluster != nil,
        "node_pools_found", len(discovered.NodePools),
    )

    return discovered, nil
}
```

### 6.3.2 Discovery Examples

**Discover VPC (AWS):**
```go
func (p *AWSProvider) discoverVPC(ctx context.Context, clusterName string) (*VPCState, error) {
    ctx, span := tracer.Start(ctx, "discoverVPC")
    defer span.End()

    // Query VPCs with NIC tags
    result, err := p.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
        Filters: []types.Filter{
            {
                Name:   aws.String("tag:nic.nebari.dev/managed-by"),
                Values: []string{"nic"},
            },
            {
                Name:   aws.String("tag:nic.nebari.dev/cluster-name"),
                Values: []string{clusterName},
            },
            {
                Name:   aws.String("tag:nic.nebari.dev/resource-type"),
                Values: []string{"vpc"},
            },
        },
    })

    if err != nil {
        return nil, fmt.Errorf("querying VPCs: %w", err)
    }

    if len(result.Vpcs) == 0 {
        slog.InfoContext(ctx, "no VPC found for cluster", "cluster", clusterName)
        return nil, nil
    }

    if len(result.Vpcs) > 1 {
        return nil, fmt.Errorf("multiple VPCs found for cluster %s (expected 1, found %d)", clusterName, len(result.Vpcs))
    }

    vpc := result.Vpcs[0]

    // Extract config hash from tags
    configHash := getTagValue(vpc.Tags, "nic.nebari.dev/config-hash")

    return &VPCState{
        ID:         *vpc.VpcId,
        CIDR:       *vpc.CidrBlock,
        ConfigHash: configHash,
    }, nil
}
```

**Discover EKS Cluster:**
```go
func (p *AWSProvider) discoverEKSCluster(ctx context.Context, clusterName string) (*ClusterState, error) {
    ctx, span := tracer.Start(ctx, "discoverEKSCluster")
    defer span.End()

    // EKS doesn't support tag filtering in ListClusters, so we check by name
    describeOutput, err := p.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
        Name: aws.String(clusterName),
    })

    if err != nil {
        var notFoundErr *types.ResourceNotFoundException
        if errors.As(err, &notFoundErr) {
            slog.InfoContext(ctx, "no EKS cluster found", "cluster", clusterName)
            return nil, nil
        }
        return nil, fmt.Errorf("querying EKS cluster: %w", err)
    }

    cluster := describeOutput.Cluster

    // Verify it's NIC-managed by checking tags
    if cluster.Tags == nil || cluster.Tags["nic.nebari.dev/managed-by"] != "nic" {
        slog.WarnContext(ctx, "cluster exists but not NIC-managed", "cluster", clusterName)
        return nil, fmt.Errorf("cluster %s exists but is not managed by NIC", clusterName)
    }

    return &ClusterState{
        Name:       *cluster.Name,
        ARN:        *cluster.Arn,
        Endpoint:   *cluster.Endpoint,
        Version:    *cluster.Version,
        Status:     string(cluster.Status),
        ConfigHash: cluster.Tags["nic.nebari.dev/config-hash"],
    }, nil
}
```

**Discover Node Pools:**
```go
func (p *AWSProvider) discoverNodePools(ctx context.Context, clusterName string) ([]NodePoolState, error) {
    ctx, span := tracer.Start(ctx, "discoverNodePools")
    defer span.End()

    // List all node groups for cluster
    listOutput, err := p.eksClient.ListNodegroups(ctx, &eks.ListNodegroupsInput{
        ClusterName: aws.String(clusterName),
    })

    if err != nil {
        return nil, fmt.Errorf("listing node groups: %w", err)
    }

    var nodePools []NodePoolState

    for _, nodeGroupName := range listOutput.Nodegroups {
        // Describe to get details and tags
        describeOutput, err := p.eksClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
            ClusterName:   aws.String(clusterName),
            NodegroupName: aws.String(nodeGroupName),
        })

        if err != nil {
            slog.WarnContext(ctx, "failed to describe node group", "nodegroup", nodeGroupName, "error", err)
            continue
        }

        ng := describeOutput.Nodegroup

        // Only include NIC-managed node groups
        if ng.Tags == nil || ng.Tags["nic.nebari.dev/managed-by"] != "nic" {
            slog.InfoContext(ctx, "skipping non-NIC-managed node group", "nodegroup", nodeGroupName)
            continue
        }

        nodePools = append(nodePools, NodePoolState{
            Name:         *ng.NodegroupName,
            ARN:          *ng.NodegroupArn,
            InstanceType: ng.InstanceTypes[0], // Simplified: assumes single instance type
            MinSize:      int(*ng.ScalingConfig.MinSize),
            MaxSize:      int(*ng.ScalingConfig.MaxSize),
            DesiredSize:  int(*ng.ScalingConfig.DesiredSize),
            Status:       string(ng.Status),
            ConfigHash:   ng.Tags["nic.nebari.dev/config-hash"],
        })
    }

    slog.InfoContext(ctx, "discovered node pools", "count", len(nodePools))
    return nodePools, nil
}
```

### 6.3.3 Kubernetes Resource Discovery

**NIC also discovers Kubernetes resources (namespaces, storage classes, etc.) via Kubernetes API:**

```go
func (k *K8sDiscovery) DiscoverKubernetesResources(ctx context.Context) (*K8sState, error) {
    ctx, span := tracer.Start(ctx, "DiscoverKubernetesResources")
    defer span.End()

    state := &K8sState{}

    // Discover namespaces with NIC labels
    namespaces, err := k.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
        LabelSelector: "nic.nebari.dev/managed-by=nic",
    })

    if err != nil {
        return nil, fmt.Errorf("listing namespaces: %w", err)
    }

    for _, ns := range namespaces.Items {
        state.Namespaces = append(state.Namespaces, NamespaceState{
            Name:   ns.Name,
            Labels: ns.Labels,
        })
    }

    // Discover storage classes
    storageClasses, err := k.clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{
        LabelSelector: "nic.nebari.dev/managed-by=nic",
    })

    if err != nil {
        return nil, fmt.Errorf("listing storage classes: %w", err)
    }

    for _, sc := range storageClasses.Items {
        state.StorageClasses = append(state.StorageClasses, StorageClassState{
            Name:        sc.Name,
            Provisioner: sc.Provisioner,
            IsDefault:   sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true",
        })
    }

    // Discover ArgoCD Applications (foundational software)
    argoApps, err := k.discoverArgoApplications(ctx)
    if err != nil {
        slog.WarnContext(ctx, "failed to discover ArgoCD applications", "error", err)
    } else {
        state.ArgoApplications = argoApps
    }

    return state, nil
}
```

---

## 6.4 Stateless Reconciliation

**Reconciliation without state files:**

```go
func (p *AWSProvider) Reconcile(ctx context.Context, desired *Config) error {
    ctx, span := tracer.Start(ctx, "Reconcile")
    defer span.End()

    slog.InfoContext(ctx, "starting reconciliation", "cluster", desired.Name)

    // 1. Discover actual state from cloud APIs
    actual, err := p.DiscoverInfrastructure(ctx, desired.Name)
    if err != nil {
        return fmt.Errorf("discovering actual state: %w", err)
    }

    // 2. Calculate config hash for desired state
    desiredConfigHash, err := calculateConfigHash(desired)
    if err != nil {
        return fmt.Errorf("calculating config hash: %w", err)
    }

    // 3. Reconcile VPC
    if err := p.reconcileVPC(ctx, desired, actual.VPC, desiredConfigHash); err != nil {
        return fmt.Errorf("reconciling VPC: %w", err)
    }

    // 4. Reconcile EKS Cluster
    if err := p.reconcileCluster(ctx, desired, actual.Cluster, desiredConfigHash); err != nil {
        return fmt.Errorf("reconciling cluster: %w", err)
    }

    // 5. Reconcile Node Pools
    if err := p.reconcileNodePools(ctx, desired, actual.NodePools, desiredConfigHash); err != nil {
        return fmt.Errorf("reconciling node pools: %w", err)
    }

    // 6. Reconcile Storage
    if err := p.reconcileStorage(ctx, desired, actual.Storage, desiredConfigHash); err != nil {
        return fmt.Errorf("reconciling storage: %w", err)
    }

    slog.InfoContext(ctx, "reconciliation complete", "cluster", desired.Name)
    return nil
}
```

### 6.4.1 Reconcile VPC Example

```go
func (p *AWSProvider) reconcileVPC(ctx context.Context, desired *Config, actual *VPCState, desiredHash string) error {
    ctx, span := tracer.Start(ctx, "reconcileVPC")
    defer span.End()

    // Case 1: VPC doesn't exist → Create
    if actual == nil {
        slog.InfoContext(ctx, "VPC not found, creating", "cidr", desired.Provider.AWS.VPC.CIDR)
        return p.createVPC(ctx, desired, desiredHash)
    }

    // Case 2: VPC exists with same config hash → No action needed
    if actual.ConfigHash == desiredHash {
        slog.InfoContext(ctx, "VPC up to date", "vpc_id", actual.ID)
        return nil
    }

    // Case 3: VPC exists with different config hash → Update or Replace
    slog.InfoContext(ctx, "VPC configuration changed", "vpc_id", actual.ID)

    // VPC CIDR cannot be changed (AWS limitation)
    if actual.CIDR != desired.Provider.AWS.VPC.CIDR {
        return fmt.Errorf("VPC CIDR change detected (%s → %s), which requires recreation. Manual intervention required.",
            actual.CIDR, desired.Provider.AWS.VPC.CIDR)
    }

    // Update VPC tags (config hash)
    return p.updateVPCTags(ctx, actual.ID, desired, desiredHash)
}
```

### 6.4.2 Reconcile Node Pools Example

```go
func (p *AWSProvider) reconcileNodePools(ctx context.Context, desired *Config, actual []NodePoolState, desiredHash string) error {
    ctx, span := tracer.Start(ctx, "reconcileNodePools")
    defer span.End()

    // Build maps for comparison
    desiredPools := make(map[string]NodePoolConfig)
    for _, np := range desired.Kubernetes.NodePools {
        desiredPools[np.Name] = np
    }

    actualPools := make(map[string]NodePoolState)
    for _, np := range actual {
        actualPools[np.Name] = np
    }

    // Find node pools to create (in desired but not actual)
    for name, desiredNP := range desiredPools {
        if _, exists := actualPools[name]; !exists {
            slog.InfoContext(ctx, "creating node pool", "name", name)
            if err := p.createNodePool(ctx, desired.Name, desiredNP, desiredHash); err != nil {
                return fmt.Errorf("creating node pool %s: %w", name, err)
            }
        }
    }

    // Find node pools to update (in both, but config changed)
    for name, actualNP := range actualPools {
        desiredNP, exists := desiredPools[name]
        if !exists {
            // Node pool in actual but not desired → orphaned
            slog.WarnContext(ctx, "orphaned node pool detected", "name", name)
            // Decision: Don't auto-delete (safety), log warning
            continue
        }

        // Check if update needed
        if actualNP.ConfigHash != desiredHash || p.nodePoolDiffers(actualNP, desiredNP) {
            slog.InfoContext(ctx, "updating node pool", "name", name)
            if err := p.updateNodePool(ctx, desired.Name, desiredNP, actualNP, desiredHash); err != nil {
                return fmt.Errorf("updating node pool %s: %w", name, err)
            }
        } else {
            slog.InfoContext(ctx, "node pool up to date", "name", name)
        }
    }

    return nil
}
```

---

## 6.5 Drift Detection

**Drift detection is automatic on every run** (no separate command needed).

### 6.5.1 Automatic Drift Detection

```go
func (p *AWSProvider) detectDrift(desired *Config, actual *DiscoveredState) *DriftReport {
    report := &DriftReport{
        Timestamp: time.Now(),
    }

    // Check VPC drift
    if actual.VPC != nil && actual.VPC.CIDR != desired.Provider.AWS.VPC.CIDR {
        report.Drifts = append(report.Drifts, Drift{
            Resource: "VPC",
            Field:    "CIDR",
            Expected: desired.Provider.AWS.VPC.CIDR,
            Actual:   actual.VPC.CIDR,
            Severity: "critical",
            Cause:    "manual modification outside NIC",
        })
    }

    // Check cluster version drift
    if actual.Cluster != nil && actual.Cluster.Version != desired.Kubernetes.Version {
        report.Drifts = append(report.Drifts, Drift{
            Resource: "EKS Cluster",
            Field:    "Kubernetes Version",
            Expected: desired.Kubernetes.Version,
            Actual:   actual.Cluster.Version,
            Severity: "warning",
            Cause:    "version upgrade performed outside NIC",
        })
    }

    // Check node pool drift
    for _, actualNP := range actual.NodePools {
        desiredNP := findNodePool(desired.Kubernetes.NodePools, actualNP.Name)
        if desiredNP == nil {
            report.Drifts = append(report.Drifts, Drift{
                Resource: fmt.Sprintf("Node Pool: %s", actualNP.Name),
                Field:    "Exists",
                Expected: "false",
                Actual:   "true",
                Severity: "info",
                Cause:    "node pool created outside NIC",
            })
            continue
        }

        if actualNP.MinSize != desiredNP.MinSize || actualNP.MaxSize != desiredNP.MaxSize {
            report.Drifts = append(report.Drifts, Drift{
                Resource: fmt.Sprintf("Node Pool: %s", actualNP.Name),
                Field:    "Scaling Config",
                Expected: fmt.Sprintf("min=%d, max=%d", desiredNP.MinSize, desiredNP.MaxSize),
                Actual:   fmt.Sprintf("min=%d, max=%d", actualNP.MinSize, actualNP.MaxSize),
                Severity: "warning",
                Cause:    "scaling configuration changed outside NIC",
            })
        }
    }

    report.DriftsDetected = len(report.Drifts)
    return report
}
```

### 6.5.2 Drift Reporting

```bash
$ nic deploy -f nebari-config.yaml

Discovering infrastructure...
✓ VPC found: vpc-abc123
✓ EKS Cluster found: nebari-prod (version 1.29)
✓ Node Pools found: 3 (general, compute, gpu)

Drift Detection:
⚠️  2 drifts detected:

1. [WARNING] Node Pool: general - Scaling Config
   Expected: min=3, max=10
   Actual:   min=5, max=10
   Cause: scaling configuration changed outside NIC

2. [INFO] Node Pool: experimental - Exists
   Expected: false
   Actual:   true
   Cause: node pool created outside NIC

Reconciling infrastructure...
→ Updating node pool 'general' (scaling config)
→ Skipping orphaned node pool 'experimental' (not in config)

Infrastructure reconciled successfully.
```

---

## 6.6 Benefits of Stateless Operation

**Advantages:**

1. **✅ No State Drift**: Actual cloud state is always the source of truth
2. **✅ No State Corruption**: Can't corrupt what doesn't exist
3. **✅ No State Backends**: No S3 buckets, DynamoDB tables, or locking to manage
4. **✅ No State Migration**: Version upgrades don't require state format changes
5. **✅ Automatic Drift Detection**: Every run compares desired vs actual
6. **✅ Simpler Mental Model**: Query, compare, reconcile - that's it
7. **✅ Easier Debugging**: `nic status` just queries cloud APIs (no state to inspect)
8. **✅ No Lock Contention**: Cloud APIs handle concurrent operations gracefully

**Challenges:**

1. **⚠️ Slower Performance**: Cloud API queries add 30-60 seconds per run
2. **⚠️ Requires Cloud Access**: Can't plan changes offline
3. **⚠️ API Rate Limits**: High-frequency runs could hit rate limits (rare)
4. **⚠️ Eventual Consistency**: Cloud APIs may lag (NIC handles this with retries)

**Mitigation for Challenges:**

```go
// 1. Caching for performance (optional, in-memory only, short TTL)
type InMemoryCache struct {
    discoveries map[string]*DiscoveredState
    ttl         time.Duration
    mu          sync.RWMutex
}

// 2. Exponential backoff for rate limits
func (p *AWSProvider) discoverWithRetry(ctx context.Context, clusterName string) (*DiscoveredState, error) {
    return retry.Do(
        func() (*DiscoveredState, error) {
            return p.DiscoverInfrastructure(ctx, clusterName)
        },
        retry.Attempts(3),
        retry.Delay(1*time.Second),
        retry.DelayType(retry.BackOffDelay),
    )
}

// 3. Eventual consistency handling
func (p *AWSProvider) waitForResourceReady(ctx context.Context, resourceID string, maxWait time.Duration) error {
    deadline := time.Now().Add(maxWait)
    for time.Now().Before(deadline) {
        // Check resource status
        if ready, err := p.isResourceReady(ctx, resourceID); err == nil && ready {
            return nil
        }
        time.Sleep(5 * time.Second)
    }
    return fmt.Errorf("resource %s not ready within %s", resourceID, maxWait)
}
```

---

## 6.7 Comparison: Stateless vs State-Based

| Aspect | Stateless (NIC) | State-Based (Terraform) |
|--------|-----------------|-------------------------|
| **State Storage** | None | S3/GCS/Azure Blob/Local |
| **State Locking** | Not needed | DynamoDB/Cloud Storage/Blob Lease |
| **Drift Detection** | Automatic (every run) | Manual (`terraform plan`) |
| **State Corruption Risk** | None | Possible (locks fail, corrupted JSON) |
| **Concurrent Operations** | Eventual consistency | Locking prevents concurrency |
| **Performance** | Slower (+30-60s queries) | Faster (cached state) |
| **Offline Planning** | Not possible | Possible |
| **Mental Model** | Simple (query, compare, act) | Complex (state, locking, backends) |
| **Version Upgrades** | No migration | State format migration |
| **Debugging** | Simple (query cloud) | Complex (inspect state file) |

---

## 6.8 Implementation Example

**Complete Stateless Deploy:**

```go
func (cli *CLI) Deploy(ctx context.Context, configPath string) error {
    ctx, span := tracer.Start(ctx, "CLI.Deploy")
    defer span.End()

    // 1. Parse configuration (desired state)
    config, err := config.ParseFile(configPath)
    if err != nil {
        return fmt.Errorf("parsing config: %w", err)
    }

    // 2. Get provider for cloud
    provider, err := provider.GetProvider(config.Provider.Type, config)
    if err != nil {
        return fmt.Errorf("getting provider: %w", err)
    }

    // 3. Discover actual state (NO STATE FILE READ)
    slog.InfoContext(ctx, "discovering infrastructure")
    actual, err := provider.DiscoverInfrastructure(ctx, config.Name)
    if err != nil {
        return fmt.Errorf("discovering infrastructure: %w", err)
    }

    // 4. Detect drift
    driftReport := provider.DetectDrift(config, actual)
    if driftReport.DriftsDetected > 0 {
        slog.WarnContext(ctx, "drift detected", "count", driftReport.DriftsDetected)
        // Log drift details
        for _, drift := range driftReport.Drifts {
            slog.WarnContext(ctx, "drift",
                "resource", drift.Resource,
                "field", drift.Field,
                "expected", drift.Expected,
                "actual", drift.Actual,
            )
        }
    }

    // 5. Reconcile (create/update/delete resources)
    slog.InfoContext(ctx, "reconciling infrastructure")
    if err := provider.Reconcile(ctx, config); err != nil {
        return fmt.Errorf("reconciling infrastructure: %w", err)
    }

    // 6. Wait for foundational software
    slog.InfoContext(ctx, "waiting for foundational software")
    if err := cli.waitForFoundationalSoftware(ctx, config); err != nil {
        return fmt.Errorf("waiting for foundational software: %w", err)
    }

    // 7. Done (NO STATE FILE WRITE)
    slog.InfoContext(ctx, "deployment complete")
    return nil
}
```

**Status Command (also stateless):**

```go
func (cli *CLI) Status(ctx context.Context, configPath string) error {
    ctx, span := tracer.Start(ctx, "CLI.Status")
    defer span.End()

    config, err := config.ParseFile(configPath)
    if err != nil {
        return fmt.Errorf("parsing config: %w", err)
    }

    provider, err := provider.GetProvider(config.Provider.Type, config)
    if err != nil {
        return fmt.Errorf("getting provider: %w", err)
    }

    // Query cloud APIs for status (NO STATE FILE)
    actual, err := provider.DiscoverInfrastructure(ctx, config.Name)
    if err != nil {
        return fmt.Errorf("discovering infrastructure: %w", err)
    }

    // Display status
    fmt.Printf("Cluster: %s\n", config.Name)
    fmt.Printf("Provider: %s (%s)\n", config.Provider.Type, config.Provider.Region)

    if actual.VPC != nil {
        fmt.Printf("VPC: %s (%s)\n", actual.VPC.ID, actual.VPC.CIDR)
    }

    if actual.Cluster != nil {
        fmt.Printf("Cluster: %s (version %s, status: %s)\n", actual.Cluster.Name, actual.Cluster.Version, actual.Cluster.Status)
    }

    fmt.Printf("Node Pools: %d\n", len(actual.NodePools))
    for _, np := range actual.NodePools {
        fmt.Printf("  - %s: %s (min=%d, max=%d, desired=%d, status=%s)\n",
            np.Name, np.InstanceType, np.MinSize, np.MaxSize, np.DesiredSize, np.Status)
    }

    return nil
}
```

---

**Summary:** NIC's stateless architecture simplifies operations by eliminating state management complexity. Every run queries actual cloud state, detects drift automatically, and reconciles to match desired configuration - no state files, no backends, no locks, no corruption risks.
