output "cluster_id" {
  description = "The ROSA HCP cluster ID."
  value       = module.rosa_hcp.cluster_id
}

output "cluster_name" {
  description = "The ROSA HCP cluster name."
  value       = var.cluster_name
}

output "api_url" {
  description = "The cluster's Kubernetes API server URL."
  value       = module.rosa_hcp.cluster_api_url
}

output "console_url" {
  description = "The OpenShift web console URL."
  value       = module.rosa_hcp.cluster_console_url
}

output "vpc_id" {
  description = "The ID of the created VPC."
  value       = module.vpc.vpc_id
}
