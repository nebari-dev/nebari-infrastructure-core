# OpenTofu Module Architecture

**Note**: This document describes the alternative OpenTofu-based design approach. See [../README.md](../README.md) for comparison with the native SDK design.

## Overview

This alternative design uses **OpenTofu/Terraform modules** for infrastructure provisioning instead of direct cloud SDK calls. The Go CLI orchestrates OpenTofu execution via the terraform-exec library, while actual infrastructure provisioning is delegated to HCL-based modules.

## Module Structure

### Repository Layout

```
nebari-infrastructure-core/
├── cmd/nic/               # Go CLI (same as main design)
├── pkg/
│   ├── tofu/             # terraform-exec wrapper (OpenTofu-specific)
│   ├── config/           # Parse config.yaml (same as main design)
│   ├── kubernetes/       # K8s health checks (same as main design)
│   └── telemetry/        # OpenTelemetry (same as main design)
├── terraform/            # OpenTofu/Terraform modules (OpenTofu-specific)
│   ├── main.tf           # Root module
│   ├── variables.tf      # Input variables from config.yaml
│   ├── outputs.tf        # Outputs (kubeconfig, URLs, etc.)
│   ├── backend.tf.tmpl   # State backend configuration template
│   ├── providers.tf      # Provider configurations
│   └── modules/
│       ├── aws/          # AWS-specific modules
│       │   ├── vpc/
│       │   ├── eks/
│       │   └── efs/
│       ├── gcp/          # GCP-specific modules
│       │   ├── vpc/
│       │   ├── gke/
│       │   └── filestore/
│       ├── azure/        # Azure-specific modules
│       │   ├── vnet/
│       │   ├── aks/
│       │   └── azure-files/
│       ├── local/        # Local K3s module
│       │   └── k3s/
│       ├── kubernetes/   # K8s bootstrap (namespaces, RBAC, etc.)
│       ├── argocd/       # ArgoCD Helm deployment
│       └── foundational-apps/  # ArgoCD Applications
└── go.mod
```

### How It Works

1. **User runs**: `nic deploy -f config.yaml`
2. **Go CLI (`cmd/nic`)**:
   - Parses `config.yaml` into Go structs
   - Converts config to Terraform variables JSON
   - Invokes terraform-exec to run `tofu apply`
3. **OpenTofu**:
   - Reads variables JSON
   - Executes `terraform/main.tf` root module
   - Provisions cloud infrastructure via provider plugins
   - Updates state file in configured backend
4. **Go CLI resumes**:
   - Waits for Kubernetes cluster readiness
   - Waits for ArgoCD and foundational software
   - Reports deployment success

## Root Module Design

The root module (`terraform/main.tf`) contains conditional logic to provision the correct cloud resources based on the `provider` variable:

```hcl
terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "~> 3.0"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "~> 2.12"
    }
  }
}

# Provider selection based on var.provider
locals {
  is_aws   = var.provider == "aws"
  is_gcp   = var.provider == "gcp"
  is_azure = var.provider == "azure"
  is_local = var.provider == "local"
}

# AWS Infrastructure (only created if provider=aws)
module "aws_vpc" {
  count  = local.is_aws ? 1 : 0
  source = "./modules/aws/vpc"

  name               = var.cluster_name
  cidr               = var.aws_vpc_cidr
  availability_zones = var.aws_availability_zones
  tags               = var.tags
}

module "aws_eks" {
  count  = local.is_aws ? 1 : 0
  source = "./modules/aws/eks"

  cluster_name       = var.cluster_name
  kubernetes_version = var.kubernetes_version
  vpc_id             = module.aws_vpc[0].vpc_id
  subnet_ids         = module.aws_vpc[0].private_subnet_ids
  node_pools         = var.node_pools
  tags               = var.tags
}

# GCP Infrastructure (only created if provider=gcp)
module "gcp_vpc" {
  count  = local.is_gcp ? 1 : 0
  source = "./modules/gcp/vpc"

  name    = var.cluster_name
  region  = var.region
  project = var.gcp_project_id
}

module "gcp_gke" {
  count  = local.is_gcp ? 1 : 0
  source = "./modules/gcp/gke"

  cluster_name       = var.cluster_name
  kubernetes_version = var.kubernetes_version
  region             = var.region
  project            = var.gcp_project_id
  network            = module.gcp_vpc[0].network_name
  subnetwork         = module.gcp_vpc[0].subnetwork_name
  node_pools         = var.node_pools
}

# Azure, Local, Kubernetes bootstrap, ArgoCD modules...
# (Similar pattern for other providers)
```

### Key Differences from Native SDK Design

| Aspect | Native SDK | OpenTofu |
|--------|-----------|----------|
| **Infrastructure Code** | Go code with cloud SDK calls | HCL modules in `terraform/` directory |
| **State** | Stateless (queries cloud APIs) | Terraform state file in S3/GCS/Azure Blob |
| **Execution** | Direct API calls from Go | Go → terraform-exec → OpenTofu → cloud APIs |
| **Module Reuse** | Write custom provider code | Can use community Terraform modules |
| **Debugging** | Direct SDK errors | Terraform errors + Go orchestration layer |

## AWS EKS Module Example

Shows how infrastructure is defined in HCL instead of Go SDK calls:

**terraform/modules/aws/eks/main.tf:**

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# EKS Cluster IAM Role
resource "aws_iam_role" "cluster" {
  name = "${var.cluster_name}-cluster-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "eks.amazonaws.com"
      }
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "cluster_AmazonEKSClusterPolicy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  role       = aws_iam_role.cluster.name
}

# EKS Cluster
resource "aws_eks_cluster" "main" {
  name     = var.cluster_name
  version  = var.kubernetes_version
  role_arn = aws_iam_role.cluster.arn

  vpc_config {
    subnet_ids              = var.subnet_ids
    endpoint_private_access = true
    endpoint_public_access  = true
  }

  enabled_cluster_log_types = [
    "api",
    "audit",
    "authenticator",
    "controllerManager",
    "scheduler"
  ]

  tags = var.tags

  depends_on = [
    aws_iam_role_policy_attachment.cluster_AmazonEKSClusterPolicy
  ]
}

# Node Groups
resource "aws_eks_node_group" "node_pools" {
  for_each = { for np in var.node_pools : np.name => np }

  cluster_name    = aws_eks_cluster.main.name
  node_group_name = each.value.name
  node_role_arn   = aws_iam_role.node_group.arn
  subnet_ids      = var.subnet_ids

  instance_types = [each.value.instance_type]

  scaling_config {
    desired_size = each.value.min_size
    min_size     = each.value.min_size
    max_size     = each.value.max_size
  }

  labels = each.value.labels
  taints = [
    for taint in each.value.taints : {
      key    = taint.key
      value  = taint.value
      effect = taint.effect
    }
  ]

  tags = merge(var.tags, {
    "NodePool" = each.value.name
  })
}
```

**terraform/modules/aws/eks/outputs.tf:**

```hcl
output "cluster_id" {
  description = "EKS cluster ID"
  value       = aws_eks_cluster.main.id
}

output "cluster_endpoint" {
  description = "EKS cluster endpoint"
  value       = aws_eks_cluster.main.endpoint
}

output "cluster_ca_certificate" {
  description = "EKS cluster CA certificate"
  value       = base64decode(aws_eks_cluster.main.certificate_authority[0].data)
  sensitive   = true
}

output "cluster_oidc_issuer_url" {
  description = "OIDC issuer URL for IRSA"
  value       = aws_eks_cluster.main.identity[0].oidc[0].issuer
}
```

## Community Module Integration

**Major Advantage**: Can leverage battle-tested community modules instead of writing everything from scratch.

### Example: Using terraform-aws-modules/eks

```hcl
# terraform/modules/aws/eks/main.tf (using community module)
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0"

  cluster_name    = var.cluster_name
  cluster_version = var.kubernetes_version

  vpc_id     = var.vpc_id
  subnet_ids = var.subnet_ids

  # EKS Managed Node Groups
  eks_managed_node_groups = {
    for np in var.node_pools :
    np.name => {
      min_size       = np.min_size
      max_size       = np.max_size
      desired_size   = np.min_size
      instance_types = [np.instance_type]
      labels         = np.labels
      taints         = np.taints
    }
  }

  # Enable IRSA
  enable_irsa = true

  # Cluster addons
  cluster_addons = {
    coredns = {
      most_recent = true
    }
    kube-proxy = {
      most_recent = true
    }
    vpc-cni = {
      most_recent = true
    }
    aws-ebs-csi-driver = {
      most_recent = true
    }
  }

  tags = var.tags
}
```

### Benefits of Community Modules

- ✅ **Less code**: Reuse proven modules instead of writing 100s of lines of HCL
- ✅ **Best practices**: Community modules encode AWS/GCP/Azure best practices
- ✅ **Faster development**: Don't reinvent the wheel for common patterns
- ✅ **Battle-tested**: Modules used by thousands of companies, bugs are found quickly
- ✅ **Maintained**: Active community maintenance and updates

### Trade-offs of Community Modules

- ⚠️ **Less control**: Module abstractions may hide details you want to configure
- ⚠️ **Dependency**: Reliant on module maintainer to fix bugs and add features
- ⚠️ **Version tracking**: Must monitor and update module versions
- ⚠️ **Learning curve**: Need to understand module's abstraction layer

## Kubernetes Bootstrap Module

Handles post-cluster setup that's identical across providers:

**terraform/modules/kubernetes/main.tf:**

```hcl
terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.25"
    }
  }
}

# Namespaces for foundational software
resource "kubernetes_namespace_v1" "namespaces" {
  for_each = toset(var.namespaces)

  metadata {
    name = each.value
    labels = {
      "managed-by" = "nic"
      "nic.nebari.dev/namespace" = "true"
    }
  }
}

# Storage Classes (example for AWS)
resource "kubernetes_storage_class_v1" "gp3" {
  count = var.storage_class_gp3_enabled ? 1 : 0

  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = "true"
    }
  }

  storage_provisioner = "ebs.csi.aws.com"
  reclaim_policy      = "Delete"
  volume_binding_mode = "WaitForFirstConsumer"

  parameters = {
    type      = "gp3"
    encrypted = "true"
  }
}

# Priority Classes
resource "kubernetes_priority_class_v1" "high_priority" {
  metadata {
    name = "high-priority"
  }

  value          = 1000
  global_default = false
  description    = "High priority for critical workloads"
}

# Network Policies (deny all by default, allow within namespace)
resource "kubernetes_network_policy_v1" "deny_all" {
  for_each = toset(var.namespaces)

  metadata {
    name      = "deny-all"
    namespace = each.value
  }

  spec {
    pod_selector {}
    policy_types = ["Ingress", "Egress"]
  }

  depends_on = [kubernetes_namespace_v1.namespaces]
}

resource "kubernetes_network_policy_v1" "allow_same_namespace" {
  for_each = toset(var.namespaces)

  metadata {
    name      = "allow-same-namespace"
    namespace = each.value
  }

  spec {
    pod_selector {}
    policy_types = ["Ingress", "Egress"]

    ingress {
      from {
        pod_selector {}
      }
    }

    egress {
      to {
        pod_selector {}
      }
    }
  }

  depends_on = [kubernetes_namespace_v1.namespaces]
}
```

## Comparison with Native SDK Approach

### Code Volume

**OpenTofu Approach**:
- ~200 lines HCL for AWS EKS module
- ~100 lines HCL for GCP GKE module
- ~100 lines HCL for Azure AKS module
- OR ~50 lines using community modules

**Native SDK Approach**:
- ~300-400 lines Go for AWS provider
- ~300-400 lines Go for GCP provider
- ~300-400 lines Go for Azure provider
- No community module reuse option

### Debugging Example

**OpenTofu Error**:
```
Error: creating EKS Node Group: InvalidParameterException:
The following supplied instance types do not exist: [g5.xlarge]
```
→ Must understand: Terraform → AWS API → error propagation

**Native SDK Error**:
```go
err := eksClient.CreateNodeGroup(ctx, &eks.CreateNodeGroupInput{...})
// error: InvalidParameterException: instance type does not exist
```
→ Direct error from AWS SDK

## Summary

The OpenTofu approach trades some performance and complexity for:
- Access to battle-tested Terraform modules
- Faster initial development (reuse existing modules)
- Familiar patterns for teams with Terraform experience
- Standard state format and tooling

See [../README.md](../README.md) for full comparison and decision guidance.

---
