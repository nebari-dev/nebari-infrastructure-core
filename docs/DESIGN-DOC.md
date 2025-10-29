# Software Design Document: Nebari Infrastructure Core

**Project Name:** Nebari Infrastructure Core (NIC)  
**Version:** 1.0  
**Last Updated:** 2025-01-27  
**Status:** Draft  
**Authors:** [Your Team]  
**Reviewers:** [To be assigned]  
**Context:** Replacement for Nebari infrastructure and kubernetes-initialize stages

---

## Table of Contents

1. [Introduction](#1-introduction)
2. [System Overview](#2-system-overview)
3. [Goals and Non-Goals](#3-goals-and-non-goals)
4. [Key Architectural Decisions](#4-key-architectural-decisions)
5. [Declarative Infrastructure with Native SDKs](#5-declarative-infrastructure-with-native-sdks)
6. [State Management Design](#6-state-management-design)
7. [Configuration Compatibility Strategy](#7-configuration-compatibility-strategy)
8. [Provider Porting Strategy](#8-provider-porting-strategy)
9. [Nebari Integration Design](#9-nebari-integration-design)
10. [Migration Strategy](#10-migration-strategy)
11. [Testing Strategy](#11-testing-strategy)
12. [Timeline and Milestones](#12-timeline-and-milestones)
13. [Open Questions](#13-open-questions)
14. [Appendix](#14-appendix)

---

## 1. Introduction

### 1.1 Purpose

This document describes the architectural decisions for Nebari Infrastructure Core (NIC), a standalone Go library and CLI that will replace Nebari's Terraform-based infrastructure stages (02-infrastructure and 03-kubernetes-initialize). NIC maintains configuration compatibility with existing `nebari-config.yaml` files while reimplementing complete infrastructure provisioning—from cloud resources through Kubernetes initialization—using native cloud SDKs with declarative semantics and custom state management.

### 1.2 Core Design Principles

1. **Configuration Compatibility**: Existing `nebari-config.yaml` files work without modification
2. **Declarative Infrastructure**: Declare desired state, NIC reconciles to match
3. **Native SDKs**: Use cloud provider SDKs directly, not Terraform providers
4. **Simple State Management**: Custom state format optimized for our use case
5. **Management Transfer**: v1.0 focuses on taking over existing infrastructure, not improving it
6. **Compiled Plugins with Explicit Registration**: All providers built into binary
7. **Standalone Reusability**: Usable independently of Nebari

### 1.3 Scope

**In Scope:**
- Cloud infrastructure provisioning (VPC, Kubernetes clusters, node pools, storage)
- Kubernetes initialization (namespaces, RBAC, storage classes, service accounts)
- Supported providers: AWS, GCP, Azure, and Local (K3s)
- Configuration parsing for `nebari-config.yaml` format
- Custom state management for all infrastructure
- Import from Terraform stages 02 and 03
- Python integration layer for Nebari
- Outputs compatible with application stages (04+)

**Out of Scope:**
- Terraform state backend management (stays in stage 01)
- Application stages (04+: Ingress, Keycloak, JupyterHub, Dask, monitoring)
- Infrastructure improvements (EBS → EFS, etc.) - deferred to v2.0+
- Breaking changes to nebari-config.yaml structure
- Complete replacement of all Terraform in Nebari

### 1.4 Version 1.0 Philosophy: Management Transfer Only

**Critical Decision**: v1.0 focuses exclusively on transferring management from Terraform to NIC without changing existing infrastructure.

**Rationale:**
- Minimize risk during transition
- Prove NIC can reliably manage existing infrastructure
- Avoid data migration complexity in v1.0
- Establish trust before introducing improvements
- Clear separation: v1.0 = transfer, v2.0+ = improve

**What This Means:**
```
Current Infrastructure (Terraform):
├── EBS volumes
├── EKS cluster 1.28
├── m5.xlarge instances
└── Existing K8s resources

After NIC v1.0:
├── Same EBS volumes (no change)
├── Same EKS cluster 1.28 (no change)
├── Same m5.xlarge instances (no change)
└── Same K8s resources (no change)
  
Only difference: Managed by NIC instead of Terraform
```

**v2.0+ Can Then Introduce:**
- Storage improvements (EBS → EFS)
- Better instance types
- Advanced features
- Performance optimizations

### 1.5 Relationship with Terraform

**What Changes:**
```
Current Nebari Stages:
├── 01-terraform-state/        # ← STAYS IN TERRAFORM
├── 02-infrastructure/         # ← REPLACED BY NIC
├── 03-kubernetes-initialize/  # ← REPLACED BY NIC
├── 04-kubernetes-ingress/     # ← STAYS IN TERRAFORM
├── 05-kubernetes-keycloak/    # ← STAYS IN TERRAFORM
├── 06+                        # ← STAY IN TERRAFORM
```

**Logical Boundary:**
- **Stage 01 (Terraform)**: State backend setup
- **Stages 02-03 (NIC)**: Complete infrastructure - cloud resources + Kubernetes initialization
- **Stages 04+ (Terraform)**: Applications and services running on the cluster

**Why This Boundary:**

| Consideration | Rationale |
|---------------|-----------|
| **Natural Division** | Infrastructure (NIC) vs Applications (Terraform) |
| **Storage Classes** | Cloud-provider-specific, belongs with cloud provisioning |
| **RBAC** | Foundational cluster setup, not application |
| **Single Responsibility** | NIC delivers "cluster ready for apps" |
| **Clean Handoff** | Stage 04 (Ingress) is where applications truly begin |

---

## 2. System Overview

### 2.1 Current State: Nebari's Infrastructure Stages

**Current Challenges with Stages 02-03:**

**Stage 02 (Infrastructure):**
1. **Complexity**: Multi-layered Terraform abstractions for cloud resources
2. **Performance**: Slow Terraform plan/apply cycles
3. **Debugging**: Difficult to trace issues through Terraform layers
4. **Provider Updates**: Dependency on Terraform provider releases

**Stage 03 (Kubernetes Initialize):**
1. **Cloud Coupling**: Storage classes tied to stage 02 decisions
2. **Fragmentation**: Split between cloud (stage 02) and K8s (stage 03) artificial
3. **Dependency Management**: Implicit dependencies between stages
4. **State Overhead**: Separate Terraform state for simple K8s resources

**What Stages 02-03 Currently Do:**

**Stage 02:**
- VPC/Network creation
- Kubernetes cluster provisioning (EKS/GKE/AKS/K3s)
- Node pool management
- Security groups and IAM roles
- Cloud storage provisioning
- Load balancer setup

**Stage 03:**
- Kubernetes namespaces
- RBAC (roles, role bindings, service accounts)
- Storage classes (referencing stage 02 storage)
- Resource quotas
- Network policies
- Priority classes

**What Gets Replaced:**
- `src/_nebari/stages/02-infrastructure/` - All Terraform cloud provisioning
- `src/_nebari/stages/03-kubernetes-initialize/` - All Terraform K8s initialization
- Provider-specific Terraform modules for both stages

**What Stays in Terraform:**
- Stage 01: Terraform state backend setup
- Stage 04+: All applications (Ingress, Keycloak, JupyterHub, Dask, monitoring)

### 2.2 Target Architecture

**Component Relationship:**
```
Nebari Python CLI
    ↓
Stage 01 (Terraform) - State Backend Setup
    ↓
Stages 02-03 (NIC) - Complete Infrastructure ← THIS PROJECT
    ├── Declarative Config (nebari-config.yaml)
    ├── Native Cloud SDKs (AWS/GCP/Azure)
    ├── Kubernetes client-go
    └── Custom State Management (JSON)
    ↓ outputs
Stage 04+ (Terraform) - Applications & Services
    ↓
Kubernetes Cluster
```

**NIC's Unified Workflow:**
1. Parse nebari-config.yaml (desired state)
2. Read NIC state file (current managed state)
3. Query cloud/K8s APIs (actual infrastructure state)
4. Calculate diff (desired vs actual)
5. Execute changes via native SDKs (declarative reconciliation)
6. Update state file
7. Generate outputs for application stages

**Key Integration Points:**

1. **NIC → Application Stages**
   - NIC generates single outputs.json
   - Application stages (04+) consume these outputs
   - Standard format for all cluster information

2. **State Isolation**
   - NIC maintains custom state for all infrastructure
   - Terraform maintains state for application resources
   - No shared state files

3. **Dependency Flow**
   - Stage 01 (TF) runs first → creates state backend
   - Stages 02-03 (NIC) run second → complete infrastructure setup
   - Stage 04+ (TF) run third → deploy applications

### 2.3 Why Combine Stages 02 and 03?

**Decision: Unified Infrastructure Management**

**Rationale:**
1. **Logical Cohesion**: Both stages deliver "infrastructure" vs "applications"
2. **Storage Classes**: Cloud-provider-specific configuration naturally belongs with cloud provisioning
3. **Single Transaction**: Infrastructure should be atomic - either fully ready or not
4. **Eliminates Inter-Stage Dependencies**: No artificial split requiring coordination
5. **Simplified State**: One state file for complete infrastructure
6. **Better Performance**: Single tool invocation vs two separate Terraform applies

**Natural Boundary:**
- **Before Stage 04**: Cluster exists, configured, ready for applications
- **Stage 04+**: Deploy services that run ON the cluster

**Examples Supporting This Boundary:**

| Resource | Stage | Why It Belongs in NIC |
|----------|-------|----------------------|
| EBS volumes | 02 (old) | Cloud resource |
| Storage Class referencing EBS | 03 (old) | Directly tied to cloud storage |
| Combined | NIC | Single coherent storage setup |
| | | |
| VPC | 02 (old) | Cloud network |
| Network Policies | 03 (old) | K8s network rules for VPC |
| Combined | NIC | Complete network configuration |
| | | |
| IAM Roles | 02 (old) | Cloud identity |
| Service Accounts | 03 (old) | K8s identity bound to IAM |
| Combined | NIC | Complete identity setup |

---

## 3. Goals and Non-Goals

### 3.1 Primary Goals

**v1.0 - Management Transfer:**
1. Import existing Terraform-managed infrastructure (stages 02-03)
2. Manage existing infrastructure without changes
3. Parse existing `nebari-config.yaml` without modifications
4. Support all four existing providers (AWS, GCP, Azure, Local)
5. Generate outputs compatible with application stages (04+)
6. Prove NIC can reliably manage what Terraform created
7. Integrate seamlessly with Nebari via subprocess
8. Zero data loss, zero infrastructure changes

**v1.x - Stabilization:**
1. Bug fixes and reliability improvements
2. Better error reporting
3. Performance optimization
4. Comprehensive testing coverage
5. Production hardening

**v2.0+ - Infrastructure Improvements:**
1. Storage improvements (EBS → EFS, etc.)
2. Advanced features (Karpenter, spot instances, etc.)
3. Multi-region support
4. External plugin system via RPC
5. Community-contributed providers

### 3.2 Explicit Non-Goals

**v1.0 Non-Goals:**
1. **No Infrastructure Changes**: Keep existing resources exactly as-is
2. **No Storage Migration**: Don't change EBS to EFS or similar
3. **No Instance Type Changes**: Keep existing instance types
4. **No K8s Version Upgrades**: Keep existing K8s version
5. **No New Features**: Only replicate what Terraform stages do
6. **No Terraform Replacement for Stages 01, 04+**: Keep Terraform for state backend and applications
7. **No Config Breaking Changes**: nebari-config.yaml structure is API contract
8. **No CGO**: Keep Go compilation simple and portable

**Why These Are Non-Goals for v1.0:**
- Risk reduction: Prove management transfer works first
- Data safety: No migration = no data loss risk
- Trust building: Users see NIC works before changes
- Complexity management: One thing at a time
- Clear milestone: v1.0 = management transfer, v2.0 = improvements

---

## 4. Key Architectural Decisions

### 4.1 Decision: Unified Infrastructure Stages (02 + 03)

**Decision**: Combine stages 02 (infrastructure) and 03 (kubernetes-initialize) into single NIC implementation.

**Rationale:**
- **Logical Cohesion**: Both deliver infrastructure, not applications
- **Storage Classes**: Cloud-specific configuration belongs with cloud provisioning
- **Atomicity**: Infrastructure should be "all ready" or "not ready"
- **Performance**: Single operation faster than two Terraform stages
- **State Simplicity**: One state file vs two Terraform states
- **Natural Boundary**: Applications begin at stage 04 (Ingress)

**Alternatives Considered:**

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **Replace stage 02 only** | Smaller scope | Artificial split, storage class disconnect | ❌ Rejected |
| **Replace stages 02-03** | Natural boundary, better performance | Slightly larger scope | ✅ **Chosen** |
| **Replace through stage 04** | Include ingress | Ingress is application, not infrastructure | ❌ Rejected |

### 4.2 Decision: Custom State Management (Not Terraform/OpenTofu)

**Decision**: Implement custom JSON-based state management optimized for our use case, not use Terraform/OpenTofu state.

**Rationale:**
- **Simpler for Our Use Case**: We're not building a general-purpose IaC tool
- **No Terraform Dependency**: Pure Go, single binary distribution
- **Cleaner Import**: Convert Terraform state to our format, done
- **State is Just Data**: Not complex for our focused scope
- **Optimized Format**: Only track what we need
- **Native SDK Focus**: Terraform state designed for HCL providers

**Alternatives Considered:**

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **OpenTofu state management** | Mature, tested | Complex, requires OpenTofu binary, designed for HCL providers | ❌ Rejected |
| **Pulumi state service** | Cloud-native | External dependency, overkill | ❌ Rejected |
| **Custom JSON state** | Simple, optimized for our needs | Need to implement (but not complex) | ✅ **Chosen** |

**What We're NOT Reinventing:**
- State locking: Use existing cloud primitives (DynamoDB, Cloud Storage metadata, Blob lease)
- State backends: Use cloud SDKs we already have (S3, GCS, Azure Blob)
- Drift detection: Query cloud APIs and compare (straightforward)

**What IS Simple for Our Use Case:**
- State format: Just resource IDs and metadata
- No dependency graphs: Natural resource ordering
- No provider schemas: We control the resources
- No HCL parsing: We own the config format

### 4.3 Decision: Declarative Semantics with Native SDKs

**Decision**: Use declarative approach (desired state reconciliation) with native cloud SDKs for execution.

**Key Principle**: "Declare what you want, NIC makes it happen"

**How It Works:**
```
User Config (nebari-config.yaml) → Desired State
    ↓
NIC State File → Current Managed State
    ↓
Cloud/K8s APIs → Actual Infrastructure State
    ↓
Diff Calculation → Changes Needed
    ↓
Native SDK Execution → Apply Changes
    ↓
State Update → Record New State
```

**Example:**
```yaml
# User declares: I want 3 node groups
node_groups:
  general: {instance_type: m5.xlarge, min: 1, max: 5}
  user: {instance_type: m5.2xlarge, min: 0, max: 10}
  worker: {instance_type: m5.xlarge, min: 0, max: 20}
```

```go
// NIC reconciliation
desired := parseConfig(yaml) // 3 node groups
current := readState()        // 2 node groups
actual := queryEKS()          // 2 node groups exist

diff := calculateDiff(desired, actual)
// diff = {Create: [worker]}

// Execute via native AWS SDK
eks.CreateNodeGroup(worker)

// Update state
saveState(desired)
```

**This Is Declarative:**
- User declares desired end state
- NIC calculates how to get there
- Idempotent: running again does nothing if already correct
- Convergence: keeps moving toward desired state

**This Is NOT Imperative:**
- Not "run these commands"
- Not "call these APIs in order"
- Just "make infrastructure match this config"

**Alternatives Considered:**

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **Imperative scripts** | Simple initially | Not idempotent, hard to maintain | ❌ Rejected |
| **Terraform wrapper** | Proven | Still using Terraform, defeats purpose | ❌ Rejected |
| **Declarative + Native SDKs** | Best of both worlds | Need reconciliation logic | ✅ **Chosen** |

### 4.4 Decision: v1.0 Management Transfer Only (No Infrastructure Changes)

**Decision**: v1.0 focuses exclusively on taking over management of existing infrastructure without making any changes to resources.

**Rationale:**
- **Risk Minimization**: Prove management transfer works before introducing changes
- **Data Safety**: No resource changes = no data migration = no data loss risk
- **Trust Building**: Users validate NIC reliability before trusting it with changes
- **Complexity Management**: One major change at a time
- **Clear Milestone**: Success criteria is "manages exactly what exists"

**What This Means in Practice:**

| Resource Type | Current (Terraform) | NIC v1.0 | NIC v2.0+ |
|--------------|---------------------|----------|-----------|
| **Storage** | EBS volumes | Same EBS volumes | Can migrate to EFS |
| **Instances** | m5.xlarge | Same m5.xlarge | Can recommend better types |
| **K8s Version** | 1.28 | Same 1.28 | Can upgrade |
| **Storage Classes** | "gp3" provisioner | Same "gp3" provisioner | Can improve |

**Import Behavior:**
```bash
nebari infrastructure import

# NIC detects:
# - EBS volumes (keeps them)
# - m5.xlarge instances (keeps them)
# - K8s 1.28 (keeps it)
# - Storage class "gp3" (keeps it)

# NIC creates state tracking these resources
# Future deploys: no changes unless user modifies config
```

**Config Interpretation for v1.0:**
```yaml
# This config in v1.0:
amazon_web_services:
  storage:
    type: ebs
    size: 100

# During import: "Keep existing EBS setup"
# After import: "Continue managing EBS"
# NOT: "Create new EFS and migrate"
```

**Benefits:**
- Users can test NIC with confidence
- No production impact beyond management transfer
- Rollback is simple (go back to Terraform)
- Foundation for v2.0 improvements

**v2.0+ Can Then Add:**
- Storage upgrade commands
- Instance type recommendations
- K8s version upgrades
- Performance improvements

### 4.5 Decision: Compiled Plugins with Explicit Registration

**Decision**: Use compiled plugins registered explicitly in `main()`, not blank imports or init() patterns.

**Rationale:**
- Clear visibility of included providers
- Simple to test and debug
- No global state magic
- Easy to understand dependency graph
- Follows modern Go best practices
- Future-proof for RPC migration

**Implementation:**
```go
// cmd/nic/main.go
func main() {
    registry := provider.NewRegistry()
    
    // Explicit registration
    registry.Register("aws", aws.NewProvider())
    registry.Register("gcp", gcp.NewProvider())
    registry.Register("azure", azure.NewProvider())
    registry.Register("local", local.NewProvider())
    
    app := cli.NewApp(registry)
    app.Run()
}
```

**Alternatives Considered:**

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **Blank imports + init()** | Familiar database/sql pattern | Hidden magic, global state | ❌ Rejected |
| **Runtime plugins (.so)** | Dynamic loading | Platform-specific, complex | ❌ Rejected |
| **RPC plugins (go-plugin)** | Language agnostic, isolated | IPC overhead | ⏰ Future phase (v2.0+) |
| **Explicit registration** | Clear, testable, simple | Slightly more verbose | ✅ **Chosen** |

### 4.6 Decision: Subprocess Integration with Nebari

**Decision**: Integrate with Nebari via subprocess calls, not CGO or embedded library.

**Rationale:**
- No CGO compilation complexity
- Clear process boundaries
- Easy debugging (can run NIC standalone)
- Standard Unix tool composition
- NIC remains fully independent
- Simpler error handling
- Can replace NIC binary without Nebari changes

**Communication Protocol:**
```
Nebari Python → subprocess → NIC binary
     ↓                           ↓
Parse JSON output ←─── stdout (structured data)
Log progress     ←─── stderr (human logs)
Check status     ←─── exit code (success/failure)
```

**Alternatives Considered:**

| Approach | Pros | Cons | Decision |
|----------|------|------|----------|
| **CGO embedded library** | Direct function calls | Complex builds, tight coupling | ❌ Rejected |
| **Python bindings** | Native Python interface | CGO required, fragile ABI | ❌ Rejected |
| **REST API service** | Language agnostic | Daemon required, complexity | ❌ Rejected |
| **Subprocess** | Simple, portable, testable | Process overhead (negligible) | ✅ **Chosen** |

---

## 5. Declarative Infrastructure with Native SDKs

### 5.1 Declarative Programming Model

**Core Concept**: User declares desired end state, NIC reconciles actual state to match.

**Desired State (from nebari-config.yaml):**
```yaml
project_name: my-nebari
provider: aws

amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.28"
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 5
    user:
      instance_type: m5.2xlarge
      min_size: 0
      max_size: 10
```

**NIC Internal Representation:**
```go
type DesiredState struct {
    Provider string
    Region   string
    VPC      VPCConfig
    Cluster  ClusterConfig
    NodeGroups []NodeGroupConfig
    Storage  StorageConfig
    Kubernetes KubernetesConfig
}
```

### 5.2 Reconciliation Loop

**High-Level Flow:**
```go
func Deploy(configPath string) error {
    // 1. Parse user config (desired state)
    desired := parseConfig(configPath)
    
    // 2. Read current managed state
    current := readState()
    
    // 3. Query actual infrastructure
    actual := queryInfrastructure(desired.Provider)
    
    // 4. Detect drift
    if actual != current {
        logDrift(actual, current)
    }
    
    // 5. Calculate changes needed
    plan := calculatePlan(desired, actual)
    
    // 6. Execute changes via native SDKs
    if err := executePlan(plan); err != nil {
        return err
    }
    
    // 7. Update state
    return saveState(desired)
}
```

**Detailed Reconciliation:**
```go
func (p *AWSProvider) Reconcile(desired DesiredState) error {
    // VPC
    vpc, err := p.reconcileVPC(desired.VPC)
    if err != nil {
        return err
    }
    
    // Cluster
    cluster, err := p.reconcileCluster(desired.Cluster, vpc)
    if err != nil {
        return err
    }
    
    // Node Groups
    for _, ng := range desired.NodeGroups {
        if err := p.reconcileNodeGroup(ng, cluster); err != nil {
            return err
        }
    }
    
    // Storage
    storage, err := p.reconcileStorage(desired.Storage, vpc)
    if err != nil {
        return err
    }
    
    // Kubernetes Resources
    if err := p.reconcileKubernetes(desired.Kubernetes, cluster); err != nil {
        return err
    }
    
    return nil
}
```

**Individual Resource Reconciliation:**
```go
func (p *AWSProvider) reconcileNodeGroup(desired NodeGroupConfig, cluster Cluster) error {
    // Query AWS to see if node group exists
    actual, err := p.eks.DescribeNodeGroup(cluster.Name, desired.Name)
    
    if err == ErrNotFound {
        // Create: doesn't exist, desired says it should
        return p.createNodeGroup(desired, cluster)
    }
    
    if err != nil {
        return err
    }
    
    // Update: exists but configuration differs
    if needsUpdate(desired, actual) {
        return p.updateNodeGroup(desired, actual, cluster)
    }
    
    // No change needed
    return nil
}
```

### 5.3 Idempotency

**Key Property**: Running NIC multiple times with same config produces same result.

**Examples:**

**Scenario 1: No Changes**
```bash
# First run
$ nic deploy -f nebari-config.yaml
Creating VPC vpc-123...
Creating cluster my-cluster...
Creating node group general...
Done.

# Second run (no config changes)
$ nic deploy -f nebari-config.yaml
VPC vpc-123 exists, no changes needed.
Cluster my-cluster exists, no changes needed.
Node group general exists, no changes needed.
Done. (0 changes)
```

**Scenario 2: Add Node Group**
```yaml
# Config before
node_groups:
  general: {...}
  user: {...}

# Config after (add worker)
node_groups:
  general: {...}
  user: {...}
  worker: {...}  # new
```

```bash
$ nic deploy -f nebari-config.yaml
VPC vpc-123 exists, no changes needed.
Cluster my-cluster exists, no changes needed.
Node group general exists, no changes needed.
Node group user exists, no changes needed.
Creating node group worker...
Done. (1 change)
```

**Scenario 3: Update Node Group Size**
```yaml
# Config before
node_groups:
  general:
    max_size: 5

# Config after
node_groups:
  general:
    max_size: 10  # changed
```

```bash
$ nic deploy -f nebari-config.yaml
VPC vpc-123 exists, no changes needed.
Cluster my-cluster exists, no changes needed.
Updating node group general (max_size: 5 → 10)...
Done. (1 change)
```

### 5.4 Native SDK Usage

**AWS Example:**
```go
import (
    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/eks"
    "github.com/aws/aws-sdk-go-v2/service/ec2"
)

func (p *AWSProvider) createCluster(config ClusterConfig) error {
    // Direct AWS SDK usage
    input := &eks.CreateClusterInput{
        Name:    aws.String(config.Name),
        Version: aws.String(config.Version),
        ResourcesVpcConfig: &types.VpcConfigRequest{
            SubnetIds: config.SubnetIDs,
        },
        RoleArn: aws.String(config.RoleARN),
    }
    
    result, err := p.eks.CreateCluster(context.TODO(), input)
    if err != nil {
        return fmt.Errorf("failed to create cluster: %w", err)
    }
    
    // Wait for cluster to be ready
    waiter := eks.NewClusterActiveWaiter(p.eks)
    return waiter.Wait(context.TODO(), 
        &eks.DescribeClusterInput{Name: aws.String(config.Name)},
        30*time.Minute)
}
```

**GCP Example:**
```go
import (
    container "cloud.google.com/go/container/apiv1"
    containerpb "google.golang.org/genproto/googleapis/container/v1"
)

func (p *GCPProvider) createCluster(config ClusterConfig) error {
    // Direct GCP SDK usage
    req := &containerpb.CreateClusterRequest{
        Cluster: &containerpb.Cluster{
            Name:             config.Name,
            InitialClusterVersion: config.Version,
            Network:          config.Network,
            Subnetwork:       config.Subnetwork,
            NodePools:        []*containerpb.NodePool{...},
        },
    }
    
    op, err := p.gke.CreateCluster(context.TODO(), req)
    if err != nil {
        return fmt.Errorf("failed to create cluster: %w", err)
    }
    
    // Wait for operation to complete
    return p.waitForOperation(op)
}
```

**Kubernetes Example:**
```go
import (
    "k8s.io/client-go/kubernetes"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (p *Provider) createNamespace(name string) error {
    // Direct Kubernetes client-go usage
    ns := &corev1.Namespace{
        ObjectMeta: metav1.ObjectMeta{
            Name: name,
            Labels: map[string]string{
                "managed-by": "nebari-infrastructure-core",
                "project":    p.config.ProjectName,
            },
        },
    }
    
    _, err := p.k8s.CoreV1().Namespaces().Create(
        context.TODO(),
        ns,
        metav1.CreateOptions{},
    )
    
    if err != nil && !errors.IsAlreadyExists(err) {
        return fmt.Errorf("failed to create namespace: %w", err)
    }
    
    return nil
}
```

### 5.5 Error Handling and Rollback

**Declarative Error Handling:**

When errors occur during reconciliation, NIC handles them declaratively:

```go
func (p *Provider) Reconcile(desired DesiredState) error {
    // Track what was created for potential rollback
    tracker := NewResourceTracker()
    
    // Phase 1: VPC
    vpc, err := p.reconcileVPC(desired.VPC)
    if err != nil {
        return fmt.Errorf("VPC reconciliation failed: %w", err)
    }
    tracker.Add(vpc)
    
    // Phase 2: Cluster
    cluster, err := p.reconcileCluster(desired.Cluster, vpc)
    if err != nil {
        // Cluster failed, but VPC succeeded
        // Option 1: Keep VPC (default)
        // Option 2: Rollback VPC (if user requested)
        return p.handlePartialFailure(tracker, err)
    }
    tracker.Add(cluster)
    
    // Phase 3: Node Groups
    for _, ng := range desired.NodeGroups {
        if err := p.reconcileNodeGroup(ng, cluster); err != nil {
            return p.handlePartialFailure(tracker, err)
        }
    }
    
    // All succeeded
    return nil
}
```

**Partial Failure Handling:**

```go
func (p *Provider) handlePartialFailure(tracker *ResourceTracker, err error) error {
    log.Errorf("Reconciliation failed: %v", err)
    log.Infof("Successfully created: %v", tracker.Resources())
    
    // User can choose:
    // 1. Keep partial infrastructure, fix issue, re-run
    // 2. Rollback everything
    // 3. Manual intervention
    
    if p.config.RollbackOnFailure {
        log.Info("Rolling back created resources...")
        return tracker.Rollback()
    }
    
    return fmt.Errorf("partial deployment: %w", err)
}
```

### 5.6 Convergence and Eventual Consistency

**Goal**: Keep trying to reach desired state even if temporary failures occur.

**Example: Node Group Auto-Scaling**
```yaml
# Desired state
node_groups:
  worker:
    min_size: 1
    max_size: 10
    desired_size: 3
```

**Reconciliation:**
```go
func (p *AWSProvider) reconcileNodeGroupSize(desired, actual NodeGroup) error {
    if actual.DesiredSize != desired.DesiredSize {
        // Actual: 5, Desired: 3
        // Update to converge toward desired
        return p.updateNodeGroupSize(desired.Name, desired.DesiredSize)
    }
    return nil
}
```

**On Failure:**
```go
// First attempt fails (transient network error)
if err := p.updateNodeGroupSize(...); err != nil {
    // Don't fail entire deployment
    // Just log and continue
    log.Warnf("Failed to update node group size: %v", err)
    log.Info("Will retry on next reconciliation")
}

// User runs deploy again
// NIC detects actual still != desired
// Tries again (succeeds this time)
```

---

## 6. State Management Design

### 6.1 State File Format

**Format**: JSON (human-readable, version-controllable, widely supported)

**Location**: `.nebari/stages/02-infrastructure/nic-state.json`

**Basic Structure:**
```json
{
  "version": "1.0.0",
  "format_version": "1",
  "provider": "aws",
  "project_name": "my-nebari",
  "region": "us-west-2",
  "imported_from_terraform": true,
  
  "cloud": {
    "vpc": {
      "id": "vpc-123",
      "cidr": "10.0.0.0/16",
      "subnets": [
        {"id": "subnet-123", "cidr": "10.0.1.0/24", "az": "us-west-2a"},
        {"id": "subnet-456", "cidr": "10.0.2.0/24", "az": "us-west-2b"}
      ]
    },
    "cluster": {
      "name": "my-nebari",
      "id": "my-nebari",
      "endpoint": "https://ABC123.gr7.us-west-2.eks.amazonaws.com",
      "ca_certificate": "LS0tLS1CRUdJTi...",
      "version": "1.28",
      "oidc_issuer": "https://oidc.eks.us-west-2.amazonaws.com/id/ABC123"
    },
    "node_groups": [
      {
        "name": "general",
        "arn": "arn:aws:eks:...",
        "instance_type": "m5.xlarge",
        "min_size": 1,
        "max_size": 5,
        "desired_size": 2
      },
      {
        "name": "user",
        "arn": "arn:aws:eks:...",
        "instance_type": "m5.2xlarge",
        "min_size": 0,
        "max_size": 10,
        "desired_size": 0
      }
    ],
    "storage": {
      "type": "ebs",
      "volumes": [
        {"id": "vol-123", "size": 100, "type": "gp3"}
      ]
    },
    "iam_roles": [
      {"name": "my-nebari-cluster-role", "arn": "arn:aws:iam::..."}
    ]
  },
  
  "kubernetes": {
    "namespaces": [
      {"name": "dev", "labels": {"managed-by": "nic"}},
      {"name": "prod", "labels": {"managed-by": "nic"}}
    ],
    "service_accounts": [
      {
        "name": "default",
        "namespace": "dev",
        "annotations": {
          "eks.amazonaws.com/role-arn": "arn:aws:iam::..."
        }
      }
    ],
    "storage_classes": [
      {
        "name": "gp3",
        "provisioner": "ebs.csi.aws.com",
        "parameters": {
          "type": "gp3",
          "encrypted": "true"
        },
        "is_default": true
      }
    ],
    "roles": [
      {"name": "admin", "namespace": "dev"}
    ],
    "cluster_roles": [
      {"name": "cluster-admin"}
    ]
  },
  
  "metadata": {
    "created_at": "2025-01-27T10:00:00Z",
    "updated_at": "2025-01-27T15:30:00Z",
    "nic_version": "1.0.0",
    "configuration_hash": "abc123...",
    "last_successful_deploy": "2025-01-27T15:30:00Z"
  }
}
```

**State Design Principles:**

1. **Resource IDs**: Store cloud provider IDs for all resources
2. **Minimal Data**: Only what's needed for management
3. **No Secrets**: Never store credentials or sensitive data
4. **Configuration Hash**: Detect config changes
5. **Timestamps**: Track when resources created/updated
6. **Import Flag**: Track imported vs NIC-created resources

### 6.2 State Operations

**Read State:**
```go
func ReadState(path string) (*State, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return NewEmptyState(), nil
        }
        return nil, err
    }
    
    var state State
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, fmt.Errorf("invalid state file: %w", err)
    }
    
    if err := state.Validate(); err != nil {
        return nil, fmt.Errorf("state validation failed: %w", err)
    }
    
    return &state, nil
}
```

**Write State:**
```go
func WriteState(path string, state *State) error {
    // Update metadata
    state.Metadata.UpdatedAt = time.Now()
    state.Metadata.NICVersion = version.Current
    
    // Validate before writing
    if err := state.Validate(); err != nil {
        return fmt.Errorf("state validation failed: %w", err)
    }
    
    // Pretty-print JSON for readability
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return err
    }
    
    // Atomic write (write temp file, then rename)
    tmpPath := path + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0600); err != nil {
        return err
    }
    
    return os.Rename(tmpPath, path)
}
```

**State Validation:**
```go
func (s *State) Validate() error {
    if s.Version == "" {
        return errors.New("state version missing")
    }
    
    if s.Provider == "" {
        return errors.New("provider missing")
    }
    
    // Validate resource IDs are present
    if s.Cloud.Cluster.ID == "" {
        return errors.New("cluster ID missing")
    }
    
    // Validate format version compatibility
    if !isCompatibleVersion(s.FormatVersion, CurrentFormatVersion) {
        return fmt.Errorf("incompatible state format: %s (current: %s)",
            s.FormatVersion, CurrentFormatVersion)
    }
    
    return nil
}
```

### 6.3 State Backends

**Local Backend (Default):**
```go
type LocalBackend struct {
    path string
}

func (b *LocalBackend) Read() (*State, error) {
    return ReadState(b.path)
}

func (b *LocalBackend) Write(state *State) error {
    return WriteState(b.path, state)
}

func (b *LocalBackend) Lock() error {
    // Use flock for file locking
    return flock.Lock(b.path + ".lock")
}

func (b *LocalBackend) Unlock() error {
    return flock.Unlock(b.path + ".lock")
}
```

**S3 Backend:**
```go
type S3Backend struct {
    bucket      string
    key         string
    lockTable   string  // DynamoDB table for locking
    s3Client    *s3.Client
    dynamoClient *dynamodb.Client
}

func (b *S3Backend) Read() (*State, error) {
    result, err := b.s3Client.GetObject(context.TODO(), &s3.GetObjectInput{
        Bucket: aws.String(b.bucket),
        Key:    aws.String(b.key),
    })
    if err != nil {
        return nil, err
    }
    defer result.Body.Close()
    
    var state State
    if err := json.NewDecoder(result.Body).Decode(&state); err != nil {
        return nil, err
    }
    
    return &state, nil
}

func (b *S3Backend) Write(state *State) error {
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return err
    }
    
    _, err = b.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
        Bucket: aws.String(b.bucket),
        Key:    aws.String(b.key),
        Body:   bytes.NewReader(data),
    })
    
    return err
}

func (b *S3Backend) Lock() error {
    // Use DynamoDB for distributed locking
    _, err := b.dynamoClient.PutItem(context.TODO(), &dynamodb.PutItemInput{
        TableName: aws.String(b.lockTable),
        Item: map[string]types.AttributeValue{
            "LockID":    &types.AttributeValueMemberS{Value: b.key},
            "Info":      &types.AttributeValueMemberS{Value: getLockInfo()},
            "ExpiresAt": &types.AttributeValueMemberN{Value: getExpiry()},
        },
        ConditionExpression: aws.String("attribute_not_exists(LockID)"),
    })
    
    if err != nil {
        return fmt.Errorf("failed to acquire lock: %w", err)
    }
    
    return nil
}
```

**GCS Backend:**
```go
type GCSBackend struct {
    bucket string
    object string
    client *storage.Client
}

func (b *GCSBackend) Lock() error {
    // Use GCS object metadata conditions for locking
    obj := b.client.Bucket(b.bucket).Object(b.object + ".lock")
    
    w := obj.If(storage.Conditions{DoesNotExist: true}).NewWriter(context.TODO())
    w.ObjectAttrs.Metadata = map[string]string{
        "locked_by": getLockInfo(),
        "locked_at": time.Now().Format(time.RFC3339),
    }
    
    if err := w.Close(); err != nil {
        return fmt.Errorf("failed to acquire lock: %w", err)
    }
    
    return nil
}
```

**Azure Blob Backend:**
```go
type AzureBlobBackend struct {
    account   string
    container string
    blob      string
    client    *azblob.Client
}

func (b *AzureBlobBackend) Lock() error {
    // Use blob lease for locking
    leaseClient, err := b.client.ServiceClient().
        NewContainerClient(b.container).
        NewBlobClient(b.blob).
        GetLeaseClient(nil)
    
    if err != nil {
        return err
    }
    
    _, err = leaseClient.AcquireLease(context.TODO(), &blob.AcquireLeaseOptions{
        Duration: to.Ptr(int32(60)), // 60 second lease
    })
    
    if err != nil {
        return fmt.Errorf("failed to acquire lock: %w", err)
    }
    
    return nil
}
```

### 6.4 State Locking

**Why Locking Matters:**
- Prevent concurrent modifications
- Avoid race conditions
- Ensure consistency

**Lock Acquisition Flow:**
```go
func Deploy(config Config) error {
    backend := getBackend(config)
    
    // Acquire lock
    if err := backend.Lock(); err != nil {
        return fmt.Errorf("failed to acquire state lock: %w", err)
    }
    defer backend.Unlock()
    
    // Read state (protected by lock)
    state, err := backend.Read()
    if err != nil {
        return err
    }
    
    // Perform reconciliation
    if err := reconcile(config, state); err != nil {
        return err
    }
    
    // Write state (still protected by lock)
    return backend.Write(state)
}
```

**Lock Timeout:**
```go
func (b *Backend) LockWithTimeout(timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return fmt.Errorf("timeout acquiring lock after %v", timeout)
        case <-ticker.C:
            if err := b.Lock(); err == nil {
                return nil
            }
            // Lock failed, retry
        }
    }
}
```

**Force Unlock (Emergency):**
```bash
# If lock gets stuck (process crashed, etc.)
nic state unlock --force
```

```go
func (b *S3Backend) ForceUnlock() error {
    _, err := b.dynamoClient.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
        TableName: aws.String(b.lockTable),
        Key: map[string]types.AttributeValue{
            "LockID": &types.AttributeValueMemberS{Value: b.key},
        },
    })
    return err
}
```

### 6.5 Drift Detection

**Drift**: State file says one thing, actual infrastructure is different.

**Detection Process:**
```go
func DetectDrift(stateFile string, provider string) (*DriftReport, error) {
    // Read NIC state (what we think we're managing)
    state, err := ReadState(stateFile)
    if err != nil {
        return nil, err
    }
    
    // Query actual infrastructure
    p := getProvider(provider)
    actual, err := p.QueryInfrastructure()
    if err != nil {
        return nil, err
    }
    
    // Compare
    report := &DriftReport{}
    
    // Check cloud resources
    report.Cloud = compareCloudResources(state.Cloud, actual.Cloud)
    
    // Check K8s resources
    report.Kubernetes = compareK8sResources(state.Kubernetes, actual.Kubernetes)
    
    return report, nil
}
```

**Drift Report:**
```json
{
  "has_drift": true,
  "cloud": {
    "vpc": {"drift": false},
    "cluster": {"drift": false},
    "node_groups": {
      "general": {
        "drift": true,
        "field": "max_size",
        "state_value": 5,
        "actual_value": 10
      },
      "user": {"drift": false}
    }
  },
  "kubernetes": {
    "namespaces": {
      "dev": {"drift": false},
      "staging": {
        "drift": true,
        "reason": "exists in cluster but not in state"
      }
    },
    "storage_classes": {"drift": false}
  },
  "summary": {
    "total_resources": 25,
    "drifted_resources": 2,
    "added_resources": 1,
    "removed_resources": 0
  }
}
```

**Drift Commands:**
```bash
# Detect drift
nic drift detect
# Shows what's different

# Show detailed diff
nic drift diff
# Shows exact differences

# Update state to match actual (accept drift)
nic drift accept
# State <- Actual infrastructure

# Update infrastructure to match state (fix drift)
nic drift fix
# State -> Actual infrastructure
```

### 6.6 State Migration and Versioning

**State Version Evolution:**
```json
{
  "version": "1.0.0",      // NIC version that created this state
  "format_version": "1"    // State schema version
}
```

**Migration Between Format Versions:**
```go
func MigrateState(oldState *State) (*State, error) {
    switch oldState.FormatVersion {
    case "1":
        // Already current
        return oldState, nil
    case "0":
        // Migrate from v0 to v1
        return migrateV0ToV1(oldState)
    default:
        return nil, fmt.Errorf("unknown state format version: %s", 
            oldState.FormatVersion)
    }
}

func migrateV0ToV1(old *State) (*State, error) {
    new := &State{
        FormatVersion: "1",
        Version: version.Current,
        Provider: old.Provider,
        // ... map old fields to new structure
    }
    return new, nil
}
```

**Automatic Migration:**
```go
func ReadState(path string) (*State, error) {
    state, err := readRawState(path)
    if err != nil {
        return nil, err
    }
    
    // Auto-migrate if needed
    if state.FormatVersion != CurrentFormatVersion {
        log.Infof("Migrating state from v%s to v%s",
            state.FormatVersion, CurrentFormatVersion)
        
        state, err = MigrateState(state)
        if err != nil {
            return nil, fmt.Errorf("migration failed: %w", err)
        }
        
        // Save migrated state
        if err := WriteState(path, state); err != nil {
            return nil, fmt.Errorf("failed to save migrated state: %w", err)
        }
    }
    
    return state, nil
}
```

---

## 7. Configuration Compatibility Strategy

### 7.1 Configuration Parsing Philosophy

**Core Requirement**: Existing `nebari-config.yaml` files must work without modification in v1.0.

**v1.0 Interpretation**: Config describes existing infrastructure (after import) or desired infrastructure (fresh deploy that matches what Terraform would create).

**Strategy**: 
1. Parse full nebari-config.yaml
2. Extract infrastructure sections (stages 02-03 equivalent)
3. Ignore application-stage configurations (04+)
4. For imports: Preserve existing resources exactly
5. For fresh deploys: Create equivalent to what Terraform would create

### 7.2 Nebari Config Structure (Infrastructure Sections)

**Common Structure:**
```yaml
project_name: my-nebari       # ← Required
namespace: dev                 # ← Optional (used for K8s namespaces)
domain: nebari.example.com    # ← Not used by NIC (app stages use this)
provider: aws                  # ← Required: aws|gcp|azure|local

# Provider-specific section
amazon_web_services:          # ← API contract
  region: us-west-2
  kubernetes_version: "1.28"
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 5
    user:
      instance_type: m5.2xlarge
      min_size: 0
      max_size: 10

# These sections exist but NIC ignores them (used by stage 04+)
certificate: {...}            # ← Used by cert-manager stage
ingress: {...}                # ← Used by ingress stage
default_images: {...}         # ← Used by application stages
```

**All Provider Section Names (API Contracts):**
- `amazon_web_services` - AWS configuration
- `google_cloud_platform` - GCP configuration
- `azure` - Azure configuration
- `local` - Local/on-prem configuration

### 7.3 v1.0 Config Interpretation: Preserve What Exists

**Key Principle**: v1.0 interprets config as "describe what I have" not "what I want to change to".

**Example - Storage Config:**
```yaml
amazon_web_services:
  storage:
    type: ebs
    size: 100
```

**v1.0 Interpretation:**

| Context | Meaning | NIC Action |
|---------|---------|------------|
| **Import** | "I have EBS storage" | Keep existing EBS volumes |
| **Fresh Deploy** | "Create EBS storage" | Create EBS (match Terraform behavior) |
| **Subsequent Deploy** | "Continue with EBS" | No changes to storage |

**NOT Interpreted As:**
- "Change my storage to EBS from something else"
- "Optimize my storage setup"
- "Migrate to better storage"

**Example - Node Group Config:**
```yaml
amazon_web_services:
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 5
```

**v1.0 Interpretation:**

| Context | Meaning | NIC Action |
|---------|---------|------------|
| **Import (existing m5.xlarge)** | "I have m5.xlarge" | Keep m5.xlarge |
| **Import (existing m5.large)** | "I have m5.large, config says xlarge" | Keep m5.large, log drift |
| **Fresh Deploy** | "Create m5.xlarge" | Create m5.xlarge |
| **Add Node Group** | "Add new group" | Create new group only |

### 7.4 Output Generation for Application Stages

**Critical Requirement**: Outputs must be compatible with existing application stages (04+).

**Output File Structure (Unified):**

```json
{
  "format_version": "1.0",
  "provider": "aws",
  "cluster": {
    "name": "my-nebari",
    "endpoint": "https://ABC.eks.amazonaws.com",
    "ca_certificate": "base64...",
    "kubernetes_version": "1.28",
    "oidc_issuer": "https://oidc.eks.amazonaws.com/id/ABC"
  },
  "networking": {
    "vpc_id": "vpc-123",
    "vpc_cidr": "10.0.0.0/16",
    "subnet_ids": ["subnet-123", "subnet-456"],
    "security_group_ids": ["sg-789"]
  },
  "kubeconfig": {
    "path": "/path/to/kubeconfig",
    "context": "my-nebari"
  },
  "kubernetes": {
    "namespaces": ["dev", "prod", "monitoring"],
    "service_accounts": {
      "default": "arn:aws:iam::123456789012:role/my-nebari-sa",
      "monitoring": "arn:aws:iam::123456789012:role/my-nebari-monitoring"
    },
    "storage_classes": ["gp3", "gp2"],
    "default_storage_class": "gp3"
  },
  "node_groups": {
    "general": {
      "arn": "arn:aws:eks:...",
      "instance_type": "m5.xlarge",
      "min_size": 1,
      "max_size": 5
    },
    "user": {
      "arn": "arn:aws:eks:...",
      "instance_type": "m5.2xlarge",
      "min_size": 0,
      "max_size": 10
    }
  },
  "storage": {
    "type": "ebs",
    "volume_ids": ["vol-123"],
    "storage_class": "gp3"
  },
  "iam": {
    "cluster_role_arn": "arn:aws:iam::...",
    "node_role_arn": "arn:aws:iam::..."
  }
}
```

**Application Stage Compatibility:**

Application stages (04+) read outputs like:

```hcl
# Stage 04+ Terraform
locals {
  infrastructure = jsondecode(file("../02-infrastructure/outputs.json"))
  cluster_endpoint = local.infrastructure.cluster.endpoint
  vpc_id = local.infrastructure.networking.vpc_id
  namespaces = local.infrastructure.kubernetes.namespaces
}

# Use these values
resource "kubernetes_ingress" "main" {
  metadata {
    namespace = local.infrastructure.kubernetes.namespaces[0]
  }
  # ...
}
```

**Backwards Compatibility:**

Old approach (two Terraform stages):
```
stages/02-infrastructure/terraform-output.json
stages/03-kubernetes-initialize/terraform-output.json
```

New approach (NIC):
```
stages/02-infrastructure/outputs.json (consolidated)
```

Application stages need to read one file instead of two, but all required data is present.

### 7.5 Provider-Specific Configuration

**AWS Configuration:**
```yaml
amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.28"
  availability_zones:
    - us-west-2a
    - us-west-2b
  vpc_cidr: "10.0.0.0/16"
  
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 5
      disk_size: 100
    user:
      instance_type: m5.2xlarge
      min_size: 0
      max_size: 10
      disk_size: 200
  
  storage:
    type: ebs  # v1.0: keep as-is
    size: 100
```

**GCP Configuration:**
```yaml
google_cloud_platform:
  region: us-central1
  kubernetes_version: "1.28"
  zone: us-central1-a
  project_id: my-project
  
  node_pools:
    general:
      machine_type: n1-standard-4
      min_count: 1
      max_count: 5
    user:
      machine_type: n1-standard-8
      min_count: 0
      max_count: 10
  
  storage:
    type: pd-standard  # v1.0: keep as-is
    size: 100
```

**Azure Configuration:**
```yaml
azure:
  region: eastus
  kubernetes_version: "1.28"
  resource_group: my-nebari-rg
  
  node_pools:
    general:
      vm_size: Standard_D4s_v3
      min_count: 1
      max_count: 5
    user:
      vm_size: Standard_D8s_v3
      min_count: 0
      max_count: 10
  
  storage:
    type: azure-disk  # v1.0: keep as-is
    size: 100
```

**Local Configuration:**
```yaml
local:
  kube_context: k3s-default
  
  node_selectors:
    general: {"role": "general"}
    user: {"role": "user"}
  
  storage:
    type: local-path
    path: /mnt/data
```

---

## 8. Provider Porting Strategy

### 8.1 Porting Priorities

**Phase 1 (v0.1-v0.3): Core Providers**
1. AWS (most common in Nebari deployments)
2. GCP (second most common)
3. Local (for development/testing)

**Phase 2 (v0.4-v0.5): Remaining Provider**
4. Azure

**Rationale**: Focus on most-used providers first, establish patterns for unified cloud+K8s provisioning, then port remaining.

### 8.2 Porting Approach: Exact Replication

**Philosophy**: v1.0 replicates **exactly** what Terraform stages 02-03 create, not **improvements**.

**Porting Process per Provider:**

1. **Audit Current Terraform Modules (Stages 02 + 03)**
   - Document every resource created
   - Document resource configurations
   - Document dependencies between resources
   - **Document outputs generated** (critical for stage 04+)
   - Capture implicit behaviors
   - Test current outputs with stage 04+

2. **Design Go Implementation (Native SDKs)**
   - Map each Terraform resource to native SDK calls
   - **Preserve exact resource configurations**
   - Identify natural ordering (no need for graph)
   - Design error handling
   - Plan rollback strategy
   - **Ensure outputs match Terraform exactly**

3. **Implement Core Functions**
   - VPC/Network creation (exact match)
   - Kubernetes cluster provisioning (same versions, configs)
   - Node group management (same instance types, sizes)
   - Storage setup (same storage types)
   - **Kubernetes namespace creation**
   - **RBAC setup (same roles, permissions)**
   - **Storage class creation (same provisioners, parameters)**
   - **Service account configuration**
   - **Output generation (compatible format)**

4. **Validate Equivalence**
   - Deploy test cluster with Terraform
   - Document all resources created
   - Deploy equivalent cluster with NIC
   - Compare resource lists
   - Compare resource configurations
   - **Compare outputs files**
   - **Test stages 04+ work with NIC outputs**
   - Verify Nebari end-to-end works

5. **Document Implementation**
   - Map Terraform → NIC SDK calls
   - Note any minor differences (if unavoidable)
   - Explain why differences acceptable
   - Document validation results

### 8.3 AWS Provider Porting Details

**Current Terraform Resources:**

**Stage 02:**
- VPC with public/private subnets
- Internet Gateway and NAT Gateways
- EKS cluster with OIDC provider
- EKS node groups (general, user, worker)
- Security groups
- IAM roles and policies
- EBS volumes
- Route tables

**Stage 03:**
- Namespaces (dev, monitoring, etc.)
- ServiceAccounts
- Roles and RoleBindings
- ClusterRoles and ClusterRoleBindings
- StorageClasses (referencing stage 02 EBS)
- ResourceQuotas
- LimitRanges

**NIC Implementation (Exact Replication):**

| Resource | Terraform | NIC v1.0 | Verification |
|----------|-----------|----------|--------------|
| **VPC CIDR** | 10.0.0.0/16 | 10.0.0.0/16 | Match |
| **Subnets** | 2 public, 2 private | 2 public, 2 private | Match |
| **EKS Version** | User-specified | Same from config | Match |
| **Node Group Instance** | m5.xlarge | m5.xlarge | Match |
| **EBS Volume** | gp3 100GB | gp3 100GB | Match |
| **Storage Class** | "gp3" provisioner | "gp3" provisioner | Match |
| **RBAC Roles** | admin, viewer | admin, viewer | Match |
| **Namespaces** | dev, prod | dev, prod | Match |

**SDK Implementation Example:**

```go
// VPC Creation (matching Terraform)
func (p *AWSProvider) createVPC(config VPCConfig) (*VPC, error) {
    // Same CIDR as Terraform module used
    input := &ec2.CreateVpcInput{
        CidrBlock: aws.String("10.0.0.0/16"),
        TagSpecifications: []types.TagSpecification{
            {
                ResourceType: types.ResourceTypeVpc,
                Tags: []types.Tag{
                    {Key: aws.String("Name"), Value: aws.String(config.Name)},
                    {Key: aws.String("managed-by"), Value: aws.String("nebari-infrastructure-core")},
                },
            },
        },
    }
    
    result, err := p.ec2.CreateVpc(context.TODO(), input)
    if err != nil {
        return nil, fmt.Errorf("failed to create VPC: %w", err)
    }
    
    // Wait for VPC to be available (same as Terraform)
    waiter := ec2.NewVpcAvailableWaiter(p.ec2)
    if err := waiter.Wait(context.TODO(),
        &ec2.DescribeVpcsInput{VpcIds: []string{*result.Vpc.VpcId}},
        5*time.Minute); err != nil {
        return nil, fmt.Errorf("VPC not available: %w", err)
    }
    
    return &VPC{ID: *result.Vpc.VpcId, CIDR: *result.Vpc.CidrBlock}, nil
}

// EKS Cluster (matching Terraform)
func (p *AWSProvider) createEKSCluster(config ClusterConfig, vpc *VPC) error {
    input := &eks.CreateClusterInput{
        Name:    aws.String(config.Name),
        Version: aws.String(config.Version),  // From nebari-config.yaml
        RoleArn: aws.String(config.RoleARN),
        ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
            SubnetIds:         config.SubnetIDs,
            SecurityGroupIds:  config.SecurityGroupIDs,
            EndpointPublicAccess: aws.Bool(true),   // Match Terraform default
            EndpointPrivateAccess: aws.Bool(false), // Match Terraform default
        },
        Tags: map[string]string{
            "managed-by": "nebari-infrastructure-core",
            "project":    config.ProjectName,
        },
    }
    
    _, err := p.eks.CreateCluster(context.TODO(), input)
    if err != nil {
        return fmt.Errorf("failed to create EKS cluster: %w", err)
    }
    
    // Wait for cluster to be active (same as Terraform)
    waiter := eks.NewClusterActiveWaiter(p.eks)
    return waiter.Wait(context.TODO(),
        &eks.DescribeClusterInput{Name: aws.String(config.Name)},
        30*time.Minute)
}

// Storage Class (matching Terraform stage 03)
func (p *AWSProvider) createStorageClass(name string, parameters map[string]string) error {
    sc := &storagev1.StorageClass{
        ObjectMeta: metav1.ObjectMeta{
            Name: name,
            Annotations: map[string]string{
                "storageclass.kubernetes.io/is-default-class": "true",
            },
        },
        Provisioner: "ebs.csi.aws.com",  // Same as Terraform
        Parameters:  parameters,          // Same parameters
        VolumeBindingMode: ptr(storagev1.VolumeBindingWaitForFirstConsumer),
    }
    
    _, err := p.k8s.StorageV1().StorageClasses().Create(
        context.TODO(),
        sc,
        metav1.CreateOptions{},
    )
    
    if err != nil && !errors.IsAlreadyExists(err) {
        return fmt.Errorf("failed to create storage class: %w", err)
    }
    
    return nil
}
```

### 8.4 GCP Provider Porting Details

**Current Terraform Resources:**

**Stage 02:**
- VPC network and subnets
- GKE cluster with workload identity
- GKE node pools
- Cloud NAT
- Firewall rules
- Service accounts (GCP)
- Persistent disks

**Stage 03:**
- Namespaces
- ServiceAccounts (K8s)
- Roles and RoleBindings
- StorageClasses (pd-standard)
- NetworkPolicies

**NIC Implementation (Exact Replication):**

| Resource | Terraform | NIC v1.0 | Verification |
|----------|-----------|----------|--------------|
| **VPC CIDR** | 10.0.0.0/16 | 10.0.0.0/16 | Match |
| **GKE Version** | User-specified | Same | Match |
| **Node Machine Type** | n1-standard-4 | n1-standard-4 | Match |
| **Disk Type** | pd-standard | pd-standard | Match |
| **Storage Class** | "standard" | "standard" | Match |

### 8.5 Azure Provider Porting Details

**Current Terraform Resources:**

**Stage 02:**
- Resource group
- Virtual Network with subnets
- AKS cluster
- Node pools
- Azure Disk
- Managed identities
- NSGs

**Stage 03:**
- Namespaces
- ServiceAccounts
- Roles and RoleBindings
- StorageClasses (azure-disk)
- NetworkPolicies

**NIC Implementation**: Exact replication of Terraform behavior.

### 8.6 Local Provider Porting Details

**Current Implementation:**

**Stage 02:**
- K3s installation via shell scripts
- Local storage provisioner

**Stage 03:**
- Namespaces
- RBAC
- StorageClass for local storage

**NIC Implementation**:
- Go-native K3s bootstrapping
- Same local storage setup
- Same namespaces and RBAC

### 8.7 Cross-Provider Consistency

**Standardization Goals for v1.0:**
1. **Resource Naming**: Same naming conventions as Terraform
2. **Labels**: Same Kubernetes labels as Terraform
3. **Taints**: Same node taints as Terraform
4. **Storage Classes**: Same names and provisioners as Terraform
5. **Outputs**: Same output structure as Terraform

**Example - Kubernetes Labels (Match Terraform):**
```go
// If Terraform adds these labels:
labels := map[string]string{
    "app.kubernetes.io/name":       "nebari",
    "app.kubernetes.io/managed-by": "terraform",  // v1.0: keep this!
    "nebari.dev/project":          config.ProjectName,
    "nebari.dev/node-group":       groupName,
}

// NIC v1.0 uses same labels (to match exactly)
// v2.0+ can change "managed-by" to "nebari-infrastructure-core"
```

---

## 9. Nebari Integration Design

### 9.1 Integration Architecture with Unified Stages

**Stage Flow:**
```
Nebari CLI (nebari deploy)
    ↓
┌───────────────────────────────────┐
│ Stage 01: Terraform State Backend │
│ - S3 bucket or GCS bucket         │
│ - DynamoDB table (AWS)            │
│ - State backend init              │
└─────────────┬─────────────────────┘
              │ outputs: backend config
              ↓
┌───────────────────────────────────┐
│ Stages 02-03: Complete            │ ← THIS PROJECT
│ Infrastructure (NIC)              │
│ - VPC/Network                     │
│ - Kubernetes cluster              │
│ - Node pools                      │
│ - Storage                         │
│ - Namespaces                      │
│ - RBAC                            │
│ - StorageClasses                  │
└─────────────┬─────────────────────┘
              │ outputs: complete infrastructure
              │ (single outputs.json)
              ↓
┌───────────────────────────────────┐
│ Stage 04+: Applications (Terraform)│
│ - Ingress controller              │
│ - Cert-manager                    │
│ - JupyterHub                      │
│ - Dask                            │
│ - Keycloak                        │
│ - Monitoring                      │
└───────────────────────────────────┘
```

### 9.2 Python Adapter Layer Design

**Location in Nebari**: `src/_nebari/stages/02-infrastructure/nic.py`

**Note**: Stage 03 code will be removed entirely, functionality absorbed into stage 02.

**Responsibilities:**
1. Replace both Terraform infrastructure stages
2. Invoke NIC binary with nebari-config.yaml
3. Parse JSON output from NIC
4. Write single outputs.json for application stages
5. Handle errors and provide user feedback
6. Manage NIC state file
7. Coordinate with stage 01 and stage 04+

**Python Adapter Implementation:**
```python
# src/_nebari/stages/02-infrastructure/nic.py

import json
import subprocess
from pathlib import Path
from typing import Dict, Any

class NICAdapter:
    """Adapter for calling Nebari Infrastructure Core from Python"""
    
    def __init__(self, nic_binary_path: str = "nic"):
        self.nic_binary = nic_binary_path
        
    def deploy(self, config_path: str, output_dir: str) -> Dict[str, Any]:
        """
        Deploy infrastructure using NIC
        
        Args:
            config_path: Path to nebari-config.yaml
            output_dir: Directory for outputs.json and state
            
        Returns:
            Parsed outputs from NIC
        """
        state_path = Path(output_dir) / "nic-state.json"
        outputs_path = Path(output_dir) / "outputs.json"
        
        cmd = [
            self.nic_binary,
            "deploy",
            "-f", config_path,
            "--state", str(state_path),
            "--outputs", str(outputs_path),
            "--format", "json"
        ]
        
        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                check=True
            )
            
            # Parse structured output from stdout
            output = json.loads(result.stdout)
            
            # Log progress from stderr
            if result.stderr:
                for line in result.stderr.splitlines():
                    self._log_progress(line)
            
            return output
            
        except subprocess.CalledProcessError as e:
            # Parse error from stdout
            try:
                error = json.loads(e.stdout)
                raise NICError(error["error"]["message"], error)
            except json.JSONDecodeError:
                raise NICError(f"NIC failed: {e.stderr}")
    
    def import_from_terraform(
        self, 
        config_path: str,
        terraform_state_02: str,
        terraform_state_03: str,
        output_dir: str
    ) -> Dict[str, Any]:
        """
        Import existing Terraform-managed infrastructure
        
        Args:
            config_path: Path to nebari-config.yaml
            terraform_state_02: Path to stage 02 Terraform state
            terraform_state_03: Path to stage 03 Terraform state
            output_dir: Directory for NIC state and outputs
            
        Returns:
            Import results
        """
        state_path = Path(output_dir) / "nic-state.json"
        outputs_path = Path(output_dir) / "outputs.json"
        
        cmd = [
            self.nic_binary,
            "import",
            "--from-terraform",
            "--config", config_path,
            "--terraform-state-02", terraform_state_02,
            "--terraform-state-03", terraform_state_03,
            "--output-state", str(state_path),
            "--output-file", str(outputs_path),
            "--format", "json"
        ]
        
        try:
            result = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                check=True
            )
            
            return json.loads(result.stdout)
            
        except subprocess.CalledProcessError as e:
            try:
                error = json.loads(e.stdout)
                raise NICImportError(error["error"]["message"], error)
            except json.JSONDecodeError:
                raise NICImportError(f"Import failed: {e.stderr}")
    
    def validate(self, config_path: str) -> Dict[str, Any]:
        """Validate nebari-config.yaml"""
        cmd = [
            self.nic_binary,
            "validate",
            "-f", config_path,
            "--format", "json"
        ]
        
        result = subprocess.run(cmd, capture_output=True, text=True)
        return json.loads(result.stdout)
    
    def status(self, state_path: str) -> Dict[str, Any]:
        """Get current infrastructure status"""
        cmd = [
            self.nic_binary,
            "status",
            "--state", state_path,
            "--format", "json"
        ]
        
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return json.loads(result.stdout)
    
    def _log_progress(self, line: str):
        """Log progress from NIC stderr"""
        # Parse structured logs from NIC
        try:
            log = json.loads(line)
            level = log.get("level", "info")
            message = log.get("message", line)
            
            if level == "error":
                logger.error(message)
            elif level == "warn":
                logger.warning(message)
            else:
                logger.info(message)
        except json.JSONDecodeError:
            # Plain text log
            logger.info(line)


class NICError(Exception):
    """Base exception for NIC errors"""
    def __init__(self, message: str, details: Dict[str, Any] = None):
        super().__init__(message)
        self.details = details or {}


class NICImportError(NICError):
    """Error during Terraform import"""
    pass
```

**Nebari Stage Implementation:**
```python
# src/_nebari/stages/02-infrastructure/__init__.py

from nebari.stages.base import NebariStage
from .nic import NICAdapter

class InfrastructureStage(NebariStage):
    """
    Stage 02-03: Complete Infrastructure via NIC
    Replaces old Terraform stages 02 and 03
    """
    
    name = "02-infrastructure"
    priority = 20  # After stage 01
    
    def __init__(self, config, *args, **kwargs):
        super().__init__(config, *args, **kwargs)
        self.nic = NICAdapter()
        self.output_dir = self.stage_dir / "infrastructure"
        self.output_dir.mkdir(parents=True, exist_ok=True)
    
    def render(self):
        """Validate configuration"""
        self.logger.info("Validating infrastructure configuration...")
        
        result = self.nic.validate(str(self.config_path))
        
        if not result["valid"]:
            errors = result.get("errors", [])
            raise ValueError(f"Invalid configuration: {errors}")
        
        self.logger.info("Configuration valid")
    
    def deploy(self):
        """Deploy complete infrastructure"""
        self.logger.info("Deploying infrastructure with NIC...")
        
        # Check if this is first deployment or update
        state_file = self.output_dir / "nic-state.json"
        
        if not state_file.exists():
            self.logger.info("First deployment - creating new infrastructure")
        else:
            self.logger.info("Updating existing infrastructure")
        
        # Deploy via NIC
        try:
            result = self.nic.deploy(
                config_path=str(self.config_path),
                output_dir=str(self.output_dir)
            )
            
            self.logger.info(f"Infrastructure deployed successfully")
            self.logger.info(f"Cluster endpoint: {result['cluster']['endpoint']}")
            
            # Write kubeconfig for later stages
            self._write_kubeconfig(result)
            
        except NICError as e:
            self.logger.error(f"Infrastructure deployment failed: {e}")
            if e.details:
                self.logger.error(f"Details: {json.dumps(e.details, indent=2)}")
            raise
    
    def check(self):
        """Verify infrastructure exists and is healthy"""
        self.logger.info("Checking infrastructure status...")
        
        state_file = self.output_dir / "nic-state.json"
        
        if not state_file.exists():
            self.logger.warning("Infrastructure not deployed yet")
            return False
        
        try:
            status = self.nic.status(str(state_file))
            
            if status["healthy"]:
                self.logger.info("Infrastructure is healthy")
                return True
            else:
                self.logger.warning(f"Infrastructure issues: {status['issues']}")
                return False
                
        except Exception as e:
            self.logger.error(f"Failed to check infrastructure: {e}")
            return False
    
    def _write_kubeconfig(self, result: Dict[str, Any]):
        """Write kubeconfig for use by application stages"""
        kubeconfig_path = self.output_dir / "kubeconfig"
        
        # NIC provides kubeconfig in outputs
        with open(kubeconfig_path, "w") as f:
            f.write(result["kubeconfig"]["content"])
        
        self.logger.info(f"Kubeconfig written to {kubeconfig_path}")


def import_from_terraform():
    """
    CLI command to import existing Terraform infrastructure
    
    Usage:
        nebari infrastructure import --from-terraform
    """
    from nebari.config import load_config
    
    config = load_config()
    stage = InfrastructureStage(config)
    
    # Locate Terraform state files
    tf_state_02 = stage.root / "stages" / "02-infrastructure" / "terraform.tfstate"
    tf_state_03 = stage.root / "stages" / "03-kubernetes-initialize" / "terraform.tfstate"
    
    if not tf_state_02.exists():
        raise FileNotFoundError(f"Terraform state not found: {tf_state_02}")
    
    if not tf_state_03.exists():
        raise FileNotFoundError(f"Terraform state not found: {tf_state_03}")
    
    # Import via NIC
    stage.logger.info("Importing infrastructure from Terraform...")
    
    result = stage.nic.import_from_terraform(
        config_path=str(stage.config_path),
        terraform_state_02=str(tf_state_02),
        terraform_state_03=str(tf_state_03),
        output_dir=str(stage.output_dir)
    )
    
    stage.logger.info("Import successful")
    stage.logger.info(f"Imported {result['resource_count']} resources")
    stage.logger.info(f"NIC state: {stage.output_dir / 'nic-state.json'}")
```

### 9.3 Command Mapping

**Nebari Stage Operations → NIC Commands:**

| Nebari Operation | NIC Command | Notes |
|------------------|-------------|-------|
| `nebari deploy` (stages 02-03) | `nic deploy -f nebari-config.yaml` | Creates complete infrastructure |
| `nebari destroy` (stages 02-03) | `nic destroy -f nebari-config.yaml` | Tears down all infrastructure |
| `nebari validate` | `nic validate -f nebari-config.yaml` | Pre-flight checks |
| `nebari infrastructure import` | `nic import --from-terraform` | Import from Terraform |
| Infrastructure status | `nic status --state nic-state.json` | Current state |
| Drift detection | `nic drift detect` | Compare state to cloud+K8s |

**NIC CLI Examples:**

```bash
# Deploy infrastructure
nic deploy \
  -f nebari-config.yaml \
  --state .nebari/stages/02-infrastructure/nic-state.json \
  --outputs .nebari/stages/02-infrastructure/outputs.json

# Import from Terraform
nic import \
  --from-terraform \
  --config nebari-config.yaml \
  --terraform-state-02 .nebari/stages/02-infrastructure/terraform.tfstate \
  --terraform-state-03 .nebari/stages/03-kubernetes-initialize/terraform.tfstate \
  --output-state .nebari/stages/02-infrastructure/nic-state.json \
  --output-file .nebari/stages/02-infrastructure/outputs.json

# Validate configuration
nic validate -f nebari-config.yaml

# Check status
nic status --state .nebari/stages/02-infrastructure/nic-state.json

# Detect drift
nic drift detect --state .nebari/stages/02-infrastructure/nic-state.json

# Destroy infrastructure
nic destroy \
  -f nebari-config.yaml \
  --state .nebari/stages/02-infrastructure/nic-state.json
```

### 9.4 State File Management with Terraform Coexistence

**State File Locations:**
```
.nebari/
├── terraform.tfstate              # ← Terraform state (stages 01, 04+)
├── stages/
│   ├── 01-terraform-state/
│   │   └── terraform.tfstate      # ← Stage 01 creates state backend
│   ├── 02-infrastructure/
│   │   ├── nic-state.json         # ← NIC state (formerly stages 02+03)
│   │   ├── outputs.json           # ← NIC outputs for stage 04+
│   │   └── kubeconfig             # ← Kubeconfig for cluster access
│   ├── 04-kubernetes-ingress/     # ← Note: stage 03 removed
│   │   └── terraform.tfstate      # ← Terraform state for ingress
│   └── 05-kubernetes-keycloak/
│       └── terraform.tfstate
```

**State Consolidation Benefits:**
- Old approach: Two Terraform states (stage 02 and 03)
- New approach: One NIC state (replaces both)
- Simpler state management
- Atomic updates

### 9.5 Error Handling

**NIC Error Output Format (JSON to stdout):**
```json
{
  "success": false,
  "error": {
    "code": "CLUSTER_CREATE_FAILED",
    "message": "Failed to create EKS cluster: InvalidParameterException",
    "details": {
      "provider": "aws",
      "phase": "cluster_creation",
      "resource": "eks_cluster",
      "aws_error": "..."
    },
    "recovery_suggestions": [
      "Check AWS credentials are valid",
      "Verify VPC quotas in your account",
      "Ensure IAM permissions are correct"
    ]
  }
}
```

**Python Error Handling:**
```python
try:
    result = nic.deploy(config_path, output_dir)
except NICError as e:
    logger.error(f"Infrastructure deployment failed: {e}")
    
    if e.details:
        # Show detailed error information
        error_code = e.details.get("error", {}).get("code")
        phase = e.details.get("error", {}).get("details", {}).get("phase")
        
        logger.error(f"Error code: {error_code}")
        logger.error(f"Failed during: {phase}")
        
        # Show recovery suggestions
        suggestions = e.details.get("error", {}).get("recovery_suggestions", [])
        if suggestions:
            logger.info("Suggested fixes:")
            for suggestion in suggestions:
                logger.info(f"  - {suggestion}")
    
    # Provide Nebari-specific guidance
    logger.info("For help, see: https://docs.nebari.dev/troubleshooting")
    
    raise DeploymentError(f"Infrastructure stage failed: {e}")
```

### 9.6 Transition Strategy: Side-by-Side Operation

**Feature Flag in nebari-config.yaml:**
```yaml
# Feature flag for transition period
infrastructure:
  engine: terraform  # or "nic"
```

**Nebari Behavior Based on Flag:**

**terraform mode (default initially):**
```python
if config.infrastructure.engine == "terraform":
    # Use old Terraform stages 02 and 03
    run_terraform_stage_02()
    run_terraform_stage_03()
```

**nic mode:**
```python
if config.infrastructure.engine == "nic":
    # Use new NIC unified stages 02-03
    run_nic_infrastructure_stage()
```

**Transition Phases:**

**Phase 1: Development (months 1-3)**
- NIC in parallel development
- Terraform remains default
- Select users test NIC on new clusters
- `engine: terraform` (default)

**Phase 2: Beta (months 4-6)**
- NIC available via `engine: nic`
- Migration tooling available
- Documentation for switchers
- Community testing

**Phase 3: Default (month 7+)**
- `engine: nic` becomes default for new deployments
- Terraform still available via `engine: terraform`
- Deprecation notice for Terraform path
- Stage 03 code marked deprecated

**Phase 4: Terraform Removal (month 12+)**
- Remove Terraform stages 02-03 code
- Remove stage 03 directory
- NIC only path
- Import tool mandatory for old clusters

---

## 10. Migration Strategy

### 10.1 Migration Philosophy

**Core Principle**: v1.0 import transfers management without changing infrastructure.

**Goals:**
1. Zero downtime for running clusters
2. Zero resource changes
3. Zero data loss
4. Maintain kubeconfig and access
5. Rollback capability
6. **Application stages (04+) continue working unchanged**

**Key Simplification**: v1.0 doesn't change resources, so no data migration needed!

### 10.2 Import Process: Management Transfer Only

**Step 1: Backup**
```bash
nebari infrastructure backup-state

# Creates backup of:
# - Stage 01 Terraform state
# - Stage 02 Terraform state
# - Stage 03 Terraform state
# - Stage 04+ Terraform states
# - All in: .nebari/backups/state-backup-<timestamp>.tar.gz
```

**Step 2: Import (Management Transfer)**
```bash
nebari infrastructure import --from-terraform
```

**What Happens:**

1. **Read Terraform States (02 & 03)**
   ```
   Reading Terraform state files...
   ✓ Found stage 02: .nebari/stages/02-infrastructure/terraform.tfstate
   ✓ Found stage 03: .nebari/stages/03-kubernetes-initialize/terraform.tfstate
   ```

2. **Extract Resource IDs**
   ```
   Extracting resource information...
   
   Cloud Resources (Stage 02):
   ✓ VPC: vpc-123 (10.0.0.0/16)
   ✓ EKS Cluster: my-nebari (1.28)
   ✓ Node Groups: general, user, worker
   ✓ EBS Volumes: vol-123 (100GB gp3)
   ✓ Security Groups: 3 groups
   ✓ IAM Roles: 5 roles
   
   Kubernetes Resources (Stage 03):
   ✓ Namespaces: dev, prod, monitoring
   ✓ Service Accounts: 8 accounts
   ✓ Storage Classes: gp3 (default)
   ✓ Roles: 12 roles
   ✓ Role Bindings: 15 bindings
   ```

3. **Verify Resources Exist**
   ```
   Verifying resources in AWS...
   ✓ VPC vpc-123 exists
   ✓ EKS cluster my-nebari exists and is ACTIVE
   ✓ Node group general exists (2/5 nodes)
   ✓ Node group user exists (0/10 nodes)
   ✓ EBS volume vol-123 exists
   
   Verifying resources in Kubernetes...
   ✓ Namespace dev exists
   ✓ Namespace prod exists
   ✓ StorageClass gp3 exists
   ✓ ServiceAccount default exists
   ```

4. **Generate NIC State**
   ```
   Creating NIC state file...
   ✓ State file: .nebari/stages/02-infrastructure/nic-state.json
   ✓ Tracking 45 resources
   ✓ All marked as imported (preserve as-is)
   ```

5. **Generate Outputs**
   ```
   Generating outputs for application stages...
   ✓ Outputs file: .nebari/stages/02-infrastructure/outputs.json
   ✓ Cluster endpoint: https://ABC.eks.amazonaws.com
   ✓ Kubeconfig path: .nebari/stages/02-infrastructure/kubeconfig
   ```

**Step 3: Validation**
```bash
nebari infrastructure validate-import
```

```
Validating import...

Resource Verification:
✓ All cloud resources found (25/25)
✓ All K8s resources found (20/20)
✓ No missing resources
✓ No unexpected resources

Configuration Match:
✓ Node groups match nebari-config.yaml
✓ Storage configuration matches
✓ K8s version matches (1.28)
✓ Instance types match

Outputs Verification:
✓ outputs.json generated
✓ Contains all required fields for stage 04+
✓ Kubeconfig accessible
✓ Cluster endpoint reachable

State File:
✓ nic-state.json valid
✓ All resources marked as imported
✓ Configuration hash generated

Ready for management transfer! ✓
```

**Step 4: Test Deploy (Dry Run)**
```bash
nebari deploy --dry-run
```

```
Dry run - no changes will be made

Stage 01 (Terraform - State Backend): No changes
Stage 02-03 (NIC - Infrastructure): No changes needed
  ✓ VPC vpc-123 exists (managed by NIC now)
  ✓ Cluster my-nebari exists (managed by NIC now)
  ✓ Node groups exist (managed by NIC now)
  ✓ Storage exists (managed by NIC now)
  ✓ Namespaces exist (managed by NIC now)
  ✓ RBAC exists (managed by NIC now)

Stage 04+ (Terraform - Applications): No changes
  ✓ Can read outputs.json
  ✓ All required values present

Summary: 0 changes, 45 resources now managed by NIC
```

**Step 5: Cutover**
```bash
nebari infrastructure switch-to-nic
```

```
Switching to NIC management...

✓ Archived Terraform stage 02 code
✓ Archived Terraform stage 03 code
✓ Updated nebari-config.yaml (engine: nic)
✓ Backed up Terraform states
✓ NIC state active

Management transfer complete!

Next steps:
1. Run: nebari deploy
   (Should show "no changes needed")
2. Monitor for 24-48 hours
3. If issues: nebari infrastructure rollback
4. If stable: Remove Terraform backups after 30 days
```

**Step 6: Verification**
```bash
nebari deploy
```

```
Stage 01: ✓ No changes
Stage 02-03 (NIC): ✓ No changes needed (managing 45 resources)
Stage 04+: ✓ No changes

Deployment complete (0 changes)
All infrastructure managed by NIC
```

### 10.3 Rollback Plan

**If Import Validation Fails:**
```bash
# Nothing changed yet, just don't proceed
# Terraform states still intact
# Continue using Terraform
```

**If Issues Found After Cutover:**
```bash
nebari infrastructure rollback
```

```
Rolling back to Terraform management...

✓ Restored Terraform stage 02 code
✓ Restored Terraform stage 03 code  
✓ Updated nebari-config.yaml (engine: terraform)
✓ Terraform states still intact
✓ Removed NIC state file

Rollback complete. Now using Terraform again.
```

**Critical Guarantee**: Since v1.0 doesn't change resources, rollback is safe and simple.

### 10.4 Import Tool Design

**Terraform State Parser:**
```go
func ParseTerraformState(stage02Path, stage03Path string) (*ImportedResources, error) {
    // Read both state files
    state02, err := readTerraformState(stage02Path)
    if err != nil {
        return nil, fmt.Errorf("failed to read stage 02 state: %w", err)
    }
    
    state03, err := readTerraformState(stage03Path)
    if err != nil {
        return nil, fmt.Errorf("failed to read stage 03 state: %w", err)
    }
    
    // Extract resources from stage 02
    cloudResources := extractCloudResources(state02)
    
    // Extract resources from stage 03
    k8sResources := extractK8sResources(state03)
    
    return &ImportedResources{
        Cloud:      cloudResources,
        Kubernetes: k8sResources,
    }, nil
}

func extractCloudResources(state *TerraformState) *CloudResources {
    resources := &CloudResources{}
    
    for _, resource := range state.Resources {
        switch resource.Type {
        case "aws_vpc":
            resources.VPC = extractVPC(resource)
        case "aws_eks_cluster":
            resources.Cluster = extractCluster(resource)
        case "aws_eks_node_group":
            resources.NodeGroups = append(resources.NodeGroups, extractNodeGroup(resource))
        case "aws_ebs_volume":
            resources.EBSVolumes = append(resources.EBSVolumes, extractEBSVolume(resource))
        // ... more resource types
        }
    }
    
    return resources
}

func extractK8sResources(state *TerraformState) *KubernetesResources {
    resources := &KubernetesResources{}
    
    for _, resource := range state.Resources {
        switch resource.Type {
        case "kubernetes_namespace":
            resources.Namespaces = append(resources.Namespaces, extractNamespace(resource))
        case "kubernetes_storage_class":
            resources.StorageClasses = append(resources.StorageClasses, extractStorageClass(resource))
        case "kubernetes_service_account":
            resources.ServiceAccounts = append(resources.ServiceAccounts, extractServiceAccount(resource))
        // ... more resource types
        }
    }
    
    return resources
}
```

**Resource Verification:**
```go
func VerifyImportedResources(imported *ImportedResources, provider Provider) error {
    // Verify cloud resources
    for _, vpc := range imported.Cloud.VPCs {
        actual, err := provider.GetVPC(vpc.ID)
        if err != nil {
            return fmt.Errorf("VPC %s not found: %w", vpc.ID, err)
        }
        
        if actual.CIDR != vpc.CIDR {
            return fmt.Errorf("VPC %s CIDR mismatch: state=%s actual=%s",
                vpc.ID, vpc.CIDR, actual.CIDR)
        }
    }
    
    // Verify K8s resources
    for _, ns := range imported.Kubernetes.Namespaces {
        actual, err := provider.GetNamespace(ns.Name)
        if err != nil {
            return fmt.Errorf("Namespace %s not found: %w", ns.Name, err)
        }
    }
    
    return nil
}
```

**NIC State Generation:**
```go
func GenerateNICState(imported *ImportedResources, config *Config) (*State, error) {
    state := &State{
        Version:        version.Current,
        FormatVersion:  "1",
        Provider:       config.Provider,
        ProjectName:    config.ProjectName,
        ImportedFromTerraform: true,
        
        Cloud: CloudState{
            VPC:        imported.Cloud.VPC,
            Cluster:    imported.Cloud.Cluster,
            NodeGroups: imported.Cloud.NodeGroups,
            Storage:    imported.Cloud.Storage,
        },
        
        Kubernetes: KubernetesState{
            Namespaces:      imported.Kubernetes.Namespaces,
            ServiceAccounts: imported.Kubernetes.ServiceAccounts,
            StorageClasses:  imported.Kubernetes.StorageClasses,
            Roles:           imported.Kubernetes.Roles,
        },
        
        Metadata: StateMetadata{
            CreatedAt:  time.Now(),
            UpdatedAt:  time.Now(),
            NICVersion: version.Current,
            ConfigurationHash: hashConfig(config),
        },
    }
    
    return state, nil
}
```

---

## 11. Testing Strategy

### 11.1 Testing Levels

**Unit Tests (Go):**
- Provider interface implementations
- Configuration parsing
- State management (read/write/lock/unlock)
- Output generation (unified format)
- Error handling
- Drift detection logic
- Declarative reconciliation logic
- Target: >80% coverage

**Integration Tests (Go):**
- Provider operations against mock cloud APIs
- Kubernetes operations against mock K8s API server
- Configuration end-to-end parsing
- State file operations (all backends)
- Output format validation
- Import from Terraform states
- Declarative reconciliation end-to-end

**Provider Tests (Real Cloud):**
- Deploy actual infrastructure with NIC
- Verify resources match Terraform exactly
- Verify outputs.json format
- Test import from real Terraform state
- Test updates (add node group, etc.)
- Test rollback
- Clean up resources
- Run in CI for provider changes

**End-to-End Tests (Full Nebari Stack):**
- Fresh deploy: Stage 01 (TF) → Stages 02-03 (NIC) → Stage 04+ (TF)
- Import: Terraform stages → NIC import → Continue with stage 04+
- Verify unified infrastructure deployment
- Verify application stages work with NIC outputs
- Verify K8s resources accessible by applications
- Migration scenarios
- State management
- Error handling

### 11.2 Critical Test Cases

**1. Fresh Deployment Test:**
```bash
# New Nebari deployment with NIC
nebari init --provider aws
nebari deploy

# Verify:
# - Infrastructure created
# - Matches what Terraform would create
# - Application stages work
# - Users can access JupyterHub
```

**2. Import from Terraform Test:**
```bash
# Existing Terraform-managed cluster
nebari infrastructure import --from-terraform

# Verify:
# - All resources imported
# - State file correct
# - Outputs.json correct
# - No changes on next deploy
# - Application stages still work
```

**3. Idempotency Test:**
```bash
# Deploy twice
nebari deploy  # First
nebari deploy  # Second

# Verify second deploy shows "no changes"
```

**4. Add Node Group Test:**
```yaml
# Add worker node group to config
node_groups:
  worker:
    instance_type: m5.xlarge
    min_size: 0
    max_size: 20
```

```bash
nebari deploy

# Verify:
# - Only worker node group created
# - Other resources unchanged
```

**5. Drift Detection Test:**
```bash
# Manually change max_size in AWS console
# Then:
nic drift detect

# Verify:
# - Drift detected
# - Correct difference shown
```

**6. Rollback Test:**
```bash
nebari infrastructure import --from-terraform
nebari infrastructure rollback

# Verify:
# - Back to Terraform
# - No resource changes
# - Everything still works
```

**7. Stage Boundary Test:**
```bash
# Deploy with NIC
nebari deploy

# Verify:
# - Stage 04+ can read outputs.json
# - Ingress deploys successfully
# - JupyterHub accessible
```

**8. State Locking Test:**
```bash
# Run two deploys concurrently
nebari deploy &
nebari deploy &

# Verify:
# - One gets lock
# - One waits or fails with lock error
# - No state corruption
```

### 11.3 Test Infrastructure

**Mock Services:**
- Mock AWS APIs (localstack or custom)
- Mock GCP APIs (emulators where available)
- Mock Azure APIs
- Mock Kubernetes API (client-go fake clientset)

**Test Clusters:**
- Dedicated AWS/GCP/Azure test accounts
- Automated cleanup after tests
- Cost monitoring
- Isolated from production

**CI/CD Pipeline:**
1. **Every Commit**: Unit tests
2. **Every PR**: Integration tests + mock provider tests
3. **Provider Changes**: Real cloud provider tests
4. **Nightly**: Full E2E Nebari tests (all providers)
5. **Weekly**: Import/migration tests

---

## 12. Timeline and Milestones

### 12.1 Phase 1: Foundation (Months 1-2)

**Month 1: Core Framework**
- Provider interface design (cloud + K8s)
- Registry and loader implementation
- Configuration parser for nebari-config.yaml
- Custom state management implementation (JSON)
- State backends (local, S3, GCS, Azure Blob)
- State locking implementation
- Kubernetes client-go integration
- Output generation format (unified)
- CLI framework
- Declarative reconciliation engine

**Month 2: AWS Provider**
- AWS provider implementation (cloud)
- EKS cluster deployment (exact Terraform match)
- VPC and networking (exact match)
- Node group management (exact match)
- EBS storage setup (exact match)
- Namespace creation
- RBAC setup (exact match)
- StorageClass creation (EBS integration, exact match)
- ServiceAccount configuration (IRSA, exact match)
- Unified outputs.json generation
- Test with mock stage 04
- Import from Terraform tool
- Unit and integration tests
- Documentation

**Milestone 1.0: AWS Management Transfer** - Single provider, complete management transfer capability

### 12.2 Phase 2: Multi-Provider (Months 3-4)

**Month 3: GCP and Local Providers**
- GCP provider implementation (cloud + K8s)
- GKE cluster deployment (exact match)
- Filestore + StorageClass integration (exact match)
- Workload Identity + ServiceAccount binding (exact match)
- Local provider (K3s) with unified setup
- Unified output format standardization
- Cross-provider testing
- Import tool for all providers
- Configuration validation

**Month 4: Azure Provider**
- Azure provider implementation (cloud + K8s)
- AKS deployment (exact match)
- Azure Disk + StorageClass integration (exact match)
- Managed Identity + ServiceAccount binding (exact match)
- Provider parity testing
- All providers generate unified outputs
- Integration test suite complete
- Performance benchmarking

**Milestone 2.0: All Providers** - Management transfer capability for all providers

### 12.3 Phase 3: Nebari Integration (Months 5-6)

**Month 5: Integration Layer**
- Python adapter for unified stages 02-03
- Replace both Terraform stages in Nebari
- Remove stage 03 code from Nebari
- Ensure stage 01 and 04+ unchanged
- Subprocess communication protocol
- Error handling (cloud + K8s phases)
- State file coordination with Nebari
- Import CLI command in Nebari

**Month 6: Migration Tools & Testing**
- Terraform state import tool (both stages 02+03)
- Unified output compatibility validation
- Rollback procedures implemented
- Migration documentation complete
- Beta testing with select users
- Community feedback incorporation

**Milestone 3.0: Nebari Beta** - NIC available in Nebari behind feature flag, replaces stages 02+03

### 12.4 Phase 4: Production Ready (Months 7-9)

**Month 7-8: Hardening**
- Bug fixes from beta testing
- Performance optimization
- Error handling improvements
- Comprehensive documentation
- Security audit
- Load testing
- Extensive testing with real Nebari deployments
- Production deployment guides

**Month 9: Production Release**
- NIC becomes default for stages 02-03
- Migration guide published
- Video tutorials
- Support processes established
- Terraform stages 02-03 deprecated
- Stage 03 removed from Nebari codebase
- v1.0 release announcement

**Milestone 4.0: Production Release** - NIC default for complete infrastructure, proven in production

### 12.5 Phase 5: v2.0 Planning (Months 10-12)

**Months 10-12: Future Enhancements**
- Evaluate v1.0 adoption and feedback
- Plan infrastructure improvements for v2.0:
  - Storage upgrades (EBS → EFS)
  - Advanced autoscaling (Karpenter)
  - Spot instances
  - Multi-region support
- External plugin system design (RPC)
- Community feature requests
- v2.0 roadmap published

**Milestone 5.0: v2.0 Roadmap** - Clear path for infrastructure improvements

---

## 13. Open Questions

### 13.1 Technical Questions

**Q1: Should we version the state format?**
- **Current Thinking**: Yes, use `format_version` field for future schema changes
- **Decision Needed**: Migration strategy between format versions
- **Owner**: State management team
- **Deadline**: Before Month 2

**Q2: How do we handle state format evolution?**
- **Current Thinking**: Auto-migrate on read, similar to database migrations
- **Decision Needed**: Approval of migration approach
- **Owner**: Architecture team
- **Deadline**: Before Month 3

**Q3: Should we support state encryption at rest?**
- **Current Thinking**: Not in v1.0, add in v1.x if needed
- **Decision Needed**: Security requirements for state
- **Owner**: Security team
- **Deadline**: Before Month 5

**Q4: How do we handle cloud provider API version changes?**
- **Example**: AWS SDK v2 breaking changes
- **Current Thinking**: Test against latest SDKs, document supported versions
- **Decision Needed**: SDK update policy
- **Owner**: Provider team
- **Deadline**: Before Month 3

**Q5: Should NIC support K8s operator pattern for continuous reconciliation?**
- **Current Thinking**: Not in v1.0, manual reconciliation via `nebari deploy`
- **Decision Needed**: Future roadmap for continuous reconciliation
- **Owner**: Architecture team
- **Deadline**: Before v2.0 planning

### 13.2 Integration Questions

**Q6: Should NIC be a separate repository or part of Nebari monorepo?**
- **Options**: 
  - A) Separate repo (nebari-infrastructure-core)
  - B) Subdirectory in Nebari repo
- **Tradeoffs**:
  - A: Clean separation, independent releases, reusable, better for standalone use
  - B: Easier coordination, simpler for Nebari contributors
- **Current Thinking**: Separate repo (nebari-infrastructure-core)
- **Decision Needed**: Final repository structure
- **Owner**: Project leads
- **Deadline**: Month 1

**Q7: How do we version NIC independently from Nebari?**
- **Current Thinking**: SemVer for NIC, Nebari pins NIC version in dependencies
- **Decision Needed**: Compatibility matrix strategy, deprecation policy
- **Owner**: Release team
- **Deadline**: Before Month 3

**Q8: Should we support older Nebari versions?**
- **Example**: Nebari 2022.x configs
- **Current Thinking**: Support Nebari 2023.x+, document migration for older versions
- **Decision Needed**: Support policy
- **Owner**: Product team
- **Deadline**: Before Month 5

### 13.3 Migration Questions

**Q9: Should import be automatic or require explicit user action?**
- **Current Thinking**: Explicit command: `nebari infrastructure import --from-terraform`
- **Decision Needed**: UX for import process
- **Owner**: UX team
- **Deadline**: Before Month 5

**Q10: How long do we support dual-path (Terraform and NIC)?**
- **Current Thinking**: 6 months overlap (beta), then deprecate Terraform path
- **Decision Needed**: Support timeline, deprecation schedule
- **Owner**: Product team
- **Deadline**: Before Month 6

**Q11: What if users have heavily customized Terraform stages?**
- **Example**: Custom modules, additional resources
- **Current Thinking**: Document limitations, provide manual migration guide
- **Decision Needed**: Support strategy for customizations
- **Owner**: Support team
- **Deadline**: Before Month 5

### 13.4 Future Planning Questions

**Q12: When should we introduce infrastructure improvements (v2.0)?**
- **Current Thinking**: After v1.0 in production for 6+ months
- **Decision Needed**: Criteria for v2.0 start
- **Owner**: Product team
- **Deadline**: Month 12

**Q13: Should we support other K8s distributions (EKS Anywhere, etc.)?**
- **Current Thinking**: Defer to v2.0+, focus on EKS/GKE/AKS/K3s
- **Decision Needed**: Expansion roadmap
- **Owner**: Product team
- **Deadline**: v2.0 planning

**Q14: Should NIC be marketed as standalone tool?**
- **Current Thinking**: Yes, but Nebari is primary use case
- **Decision Needed**: Branding, documentation strategy
- **Owner**: Product/Marketing team
- **Deadline**: Before Month 8

---

## 14. Appendix

### 14.1 Glossary

| Term | Definition |
|------|------------|
| **NIC** | Nebari Infrastructure Core - this project |
| **Nebari** | Data science platform built on Kubernetes |
| **Stage** | Phase in Nebari deployment |
| **Management Transfer** | Moving management from Terraform to NIC without changing resources |
| **Declarative** | Specify desired state, tool figures out how to achieve it |
| **Reconciliation** | Process of making actual state match desired state |
| **Provider** | Cloud-specific implementation (AWS, GCP, Azure, Local) |
| **State File** | JSON file tracking complete infrastructure (cloud + K8s) |
| **Terraform State** | HCL state file for Terraform-managed resources (stages 01, 04+) |
| **outputs.json** | Unified communication file between NIC and application stages |
| **Import** | Process of adopting existing Terraform-managed resources into NIC |
| **Drift** | Difference between state file and actual infrastructure |

### 14.2 State File Example

**Complete State File Structure:**
```json
{
  "version": "1.0.0",
  "format_version": "1",
  "provider": "aws",
  "project_name": "my-nebari",
  "region": "us-west-2",
  "imported_from_terraform": true,
  
  "cloud": {
    "vpc": {
      "id": "vpc-0a1b2c3d4e5f6g7h8",
      "cidr": "10.0.0.0/16",
      "subnets": [
        {
          "id": "subnet-0a1b2c3d",
          "cidr": "10.0.1.0/24",
          "availability_zone": "us-west-2a",
          "type": "public"
        },
        {
          "id": "subnet-4e5f6g7h",
          "cidr": "10.0.2.0/24",
          "availability_zone": "us-west-2b",
          "type": "public"
        },
        {
          "id": "subnet-8i9j0k1l",
          "cidr": "10.0.10.0/24",
          "availability_zone": "us-west-2a",
          "type": "private"
        },
        {
          "id": "subnet-2m3n4o5p",
          "cidr": "10.0.11.0/24",
          "availability_zone": "us-west-2b",
          "type": "private"
        }
      ],
      "internet_gateway_id": "igw-0a1b2c3d",
      "nat_gateway_ids": ["nat-0a1b2c3d", "nat-4e5f6g7h"]
    },
    
    "cluster": {
      "name": "my-nebari",
      "id": "my-nebari",
      "arn": "arn:aws:eks:us-west-2:123456789012:cluster/my-nebari",
      "endpoint": "https://ABC123DEF456.gr7.us-west-2.eks.amazonaws.com",
      "ca_certificate": "LS0tLS1CRUdJTi...",
      "version": "1.28",
      "platform_version": "eks.8",
      "status": "ACTIVE",
      "oidc_issuer": "https://oidc.eks.us-west-2.amazonaws.com/id/ABC123DEF456",
      "created_at": "2025-01-15T10:30:00Z"
    },
    
    "node_groups": [
      {
        "name": "general",
        "arn": "arn:aws:eks:us-west-2:123456789012:nodegroup/my-nebari/general/abc-123-def-456",
        "cluster_name": "my-nebari",
        "instance_types": ["m5.xlarge"],
        "ami_type": "AL2_x86_64",
        "disk_size": 100,
        "scaling_config": {
          "min_size": 1,
          "max_size": 5,
          "desired_size": 2
        },
        "subnet_ids": ["subnet-8i9j0k1l", "subnet-2m3n4o5p"],
        "status": "ACTIVE"
      },
      {
        "name": "user",
        "arn": "arn:aws:eks:us-west-2:123456789012:nodegroup/my-nebari/user/ghi-789-jkl-012",
        "cluster_name": "my-nebari",
        "instance_types": ["m5.2xlarge"],
        "ami_type": "AL2_x86_64",
        "disk_size": 200,
        "scaling_config": {
          "min_size": 0,
          "max_size": 10,
          "desired_size": 0
        },
        "subnet_ids": ["subnet-8i9j0k1l", "subnet-2m3n4o5p"],
        "status": "ACTIVE"
      }
    ],
    
    "storage": {
      "type": "ebs",
      "volumes": [
        {
          "id": "vol-0a1b2c3d4e5f6g7h8",
          "size": 100,
          "type": "gp3",
          "iops": 3000,
          "throughput": 125,
          "encrypted": true,
          "availability_zone": "us-west-2a"
        }
      ]
    },
    
    "security_groups": [
      {
        "id": "sg-0a1b2c3d",
        "name": "my-nebari-cluster-sg",
        "description": "EKS cluster security group"
      },
      {
        "id": "sg-4e5f6g7h",
        "name": "my-nebari-node-sg",
        "description": "EKS node security group"
      }
    ],
    
    "iam_roles": [
      {
        "name": "my-nebari-cluster-role",
        "arn": "arn:aws:iam::123456789012:role/my-nebari-cluster-role"
      },
      {
        "name": "my-nebari-node-role",
        "arn": "arn:aws:iam::123456789012:role/my-nebari-node-role"
      }
    ]
  },
  
  "kubernetes": {
    "namespaces": [
      {
        "name": "dev",
        "labels": {
          "managed-by": "nebari-infrastructure-core",
          "project": "my-nebari"
        },
        "created_at": "2025-01-15T11:00:00Z"
      },
      {
        "name": "prod",
        "labels": {
          "managed-by": "nebari-infrastructure-core",
          "project": "my-nebari"
        },
        "created_at": "2025-01-15T11:00:00Z"
      },
      {
        "name": "monitoring",
        "labels": {
          "managed-by": "nebari-infrastructure-core",
          "project": "my-nebari"
        },
        "created_at": "2025-01-15T11:00:00Z"
      }
    ],
    
    "service_accounts": [
      {
        "name": "default",
        "namespace": "dev",
        "annotations": {
          "eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-nebari-sa-dev"
        }
      },
      {
        "name": "monitoring",
        "namespace": "monitoring",
        "annotations": {
          "eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-nebari-sa-monitoring"
        }
      }
    ],
    
    "storage_classes": [
      {
        "name": "gp3",
        "provisioner": "ebs.csi.aws.com",
        "parameters": {
          "type": "gp3",
          "encrypted": "true",
          "iops": "3000",
          "throughput": "125"
        },
        "volume_binding_mode": "WaitForFirstConsumer",
        "is_default": true
      }
    ],
    
    "roles": [
      {
        "name": "admin",
        "namespace": "dev",
        "rules": [
          {
            "api_groups": ["*"],
            "resources": ["*"],
            "verbs": ["*"]
          }
        ]
      }
    ],
    
    "cluster_roles": [
      {
        "name": "cluster-admin",
        "rules": [
          {
            "api_groups": ["*"],
            "resources": ["*"],
            "verbs": ["*"]
          }
        ]
      }
    ]
  },
  
  "metadata": {
    "created_at": "2025-01-15T10:00:00Z",
    "updated_at": "2025-01-27T15:30:00Z",
    "nic_version": "1.0.0",
    "configuration_hash": "abc123def456ghi789jkl012mno345pqr678",
    "last_successful_deploy": "2025-01-27T15:30:00Z",
    "deploy_count": 5
  }
}
```

### 14.3 Configuration Examples

**Minimal Configuration:**
```yaml
project_name: my-nebari
provider: aws

amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.28"
  
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 5
```

**Full Configuration:**
```yaml
project_name: my-production-nebari
namespace: production
provider: aws

amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.28"
  availability_zones:
    - us-west-2a
    - us-west-2b
  vpc_cidr: "10.0.0.0/16"
  
  node_groups:
    general:
      instance_type: m5.xlarge
      min_size: 2
      max_size: 10
      disk_size: 100
      labels:
        role: general
      taints:
        - key: "workload"
          value: "general"
          effect: "NoSchedule"
    
    user:
      instance_type: m5.2xlarge
      min_size: 0
      max_size: 20
      disk_size: 200
      labels:
        role: user
    
    worker:
      instance_type: m5.xlarge
      min_size: 1
      max_size: 50
      disk_size: 100
      labels:
        role: worker
  
  storage:
    type: ebs
    size: 500
```

### 14.4 References

**Nebari Documentation:**
- [Nebari Docs](https://www.nebari.dev/docs)
- [Nebari GitHub](https://github.com/nebari-dev/nebari)
- [Nebari Architecture](https://www.nebari.dev/docs/about/architecture)
- [Nebari Stages](https://www.nebari.dev/docs/references/stages)

**Technical References:**
- [Kubernetes Client-Go](https://github.com/kubernetes/client-go)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [AWS SDK for Go v2](https://aws.github.io/aws-sdk-go-v2/)
- [AWS EKS Best Practices](https://aws.github.io/aws-eks-best-practices/)
- [GCP Cloud SDK for Go](https://cloud.google.com/go/docs/reference)
- [GCP GKE Best Practices](https://cloud.google.com/kubernetes-engine/docs/best-practices)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
- [Declarative Application Management in Kubernetes](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/)

### 14.5 Decision Log

| ID | Date | Decision | Rationale | Status |
|----|------|----------|-----------|--------|
| DEC-001 | 2025-01-27 | Unify stages 02-03 into single NIC implementation | Natural infrastructure boundary, better performance | ✅ Approved |
| DEC-002 | 2025-01-27 | Custom state management (not OpenTofu/Terraform) | Simpler for our use case, no external dependency | ✅ Approved |
| DEC-003 | 2025-01-27 | Declarative semantics with native SDKs | Best of both worlds: declarative + direct control | ✅ Approved |
| DEC-004 | 2025-01-27 | v1.0 management transfer only (no infrastructure changes) | Risk reduction, data safety, trust building | ✅ Approved |
| DEC-005 | 2025-01-27 | Use compiled plugins with explicit registration | Clarity, testability, future-proof | ✅ Approved |
| DEC-006 | 2025-01-27 | Integrate via subprocess | Simplicity, independence | ✅ Approved |
| DEC-007 | 2025-01-27 | JSON state format | Human-readable, git-friendly, simple | ✅ Approved |
| DEC-008 | 2025-01-27 | Separate repository (nebari-infrastructure-core) | Clean separation, reusability, independent releases | ✅ Approved |

### 14.6 Success Criteria

**v1.0 Release Criteria:**
- [ ] All 4 providers implemented (AWS, GCP, Azure, Local)
- [ ] Import from Terraform works for all providers
- [ ] **Zero resource changes during import** (exact replication)
- [ ] Stages 02-03 fully replaced with unified NIC
- [ ] Stage 03 removed from Nebari codebase
- [ ] Application stages (04+) work unchanged with NIC outputs
- [ ] Import tool validates >95% of real Terraform states
- [ ] Unified output format 100% compatible with application stages
- [ ] Declarative reconciliation working correctly
- [ ] State management tested with all backends
- [ ] Documentation complete (user guides, migration guides, API docs)
- [ ] Security audit passed
- [ ] 10+ successful production imports
- [ ] Performance equal to or better than Terraform
- [ ] Zero data loss in all test scenarios

**Adoption Criteria:**
- [ ] 25% of new Nebari deployments use NIC by Month 9
- [ ] 50% of new Nebari deployments use NIC by Month 12
- [ ] <3% rollback rate from NIC to Terraform
- [ ] Community reports easier infrastructure management
- [ ] Community reports equal or better reliability than Terraform
- [ ] No integration issues with application stages (04+)
- [ ] No resource management issues
- [ ] No data loss incidents
- [ ] No major bugs in issue tracker

### 14.7 Risks and Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| **Import tool bugs** | High | Medium | Extensive testing with real Terraform states, validation checks |
| **State management bugs** | High | Low | Comprehensive testing, use proven locking patterns, automated backups |
| **SDK breaking changes** | Medium | Medium | Pin SDK versions, test against SDK updates, version compatibility matrix |
| **Drift in imported clusters** | Medium | Medium | Clear documentation, drift detection, validation tools |
| **Application stage breakage** | Critical | Low | Extensive boundary testing, output format validation |
| **Rollback complexity** | Medium | Low | Simple rollback procedure (v1.0 doesn't change resources) |
| **Performance regression** | Medium | Low | Benchmarking, performance tests in CI |
| **Adoption resistance** | Medium | Medium | Clear value proposition (no changes = low risk), excellent docs, support |

---

**Document Status**: ✅ Ready for Review  

**Next Steps**: 
1. Team review and feedback (Week 1)
2. Architecture review with Nebari core team (Week 2)
3. State management design review (Week 2)
4. Declarative reconciliation design review (Week 2)
5. Provider porting strategy validation (Week 3)
6. Application stage boundary validation - test with stage 04+ (Week 3)
7. Security and compliance review (Week 3)
8. Approval and implementation kickoff (Week 4)
9. Create GitHub repository: `nebari-dev/nebari-infrastructure-core` (Week 4)
10. Project board setup and sprint planning (Week 4)

**Approval Required From:**
- [ ] Nebari Project Leads
- [ ] Nebari Core Maintainers
- [ ] Nebari Stage 04+ Maintainers (for output format approval)
- [ ] Kubernetes/RBAC experts
- [ ] Security Team
- [ ] DevOps/Infrastructure Team
- [ ] Community Representatives
