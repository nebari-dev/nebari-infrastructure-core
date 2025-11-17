# Configuration Reference

This document provides a complete reference for all configuration options in NIC, based on the actual struct definitions in `pkg/config/config.go`.

## Table of Contents

1. [Global Configuration](#global-configuration)
2. [AWS Provider Configuration](#aws-provider-configuration)
3. [GCP Provider Configuration](#gcp-provider-configuration)
4. [Azure Provider Configuration](#azure-provider-configuration)
5. [Local Provider Configuration](#local-provider-configuration)
6. [DNS Provider Configuration](#dns-provider-configuration)
7. [Complete Examples](#complete-examples)

---

## Global Configuration

These fields apply to all providers and are defined in `NebariConfig` (pkg/config/config.go:4-22).

```yaml
# REQUIRED: Unique name for your Nebari deployment
# Used for resource naming and tagging
project_name: my-nebari

# REQUIRED: Cloud provider to use
# Valid values: aws, gcp, azure, local
provider: aws

# OPTIONAL: Domain name for your Nebari deployment
# Required if you want to enable TLS/HTTPS access
# Example: nebari.example.com
domain: nebari.example.com

# OPTIONAL: DNS provider for managing DNS records
# Valid values: cloudflare (more providers in future)
# Requires corresponding dns configuration section below
dns_provider: cloudflare

# OPTIONAL: DNS provider-specific configuration
# Structure depends on dns_provider selected
# See "DNS Provider Configuration" section for details
dns:
  zone_name: example.com
  email: admin@example.com
```

**Field Descriptions:**

- **project_name** (string, required): Unique identifier for your Nebari deployment. Used in resource naming and tagging across all cloud resources.
- **provider** (string, required): Cloud provider to deploy infrastructure on. Must be one of: `aws`, `gcp`, `azure`, `local`.
- **domain** (string, optional): Fully qualified domain name for accessing Nebari services. Required for TLS/Let's Encrypt integration.
- **dns_provider** (string, optional): DNS provider to manage DNS records. Currently supports `cloudflare`. Requires `dns` section.
- **dns** (map, optional): DNS provider-specific configuration. Structure varies by provider. See DNS Provider Configuration section.

---

## AWS Provider Configuration

AWS-specific configuration defined in `AWSConfig` (pkg/config/config.go:24-39).

```yaml
provider: aws

amazon_web_services:
  # REQUIRED: AWS region to deploy infrastructure
  # Example: us-west-2, us-east-1, eu-west-1
  region: us-west-2

  # REQUIRED: Kubernetes version for EKS cluster
  # Example: "1.28", "1.29", "1.30"
  # Must be a string (quoted) to preserve minor version
  kubernetes_version: "1.28"

  # OPTIONAL: List of availability zones to use
  # If not specified, AWS will automatically select zones in the region
  # Must be valid AZs within the specified region
  availability_zones:
    - us-west-2a
    - us-west-2b
    - us-west-2c

  # OPTIONAL: CIDR block for VPC creation
  # Default: AWS default VPC CIDR
  # Must be a valid RFC 1918 private network range
  vpc_cidr_block: "10.10.0.0/16"

  # OPTIONAL: EKS API endpoint access configuration
  # Valid values: "public", "private", "public-and-private"
  # Default: "public"
  # - public: API accessible from internet
  # - private: API only accessible from within VPC
  # - public-and-private: API accessible from both
  eks_endpoint_access: "public"

  # OPTIONAL: CIDR blocks allowed to access public EKS endpoint
  # Only applies when eks_endpoint_access is "public" or "public-and-private"
  # Default: ["0.0.0.0/0"] (allow all)
  # Restrict to your organization's IP ranges for security
  eks_public_access_cidrs:
    - "203.0.113.0/24"
    - "198.51.100.0/24"

  # OPTIONAL: ARN of KMS key for EKS secrets encryption
  # If specified, Kubernetes secrets will be encrypted with this key
  # Example: arn:aws:kms:us-west-2:123456789012:key/12345678-1234-1234-1234-123456789012
  eks_kms_arn: ""

  # OPTIONAL: Use existing subnet IDs instead of creating new VPC
  # Provide list of subnet IDs that span multiple AZs
  # If specified, VPC creation is skipped
  existing_subnet_ids:
    - subnet-12345678
    - subnet-87654321

  # OPTIONAL: Use existing security group ID
  # If specified, this security group will be used for EKS cluster
  existing_security_group_id: sg-12345678

  # OPTIONAL: IAM permissions boundary ARN
  # Applied to all IAM roles created by NIC
  # Required in enterprise environments with mandatory permission boundaries
  # Example: arn:aws:iam::123456789012:policy/PermissionsBoundary
  permissions_boundary: ""

  # OPTIONAL: AWS resource tags
  # Applied to all AWS resources created by NIC
  # Useful for cost allocation, compliance, and organization
  tags:
    Environment: production
    Project: nebari
    ManagedBy: nic
    CostCenter: engineering

  # REQUIRED: Node groups (worker node pools) configuration
  # At least one node group is required for a functional cluster
  # Map of node group name to configuration
  node_groups:
    # General purpose node group (typically required)
    general:
      # REQUIRED: EC2 instance type
      # Example: m5.2xlarge, m6i.4xlarge, c5.xlarge
      # Choose based on workload requirements (CPU, memory, network)
      instance: m5.2xlarge

      # OPTIONAL: Minimum number of nodes in this group
      # Default: 0
      # Autoscaler will not scale below this number
      min_nodes: 1

      # OPTIONAL: Maximum number of nodes in this group
      # Default: 1
      # Autoscaler will not scale above this number
      max_nodes: 5

      # OPTIONAL: Kubernetes taints for this node group
      # Prevents pods from being scheduled unless they have matching tolerations
      # Useful for dedicated workloads (GPU, high-memory, etc.)
      taints:
        - key: workload
          value: general
          effect: NoSchedule  # NoSchedule, PreferNoSchedule, or NoExecute

      # OPTIONAL: Enable GPU support for this node group
      # Default: false
      # Set to true for GPU instance types (p3, p4, g4, g5)
      gpu: false

      # OPTIONAL: Deploy nodes in a single subnet only
      # Default: false
      # Set to true if node group should not span multiple AZs
      single_subnet: false

      # OPTIONAL: IAM permissions boundary for this node group's IAM role
      # Overrides the global permissions_boundary for this specific node group
      permissions_boundary: ""

      # OPTIONAL: Use EC2 Spot instances for cost savings
      # Default: false
      # Spot instances are cheaper but can be interrupted
      # Not recommended for critical workloads
      spot: false

    # User workload node group example
    user:
      instance: m5.xlarge
      min_nodes: 0
      max_nodes: 10
      taints: []

    # GPU node group example
    gpu:
      instance: g5.2xlarge
      min_nodes: 0
      max_nodes: 5
      gpu: true
      spot: false
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule

    # Spot instance node group example
    spot-workers:
      instance: m5.4xlarge
      min_nodes: 0
      max_nodes: 20
      spot: true
      taints:
        - key: workload
          value: spot
          effect: NoSchedule
```

**AWS Environment Variables (Secrets):**

NIC requires AWS credentials via environment variables or IAM roles:

```bash
# Option 1: Long-term credentials (development only)
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

# Option 2: Temporary credentials (recommended)
AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
AWS_SESSION_TOKEN=FwoGZXIvYXdzEBYaDJH...

# Option 3: AWS Profile (recommended for local development)
AWS_PROFILE=nebari-admin

# Option 4: IAM Role (recommended for CI/CD)
# No environment variables needed - uses instance/pod IAM role
```

---

## GCP Provider Configuration

GCP-specific configuration defined in `GCPConfig` (pkg/config/config.go:41-57).

```yaml
provider: gcp

google_cloud_platform:
  # REQUIRED: GCP project ID where resources will be created
  # Example: my-project-123456
  # Must be an existing GCP project with billing enabled
  project: my-gcp-project-id

  # REQUIRED: GCP region to deploy infrastructure
  # Example: us-central1, us-east1, europe-west1
  region: us-central1

  # REQUIRED: Kubernetes version for GKE cluster
  # Example: "1.28", "1.29", "1.30"
  # Must be a string (quoted) to preserve minor version
  kubernetes_version: "1.28"

  # OPTIONAL: List of availability zones (GCP zones) to use
  # If not specified, GCP will automatically select zones in the region
  # Must be valid zones within the specified region
  # Example: us-central1-a, us-central1-b, us-central1-c
  availability_zones:
    - us-central1-a
    - us-central1-b
    - us-central1-c

  # OPTIONAL: GKE release channel for automatic version management
  # Valid values: "RAPID", "REGULAR", "STABLE", "UNSPECIFIED"
  # Default: "REGULAR"
  # - RAPID: Bleeding edge, frequent updates
  # - REGULAR: Balanced updates (recommended)
  # - STABLE: Conservative updates, well-tested
  # - UNSPECIFIED: Manual version management
  release_channel: "REGULAR"

  # OPTIONAL: GKE networking mode
  # Valid values: "ROUTE", "VPC_NATIVE"
  # Default: "VPC_NATIVE"
  # - ROUTE: Routes-based networking (legacy)
  # - VPC_NATIVE: IP aliasing, recommended for new clusters
  networking_mode: "VPC_NATIVE"

  # OPTIONAL: VPC network to use for cluster
  # Default: "default"
  # Can specify existing VPC network name
  network: "default"

  # OPTIONAL: VPC subnetwork to use for cluster
  # Required if using custom VPC network
  # Example: projects/my-project/regions/us-central1/subnetworks/my-subnet
  subnetwork: ""

  # OPTIONAL: IP allocation policy for VPC-native clusters
  # Defines secondary IP ranges for pods and services
  # Only applies when networking_mode is "VPC_NATIVE"
  ip_allocation_policy:
    cluster_secondary_range_name: gke-pods
    services_secondary_range_name: gke-services
    cluster_ipv4_cidr_block: "10.4.0.0/14"
    services_ipv4_cidr_block: "10.0.32.0/20"

  # OPTIONAL: Master authorized networks configuration
  # Restricts access to GKE control plane
  # Map of CIDR name to CIDR block
  master_authorized_networks_config:
    office-network: "203.0.113.0/24"
    vpn-network: "198.51.100.0/24"

  # OPTIONAL: Private cluster configuration
  # Enables GKE private cluster mode
  private_cluster_config:
    enable_private_nodes: true
    enable_private_endpoint: false
    master_ipv4_cidr_block: "172.16.0.0/28"

  # OPTIONAL: GCP network tags (labels)
  # Applied to all GCE instances (nodes)
  # Used for firewall rules and organization
  # Note: GCP uses tags as strings, not key-value pairs
  tags:
    - production
    - nebari
    - data-science

  # REQUIRED: Node groups (node pools) configuration
  # At least one node group is required for a functional cluster
  # Map of node group name to configuration
  node_groups:
    # General purpose node pool
    general:
      # REQUIRED: GCE machine type
      # Example: n1-standard-8, n2-standard-16, e2-standard-8
      # Choose based on workload requirements (CPU, memory)
      instance: e2-standard-8

      # OPTIONAL: Minimum number of nodes per zone
      # Default: 0
      # Autoscaler will not scale below this number (per zone)
      min_nodes: 1

      # OPTIONAL: Maximum number of nodes per zone
      # Default: 1
      # Autoscaler will not scale above this number (per zone)
      max_nodes: 5

      # OPTIONAL: Kubernetes taints for this node pool
      # Prevents pods from being scheduled unless they have matching tolerations
      taints:
        - key: workload
          value: general
          effect: NoSchedule  # NoSchedule, PreferNoSchedule, or NoExecute

      # OPTIONAL: Use preemptible VMs for cost savings
      # Default: false
      # Preemptible VMs are cheaper but can be terminated at any time
      # Not recommended for critical workloads
      preemptible: false

      # OPTIONAL: Kubernetes labels for this node pool
      # Applied to all nodes in this pool
      # Used for node affinity and pod scheduling
      labels:
        workload: general
        environment: production

      # OPTIONAL: GPU configuration for this node pool
      # Required for GPU workloads (TensorFlow, PyTorch, etc.)
      # Must use GPU-enabled machine types (n1-standard-* with GPUs)
      guest_accelerators:
        - name: nvidia-tesla-t4  # GPU type
          count: 1               # Number of GPUs per node

    # User workload node pool example
    user:
      instance: e2-standard-4
      min_nodes: 0
      max_nodes: 10
      labels:
        workload: user

    # GPU node pool example
    gpu:
      instance: n1-standard-8
      min_nodes: 0
      max_nodes: 5
      labels:
        workload: gpu
        nvidia.com/gpu: "true"
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule
      guest_accelerators:
        - name: nvidia-tesla-v100
          count: 2

    # Preemptible node pool example
    preemptible-workers:
      instance: n2-standard-16
      min_nodes: 0
      max_nodes: 20
      preemptible: true
      labels:
        workload: batch
        preemptible: "true"
      taints:
        - key: preemptible
          value: "true"
          effect: NoSchedule
```

**GCP Environment Variables (Secrets):**

NIC requires GCP credentials via environment variables or service account:

```bash
# Option 1: Service account key file (development only)
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account-key.json

# Option 2: Service account key JSON (CI/CD)
GOOGLE_CREDENTIALS='{"type":"service_account","project_id":"my-project",...}'

# Option 3: Workload Identity (recommended for GKE)
# No environment variables needed - uses pod service account

# GCP Project ID (optional, can be in config)
GOOGLE_PROJECT=my-gcp-project-id
```

---

## Azure Provider Configuration

Azure-specific configuration defined in `AzureConfig` (pkg/config/config.go:59-76).

```yaml
provider: azure

azure:
  # REQUIRED: Azure region to deploy infrastructure
  # Example: eastus, westus2, westeurope
  region: eastus

  # OPTIONAL: Kubernetes version for AKS cluster
  # Example: "1.28", "1.29", "1.30"
  # Must be a string (quoted) to preserve minor version
  # If not specified, uses latest available version in region
  kubernetes_version: "1.28"

  # REQUIRED: Storage account name postfix
  # Used to create unique storage account names
  # Must be lowercase alphanumeric, 3-24 characters
  # Final name: <project_name><storage_account_postfix>
  storage_account_postfix: "nebari"

  # OPTIONAL: Resource group name for all resources
  # If not specified, NIC will create: <project_name>-<region>
  # Must be unique within your Azure subscription
  resource_group_name: "nebari-resources"

  # OPTIONAL: Node resource group name
  # Separate resource group for AKS node resources (VMs, disks, NICs)
  # If not specified, Azure creates: MC_<resource_group>_<cluster_name>_<region>
  node_resource_group_name: "nebari-node-resources"

  # OPTIONAL: VNet subnet ID for AKS cluster
  # Use existing subnet instead of creating new VNet
  # Example: /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/virtualNetworks/{vnet}/subnets/{subnet}
  vnet_subnet_id: ""

  # OPTIONAL: Enable private cluster mode
  # Default: false
  # If true, AKS API server is not publicly accessible
  # Requires VPN or ExpressRoute for management access
  private_cluster_enabled: false

  # OPTIONAL: Maximum pods per node
  # Default: 30 (Azure default)
  # Maximum: 250
  # Affects IP address requirements in subnet
  max_pods: 30

  # OPTIONAL: Enable Azure Workload Identity
  # Default: false
  # Allows pods to authenticate to Azure services using managed identities
  # Recommended for secure access to Azure resources
  workload_identity_enabled: true

  # OPTIONAL: Enable Azure Policy for Kubernetes
  # Default: false
  # Enables policy-based governance for AKS cluster
  # Useful for compliance and security enforcement
  azure_policy_enabled: false

  # OPTIONAL: Azure resource tags
  # Applied to all Azure resources created by NIC
  # Useful for cost allocation, compliance, and organization
  tags:
    Environment: production
    Project: nebari
    ManagedBy: nic
    CostCenter: engineering

  # OPTIONAL: Network profile configuration
  # Defines networking settings for AKS cluster
  network_profile:
    network_plugin: azure       # azure (Azure CNI) or kubenet
    network_policy: azure       # azure, calico, or none
    service_cidr: "10.0.0.0/16"
    dns_service_ip: "10.0.0.10"
    docker_bridge_cidr: "172.17.0.1/16"

  # OPTIONAL: Authorized IP ranges for API server access
  # Only these IPs can access the AKS API server
  # Empty list = allow all (not recommended for production)
  authorized_ip_ranges:
    - "203.0.113.0/24"
    - "198.51.100.0/24"

  # REQUIRED: Node groups (node pools) configuration
  # At least one node group is required for a functional cluster
  # Map of node group name to configuration
  node_groups:
    # General purpose node pool (system pool)
    general:
      # REQUIRED: Azure VM size
      # Example: Standard_D8_v3, Standard_D16s_v3, Standard_E8s_v3
      # Choose based on workload requirements (CPU, memory)
      instance: Standard_D8_v3

      # OPTIONAL: Minimum number of nodes in this pool
      # Default: 0
      # Autoscaler will not scale below this number
      # First node pool (system pool) should have min_nodes >= 1
      min_nodes: 1

      # OPTIONAL: Maximum number of nodes in this pool
      # Default: 1
      # Autoscaler will not scale above this number
      max_nodes: 5

      # OPTIONAL: Kubernetes taints for this node pool
      # Prevents pods from being scheduled unless they have matching tolerations
      taints:
        - key: CriticalAddonsOnly
          value: "true"
          effect: NoSchedule  # NoSchedule, PreferNoSchedule, or NoExecute

    # User workload node pool example
    user:
      instance: Standard_D4_v3
      min_nodes: 0
      max_nodes: 10

    # High-memory node pool example
    highmem:
      instance: Standard_E16s_v3
      min_nodes: 0
      max_nodes: 5
      taints:
        - key: workload
          value: highmem
          effect: NoSchedule

    # GPU node pool example (requires GPU-enabled VM sizes)
    gpu:
      instance: Standard_NC6s_v3
      min_nodes: 0
      max_nodes: 3
      taints:
        - key: sku
          value: gpu
          effect: NoSchedule
```

**Azure Environment Variables (Secrets):**

NIC requires Azure credentials via environment variables or managed identity:

```bash
# Option 1: Service Principal (recommended for automation)
AZURE_CLIENT_ID=12345678-1234-1234-1234-123456789012
AZURE_CLIENT_SECRET=your-client-secret
AZURE_TENANT_ID=87654321-4321-4321-4321-210987654321
AZURE_SUBSCRIPTION_ID=11111111-1111-1111-1111-111111111111

# Option 2: Managed Identity (recommended for Azure VMs/AKS)
# No environment variables needed - uses VM/pod managed identity

# Option 3: Azure CLI authentication (development only)
# Run: az login
# NIC will use credentials from Azure CLI
```

---

## Local Provider Configuration

Local K3s provider configuration defined in `LocalConfig` (pkg/config/config.go:78-83).

```yaml
provider: local

local:
  # OPTIONAL: Kubernetes context to use from kubeconfig
  # Default: current context from ~/.kube/config
  # Use to specify which cluster to deploy to when you have multiple contexts
  kube_context: "k3d-nebari-local"

  # OPTIONAL: Node selectors for workload placement
  # Map of workload type to Kubernetes node selector labels
  # Used to target specific nodes in the local cluster
  # Useful when running multi-node K3s/K3d/Kind clusters
  node_selectors:
    # General workloads node selector
    general:
      kubernetes.io/os: linux
      node-role.kubernetes.io/worker: "true"

    # User workloads node selector
    user:
      kubernetes.io/os: linux
      workload: user

    # Worker/batch workloads node selector
    worker:
      kubernetes.io/os: linux
      workload: batch

    # GPU workloads node selector (if you have GPU nodes locally)
    gpu:
      kubernetes.io/os: linux
      nvidia.com/gpu: "true"
```

**Local Provider Notes:**

- **Purpose**: Deploy Nebari to existing local Kubernetes cluster (K3s, K3d, Kind, Minikube, Docker Desktop)
- **No cloud credentials required**: Uses local kubeconfig for authentication
- **No infrastructure provisioning**: Assumes cluster already exists
- **Node selectors only**: No node group creation, only workload placement control
- **Development/testing use case**: Not recommended for production deployments

**Local Provider Environment Variables:**

```bash
# OPTIONAL: Custom kubeconfig location
KUBECONFIG=/path/to/custom/kubeconfig

# If not set, uses default: ~/.kube/config
```

---

## DNS Provider Configuration

DNS provider configuration for managing DNS records and Let's Encrypt integration.

### Cloudflare DNS Provider

Cloudflare DNS provider defined in `cloudflare.Config` (pkg/dnsprovider/cloudflare/config.go:5-9).

```yaml
# REQUIRED: Specify Cloudflare as DNS provider
dns_provider: cloudflare

dns:
  # REQUIRED: Cloudflare zone name (your domain)
  # This is the domain you manage in Cloudflare
  # Example: example.com, mycompany.com
  # NIC will create DNS records under this zone
  zone_name: example.com

  # OPTIONAL: Email address for Let's Encrypt notifications
  # Used by cert-manager for certificate expiration notices
  # Recommended: use a monitored email address
  email: admin@example.com
```

**Cloudflare Environment Variables (Secrets):**

Cloudflare API credentials must be provided via environment variables:

```bash
# REQUIRED: Cloudflare API Token (recommended)
# Create at: https://dash.cloudflare.com/profile/api-tokens
# Required permissions: Zone.DNS (Edit)
CLOUDFLARE_API_TOKEN=your-cloudflare-api-token

# ALTERNATIVE: Cloudflare Global API Key (legacy, not recommended)
# Less secure than API tokens
CLOUDFLARE_API_KEY=your-cloudflare-api-key
CLOUDFLARE_EMAIL=your-cloudflare-email
```

**How to Create Cloudflare API Token:**

1. Go to https://dash.cloudflare.com/profile/api-tokens
2. Click "Create Token"
3. Use "Edit zone DNS" template or create custom token
4. Permissions required:
   - Zone / DNS / Edit
   - Zone / Zone / Read
5. Zone Resources: Include / Specific zone / your-domain.com
6. Copy token and add to `.env` file: `CLOUDFLARE_API_TOKEN=...`

**DNS Provider Integration:**

When `dns_provider` is configured, NIC will:
- Create DNS records pointing to your Nebari ingress
- Configure cert-manager with DNS-01 ACME challenge provider
- Enable automatic TLS certificate provisioning via Let's Encrypt
- Provide cert-manager configuration via `GetCertManagerConfig()` method

---

## Complete Examples

### Minimal AWS Configuration

```yaml
# Minimal production-ready AWS deployment
project_name: nebari-prod
provider: aws
domain: nebari.example.com

amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.28"

  node_groups:
    general:
      instance: m5.2xlarge
      min_nodes: 3
      max_nodes: 10
```

### Full-Featured AWS Configuration

```yaml
# Production AWS deployment with all common options
project_name: nebari-production
provider: aws
domain: nebari.company.com

amazon_web_services:
  region: us-east-1
  kubernetes_version: "1.29"
  availability_zones:
    - us-east-1a
    - us-east-1b
    - us-east-1c
  vpc_cidr_block: "10.100.0.0/16"
  eks_endpoint_access: "public-and-private"
  eks_public_access_cidrs:
    - "203.0.113.0/24"  # Office network
  permissions_boundary: "arn:aws:iam::123456789012:policy/DepartmentBoundary"

  tags:
    Environment: production
    Project: nebari
    Team: data-science
    CostCenter: engineering
    ManagedBy: nic

  node_groups:
    general:
      instance: m6i.4xlarge
      min_nodes: 3
      max_nodes: 10
      taints:
        - key: CriticalAddonsOnly
          value: "true"
          effect: NoSchedule

    user:
      instance: m6i.2xlarge
      min_nodes: 2
      max_nodes: 50

    worker:
      instance: c6i.8xlarge
      min_nodes: 0
      max_nodes: 20
      taints:
        - key: workload
          value: batch
          effect: NoSchedule

    gpu:
      instance: g5.2xlarge
      min_nodes: 0
      max_nodes: 10
      gpu: true
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule

    spot:
      instance: m6i.8xlarge
      min_nodes: 0
      max_nodes: 30
      spot: true
      taints:
        - key: spot
          value: "true"
          effect: NoSchedule

dns_provider: cloudflare
dns:
  zone_name: company.com
  email: platform-team@company.com
```

### Minimal GCP Configuration

```yaml
# Minimal production-ready GCP deployment
project_name: nebari-prod
provider: gcp
domain: nebari.example.com

google_cloud_platform:
  project: my-gcp-project
  region: us-central1
  kubernetes_version: "1.28"

  node_groups:
    general:
      instance: e2-standard-8
      min_nodes: 3
      max_nodes: 10
```

### Full-Featured GCP Configuration

```yaml
# Production GCP deployment with all common options
project_name: nebari-production
provider: gcp
domain: nebari.company.com

google_cloud_platform:
  project: company-nebari-prod
  region: us-central1
  kubernetes_version: "1.29"
  availability_zones:
    - us-central1-a
    - us-central1-b
    - us-central1-c
  release_channel: "REGULAR"
  networking_mode: "VPC_NATIVE"
  network: "nebari-network"

  ip_allocation_policy:
    cluster_secondary_range_name: gke-pods
    services_secondary_range_name: gke-services
    cluster_ipv4_cidr_block: "10.4.0.0/14"
    services_ipv4_cidr_block: "10.0.32.0/20"

  master_authorized_networks_config:
    office: "203.0.113.0/24"
    vpn: "198.51.100.0/24"

  private_cluster_config:
    enable_private_nodes: true
    enable_private_endpoint: false
    master_ipv4_cidr_block: "172.16.0.0/28"

  tags:
    - production
    - nebari
    - data-science

  node_groups:
    general:
      instance: n2-standard-8
      min_nodes: 3
      max_nodes: 10
      labels:
        workload: system
      taints:
        - key: CriticalAddonsOnly
          value: "true"
          effect: NoSchedule

    user:
      instance: n2-standard-4
      min_nodes: 2
      max_nodes: 50
      labels:
        workload: user

    worker:
      instance: c2-standard-16
      min_nodes: 0
      max_nodes: 20
      labels:
        workload: batch
      taints:
        - key: workload
          value: batch
          effect: NoSchedule

    gpu:
      instance: n1-standard-8
      min_nodes: 0
      max_nodes: 10
      labels:
        workload: gpu
      guest_accelerators:
        - name: nvidia-tesla-t4
          count: 1
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule

    preemptible:
      instance: n2-standard-16
      min_nodes: 0
      max_nodes: 30
      preemptible: true
      labels:
        workload: preemptible
      taints:
        - key: preemptible
          value: "true"
          effect: NoSchedule

dns_provider: cloudflare
dns:
  zone_name: company.com
  email: platform-team@company.com
```

### Minimal Azure Configuration

```yaml
# Minimal production-ready Azure deployment
project_name: nebari-prod
provider: azure
domain: nebari.example.com

azure:
  region: eastus
  kubernetes_version: "1.28"
  storage_account_postfix: "nbri"

  node_groups:
    general:
      instance: Standard_D8_v3
      min_nodes: 3
      max_nodes: 10
```

### Full-Featured Azure Configuration

```yaml
# Production Azure deployment with all common options
project_name: nebari-production
provider: azure
domain: nebari.company.com

azure:
  region: eastus
  kubernetes_version: "1.29"
  storage_account_postfix: "nbriprod"
  resource_group_name: "nebari-prod-rg"
  node_resource_group_name: "nebari-prod-nodes-rg"
  private_cluster_enabled: false
  max_pods: 50
  workload_identity_enabled: true
  azure_policy_enabled: true

  authorized_ip_ranges:
    - "203.0.113.0/24"  # Office network
    - "198.51.100.0/24"  # VPN network

  network_profile:
    network_plugin: azure
    network_policy: azure
    service_cidr: "10.0.0.0/16"
    dns_service_ip: "10.0.0.10"
    docker_bridge_cidr: "172.17.0.1/16"

  tags:
    Environment: production
    Project: nebari
    Team: data-science
    CostCenter: engineering
    ManagedBy: nic

  node_groups:
    general:
      instance: Standard_D8s_v3
      min_nodes: 3
      max_nodes: 10
      taints:
        - key: CriticalAddonsOnly
          value: "true"
          effect: NoSchedule

    user:
      instance: Standard_D4s_v3
      min_nodes: 2
      max_nodes: 50

    worker:
      instance: Standard_F16s_v2
      min_nodes: 0
      max_nodes: 20
      taints:
        - key: workload
          value: batch
          effect: NoSchedule

    highmem:
      instance: Standard_E16s_v3
      min_nodes: 0
      max_nodes: 10
      taints:
        - key: workload
          value: highmem
          effect: NoSchedule

    gpu:
      instance: Standard_NC6s_v3
      min_nodes: 0
      max_nodes: 5
      taints:
        - key: sku
          value: gpu
          effect: NoSchedule

dns_provider: cloudflare
dns:
  zone_name: company.com
  email: platform-team@company.com
```

### Local Development Configuration

```yaml
# Local K3d/Kind cluster for development
project_name: nebari-dev
provider: local
domain: nebari.local

local:
  kube_context: "k3d-nebari-local"
  node_selectors:
    general:
      kubernetes.io/os: linux
    user:
      kubernetes.io/os: linux
    worker:
      kubernetes.io/os: linux
```

### Multi-Environment Setup (Separate Files)

**base-production.yaml** (production baseline):
```yaml
project_name: nebari-prod
provider: aws
domain: nebari.company.com

amazon_web_services:
  region: us-east-1
  kubernetes_version: "1.29"
  vpc_cidr_block: "10.100.0.0/16"

  tags:
    Environment: production
    ManagedBy: nic

  node_groups:
    general:
      instance: m6i.4xlarge
      min_nodes: 3
      max_nodes: 10
```

**staging.yaml** (smaller staging environment):
```yaml
project_name: nebari-staging
provider: aws
domain: staging.nebari.company.com

amazon_web_services:
  region: us-west-2
  kubernetes_version: "1.29"
  vpc_cidr_block: "10.200.0.0/16"

  tags:
    Environment: staging
    ManagedBy: nic

  node_groups:
    general:
      instance: m5.xlarge
      min_nodes: 1
      max_nodes: 3
```

**development.yaml** (minimal dev environment):
```yaml
project_name: nebari-dev
provider: local
domain: nebari.local

local:
  kube_context: "k3d-nebari-dev"
```

---

## Configuration Validation

Use `nic validate` to check your configuration before deployment:

```bash
# Validate configuration file
nic validate -f config.yaml

# Example output:
# ✅ Configuration valid
#
# Summary:
#   Provider: AWS (us-west-2)
#   Project: nebari-prod
#   Domain: nebari.example.com
#   DNS Provider: cloudflare
#   Node Groups: 4 (general, user, worker, gpu)
```

---

## Environment Variables Reference

### AWS Provider
```bash
AWS_ACCESS_KEY_ID=<access-key>
AWS_SECRET_ACCESS_KEY=<secret-key>
AWS_SESSION_TOKEN=<session-token>          # Optional, for temporary credentials
AWS_PROFILE=<profile-name>                 # Optional, use named profile
AWS_REGION=<region>                        # Optional, overrides config
```

### GCP Provider
```bash
GOOGLE_APPLICATION_CREDENTIALS=<path-to-key.json>
GOOGLE_CREDENTIALS=<json-key-content>      # Alternative to file path
GOOGLE_PROJECT=<project-id>                # Optional, overrides config
```

### Azure Provider
```bash
AZURE_CLIENT_ID=<client-id>
AZURE_CLIENT_SECRET=<client-secret>
AZURE_TENANT_ID=<tenant-id>
AZURE_SUBSCRIPTION_ID=<subscription-id>
```

### Cloudflare DNS Provider
```bash
CLOUDFLARE_API_TOKEN=<api-token>           # Recommended
# OR (legacy)
CLOUDFLARE_API_KEY=<api-key>
CLOUDFLARE_EMAIL=<email>
```

### Local Provider
```bash
KUBECONFIG=<path-to-kubeconfig>            # Optional, default: ~/.kube/config
```

---

## Best Practices

### Security
1. **Never commit secrets to git**: Use `.env` file (gitignored) or CI/CD secret management
2. **Use restrictive CIDR blocks**: Limit `eks_public_access_cidrs` / `authorized_ip_ranges` to known networks
3. **Enable private clusters**: Set `private_cluster_enabled: true` for production when possible
4. **Use permissions boundaries**: Apply `permissions_boundary` in enterprise environments
5. **Rotate credentials regularly**: Update API tokens and service account keys periodically

### High Availability
1. **Multi-AZ deployment**: Specify multiple `availability_zones` (minimum 3 for production)
2. **Adequate min_nodes**: Set `min_nodes >= 3` for general node group in production
3. **Node group redundancy**: Use multiple node groups for different workload types

### Cost Optimization
1. **Use spot/preemptible for batch workloads**: Save 60-90% on compute costs
2. **Right-size instances**: Start small, scale up based on actual usage
3. **Set appropriate max_nodes**: Prevent runaway scaling costs
4. **Use tags for cost allocation**: Track spending by team, project, environment

### Scalability
1. **Autoscaling ranges**: Set `min_nodes` for baseline, `max_nodes` for peak capacity
2. **Use taints for specialized workloads**: Ensure GPU/high-memory nodes only used when needed
3. **Monitor node utilization**: Adjust instance types and scaling limits based on metrics

---

**Last Updated**: 2025-01-14
**NIC Version**: v0.1.0
**Source**: Generated from pkg/config/config.go and pkg/dnsprovider/*/config.go
