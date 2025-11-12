# Provider Architecture

### 8.1 Provider Interface

**Common Interface:**
```go
package provider

type Provider interface {
    // Name returns the provider name (aws, gcp, azure, local)
    Name() string

    // Validate checks if the configuration is valid for this provider
    Validate(ctx context.Context, config Config) error

    // Provision creates all cloud infrastructure and Kubernetes cluster
    Provision(ctx context.Context, config Config) (*InfrastructureState, error)

    // Query retrieves current infrastructure state from cloud APIs
    Query(ctx context.Context, config Config) (*InfrastructureState, error)

    // Reconcile brings actual state to match desired state
    Reconcile(ctx context.Context, desired Config, actual *InfrastructureState) error

    // Destroy tears down all infrastructure
    Destroy(ctx context.Context, config Config) error

    // GetKubeconfig returns kubeconfig for the cluster
    GetKubeconfig(ctx context.Context) ([]byte, error)
}

type InfrastructureState struct {
    VPC            VPCState
    Kubernetes     KubernetesState
    NodePools      []NodePoolState
    Storage        StorageState
    LoadBalancers  []LoadBalancerState
}
```

### 8.2 Provider Registration

**Explicit Registration (No Blank Imports):**
```go
// cmd/nic/main.go
package main

import (
    "github.com/nebari-dev/nic/pkg/provider"
    "github.com/nebari-dev/nic/pkg/provider/aws"
    "github.com/nebari-dev/nic/pkg/provider/gcp"
    "github.com/nebari-dev/nic/pkg/provider/azure"
    "github.com/nebari-dev/nic/pkg/provider/local"
)

func main() {
    // Explicit provider registration
    provider.Register("aws", aws.NewProvider)
    provider.Register("gcp", gcp.NewProvider)
    provider.Register("azure", azure.NewProvider)
    provider.Register("local", local.NewProvider)

    // Run CLI
    cli.Execute()
}
```

**Provider Registry:**
```go
// pkg/provider/registry.go
package provider

type ProviderFactory func(config Config) (Provider, error)

var registry = make(map[string]ProviderFactory)

func Register(name string, factory ProviderFactory) {
    registry[name] = factory
}

func GetProvider(name string, config Config) (Provider, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }

    return factory(config)
}
```

### 8.3 Provider Implementations

**AWS Provider (`pkg/provider/aws`):**
- EKS for Kubernetes
- VPC with public/private subnets across 3 AZs
- EFS for shared storage
- Application Load Balancer for ingress
- IAM roles for service accounts (IRSA)
- EBS CSI driver for persistent volumes

**GCP Provider (`pkg/provider/gcp`):**
- GKE for Kubernetes (Autopilot or Standard mode)
- VPC with auto-mode subnets
- Filestore for shared storage
- Google Cloud Load Balancer for ingress
- Workload Identity for service accounts
- Persistent Disk CSI driver

**Azure Provider (`pkg/provider/azure`):**
- AKS for Kubernetes
- VNet with subnets
- Azure Files for shared storage
- Azure Load Balancer for ingress
- Managed Identity for service accounts
- Azure Disk CSI driver

**Local Provider (`pkg/provider/local`):**
- K3s for Kubernetes
- Local storage via local-path-provisioner
- Traefik for ingress (bundled with K3s)
- No cloud resources

### 8.4 Provider Parity

**Consistency Goals:**
- Same Kubernetes version across all providers
- Same foundational software versions
- Same API (nebari-config.yaml works across providers)
- Same outputs (kubeconfig, URLs, credentials)

**Provider-Specific Differences:**
- Storage class names (ebs vs pd-standard vs azure-disk)
- Load balancer annotations
- IAM/service account mechanisms
- Network policies (EKS uses Calico, GKE uses Dataplane V2)

---
