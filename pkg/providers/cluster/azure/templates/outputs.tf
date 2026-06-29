output "cluster_id" {
  description = "Full Azure resource ID of the AKS cluster."
  value       = module.aks_cluster.cluster_id
}

output "cluster_name" {
  description = "Name of the AKS cluster."
  value       = module.aks_cluster.cluster_name
}

output "cluster_fqdn" {
  description = "Fully-qualified domain name of the AKS API server."
  value       = module.aks_cluster.cluster_fqdn
}

output "host" {
  description = "AKS API server endpoint URL."
  value       = module.aks_cluster.host
  sensitive   = true
}

# The aks_cluster module exposes kube_config_raw (local cluster-admin), not
# kube_admin_config_raw — the latter is only populated when AAD integration
# is enabled, which the module doesn't do.
output "kube_config_raw" {
  description = "Raw cluster-admin kubeconfig for the AKS cluster."
  value       = module.aks_cluster.kube_config_raw
  sensitive   = true
}

output "cluster_ca_certificate" {
  description = "Base64-encoded CA certificate of the AKS API server."
  value       = module.aks_cluster.cluster_ca_certificate
  sensitive   = true
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL for workload identity federation."
  value       = module.aks_cluster.oidc_issuer_url
}

output "kubelet_identity_object_id" {
  description = "Object ID of the kubelet user-assigned managed identity."
  value       = module.aks_cluster.kubelet_identity_object_id
}

output "kubelet_identity_client_id" {
  description = "Client ID of the kubelet user-assigned managed identity."
  value       = module.aks_cluster.kubelet_identity_client_id
}

output "node_resource_group" {
  description = "Name of the auto-generated MC_ resource group holding cluster nodes."
  value       = module.aks_cluster.node_resource_group
}

output "resource_group_name" {
  description = "Name of the resource group containing the AKS cluster."
  value       = module.aks_cluster.resource_group_name
}

output "vnet_id" {
  description = "Resource ID of the VNet used by the cluster."
  value       = module.aks_cluster.vnet_id
}

output "node_subnet_id" {
  description = "Resource ID of the subnet hosting the cluster nodes."
  value       = module.aks_cluster.node_subnet_id
}

output "longhorn_backup_container" {
  description = "Name of the Longhorn backup container; empty when not created by NIC"
  value       = var.backup_container_create ? azurerm_storage_container.longhorn_backup[0].name : ""
}
