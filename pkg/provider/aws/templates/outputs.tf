output "cluster_name" {
  description = "The name of the EKS cluster"
  value       = module.eks_cluster.cluster_name
}

output "cluster_endpoint" {
  description = "Endpoint for your Kubernetes API server"
  value       = module.eks_cluster.cluster_endpoint
}

output "cluster_certificate_authority_data" {
  description = "Base64 encoded certificate data required to communicate with the cluster"
  value       = module.eks_cluster.cluster_certificate_authority_data
  sensitive   = true
}

output "cluster_arn" {
  description = "The Amazon Resource Name (ARN) of the cluster"
  value       = module.eks_cluster.cluster_arn
}

output "cluster_oidc_issuer_url" {
  description = "The URL on the EKS cluster for the OpenID Connect identity provider"
  value       = module.eks_cluster.cluster_oidc_issuer_url
}

output "oidc_provider_arn" {
  description = "ARN of the OIDC Provider for EKS (for IRSA)"
  value       = module.eks_cluster.oidc_provider_arn
}

output "vpc_id" {
  description = "The ID of the VPC"
  value       = module.eks_cluster.vpc_id
}

output "private_subnet_ids" {
  description = "List of private subnet IDs"
  value       = module.eks_cluster.private_subnet_ids
}

output "efs_id" {
  description = "The ID of the EFS file system"
  value       = module.eks_cluster.efs_id
}
