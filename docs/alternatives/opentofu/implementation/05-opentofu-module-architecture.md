# OpenTofu Module Architecture

### 5.1 Module Structure

**Repository Layout:**
```
nebari-infrastructure-core/
├── cmd/nic/               # Go CLI
├── pkg/
│   ├── tofu/             # terraform-exec wrapper
│   ├── config/           # Parse nebari-config.yaml
│   ├── kubernetes/       # Wait for readiness, health checks
│   └── operator/         # Nebari operator (separate repo, vendored)
├── terraform/
│   ├── main.tf           # Root module
│   ├── variables.tf      # Input variables
│   ├── outputs.tf        # Outputs (kubeconfig, URLs, etc.)
│   ├── backend.tf.tmpl   # Backend configuration template
│   ├── providers.tf      # Provider configurations
│   └── modules/
│       ├── aws/
│       │   ├── vpc/
│       │   │   ├── main.tf
│       │   │   ├── variables.tf
│       │   │   └── outputs.tf
│       │   ├── eks/
│       │   │   ├── main.tf
│       │   │   ├── variables.tf
│       │   │   └── outputs.tf
│       │   └── efs/
│       ├── gcp/
│       │   ├── vpc/
│       │   ├── gke/
│       │   └── filestore/
│       ├── azure/
│       │   ├── vnet/
│       │   ├── aks/
│       │   └── azure-files/
│       ├── local/
│       │   └── k3s/
│       ├── kubernetes/    # K8s bootstrap (namespaces, RBAC, etc.)
│       ├── argocd/        # ArgoCD Helm deployment
│       └── foundational-apps/  # ArgoCD Applications
└── go.mod
```

### 5.2 Root Module Design

**terraform/main.tf:**
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

# AWS Infrastructure
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

module "aws_efs" {
  count  = local.is_aws ? 1 : 0
  source = "./modules/aws/efs"

  name       = "${var.cluster_name}-shared-storage"
  vpc_id     = module.aws_vpc[0].vpc_id
  subnet_ids = module.aws_vpc[0].private_subnet_ids
  tags       = var.tags
}

# GCP Infrastructure
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

# Azure Infrastructure
module "azure_vnet" {
  count  = local.is_azure ? 1 : 0
  source = "./modules/azure/vnet"

  name                = var.cluster_name
  location            = var.region
  resource_group_name = var.azure_resource_group
  address_space       = var.azure_vnet_address_space
}

module "azure_aks" {
  count  = local.is_azure ? 1 : 0
  source = "./modules/azure/aks"

  cluster_name       = var.cluster_name
  kubernetes_version = var.kubernetes_version
  location           = var.region
  resource_group_name = var.azure_resource_group
  vnet_subnet_id     = module.azure_vnet[0].subnet_id
  node_pools         = var.node_pools
}

# Local K3s
module "local_k3s" {
  count  = local.is_local ? 1 : 0
  source = "./modules/local/k3s"

  cluster_name = var.cluster_name
  node_pools   = var.node_pools
}

# Kubernetes Bootstrap (runs after cluster provisioned)
module "kubernetes_bootstrap" {
  source = "./modules/kubernetes"

  cluster_endpoint = (
    local.is_aws   ? module.aws_eks[0].cluster_endpoint :
    local.is_gcp   ? module.gcp_gke[0].cluster_endpoint :
    local.is_azure ? module.azure_aks[0].cluster_endpoint :
    local.is_local ? module.local_k3s[0].cluster_endpoint :
    ""
  )

  cluster_ca_certificate = (
    local.is_aws   ? module.aws_eks[0].cluster_ca_certificate :
    local.is_gcp   ? module.gcp_gke[0].cluster_ca_certificate :
    local.is_azure ? module.azure_aks[0].cluster_ca_certificate :
    local.is_local ? module.local_k3s[0].cluster_ca_certificate :
    ""
  )

  namespaces = [
    "nebari-system",
    "monitoring",
    "cert-manager",
    "envoy-gateway-system"
  ]

  depends_on = [
    module.aws_eks,
    module.gcp_gke,
    module.azure_aks,
    module.local_k3s
  ]
}

# ArgoCD
module "argocd" {
  source = "./modules/argocd"

  namespace             = "nebari-system"
  argocd_version        = var.argocd_version
  domain                = var.domain
  foundational_repo_url = var.foundational_software_repo_url

  depends_on = [module.kubernetes_bootstrap]
}

# Foundational Software ArgoCD Applications
module "foundational_apps" {
  source = "./modules/foundational-apps"

  namespace             = "nebari-system"
  domain                = var.domain
  foundational_repo_url = var.foundational_software_repo_url
  letsencrypt_email     = var.letsencrypt_email

  # Component versions
  cert_manager_version = var.cert_manager_version
  envoy_gateway_version = var.envoy_gateway_version
  keycloak_version     = var.keycloak_version
  grafana_version      = var.grafana_version
  loki_version         = var.loki_version
  mimir_version        = var.mimir_version
  tempo_version        = var.tempo_version
  otel_version         = var.otel_collector_version

  depends_on = [module.argocd]
}
```

### 5.3 AWS EKS Module Example

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
    subnet_ids             = var.subnet_ids
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

# Node Group IAM Role
resource "aws_iam_role" "node_group" {
  name = "${var.cluster_name}-node-group-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "node_group_AmazonEKSWorkerNodePolicy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy"
  role       = aws_iam_role.node_group.name
}

resource "aws_iam_role_policy_attachment" "node_group_AmazonEKS_CNI_Policy" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
  role       = aws_iam_role.node_group.name
}

resource "aws_iam_role_policy_attachment" "node_group_AmazonEC2ContainerRegistryReadOnly" {
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
  role       = aws_iam_role.node_group.name
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

  depends_on = [
    aws_iam_role_policy_attachment.node_group_AmazonEKSWorkerNodePolicy,
    aws_iam_role_policy_attachment.node_group_AmazonEKS_CNI_Policy,
    aws_iam_role_policy_attachment.node_group_AmazonEC2ContainerRegistryReadOnly,
  ]
}

# EBS CSI Driver IAM Role (IRSA)
module "ebs_csi_irsa" {
  source  = "terraform-aws-modules/iam/aws//modules/iam-role-for-service-accounts-eks"
  version = "~> 5.0"

  role_name = "${var.cluster_name}-ebs-csi-driver"

  attach_ebs_csi_policy = true

  oidc_providers = {
    main = {
      provider_arn               = aws_eks_cluster.main.identity[0].oidc[0].issuer
      namespace_service_accounts = ["kube-system:ebs-csi-controller-sa"]
    }
  }

  tags = var.tags
}

# EBS CSI Driver Addon
resource "aws_eks_addon" "ebs_csi_driver" {
  cluster_name             = aws_eks_cluster.main.name
  addon_name               = "aws-ebs-csi-driver"
  service_account_role_arn = module.ebs_csi_irsa.iam_role_arn

  depends_on = [aws_eks_node_group.node_pools]
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

output "cluster_arn" {
  description = "EKS cluster ARN"
  value       = aws_eks_cluster.main.arn
}

output "cluster_oidc_issuer_url" {
  description = "OIDC issuer URL for IRSA"
  value       = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

output "node_groups" {
  description = "Map of node group details"
  value = {
    for name, ng in aws_eks_node_group.node_pools :
    name => {
      id     = ng.id
      arn    = ng.arn
      status = ng.status
    }
  }
}
```

### 5.4 Kubernetes Bootstrap Module

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

# Namespaces
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

# Storage Classes
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

resource "kubernetes_storage_class_v1" "efs" {
  count = var.storage_class_efs_enabled ? 1 : 0

  metadata {
    name = "efs"
  }

  storage_provisioner = "efs.csi.aws.com"
  reclaim_policy      = "Retain"
  volume_binding_mode = "Immediate"
}

# Cluster Roles
resource "kubernetes_cluster_role_v1" "namespace_reader" {
  metadata {
    name = "namespace-reader"
  }

  rule {
    api_groups = [""]
    resources  = ["namespaces"]
    verbs      = ["get", "list", "watch"]
  }
}

# Service Accounts
resource "kubernetes_service_account_v1" "argocd" {
  metadata {
    name      = "argocd-server"
    namespace = "nebari-system"
  }

  depends_on = [kubernetes_namespace_v1.namespaces]
}

# Priority Classes
resource "kubernetes_priority_class_v1" "high_priority" {
  metadata {
    name = "high-priority"
  }

  value             = 1000
  global_default    = false
  description       = "High priority class for critical workloads"
}

resource "kubernetes_priority_class_v1" "low_priority" {
  metadata {
    name = "low-priority"
  }

  value             = 100
  global_default    = false
  description       = "Low priority class for non-critical workloads"
}

# Network Policies (example: deny all by default, allow within namespace)
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

### 5.5 Community Module Integration

**Leverage Existing Modules:**

Instead of writing all modules from scratch, we can use proven community modules:

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

**Benefits:**
- ✅ Less code to write and maintain
- ✅ Battle-tested modules (community vetting)
- ✅ Automatic best practices
- ✅ Faster development

**Trade-off:**
- ⚠️ Less control over implementation details
- ⚠️ Dependency on module maintainers
- ⚠️ Must track module version updates

---
