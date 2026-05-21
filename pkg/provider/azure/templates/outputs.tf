output "cluster_id" {
  value = module.aks_cluster.cluster_id
}

output "cluster_name" {
  value = module.aks_cluster.cluster_name
}

output "cluster_fqdn" {
  value = module.aks_cluster.cluster_fqdn
}

output "host" {
  value     = module.aks_cluster.host
  sensitive = true
}

output "kube_admin_config_raw" {
  value     = module.aks_cluster.kube_admin_config_raw
  sensitive = true
}

output "cluster_ca_certificate" {
  value     = module.aks_cluster.cluster_ca_certificate
  sensitive = true
}

output "oidc_issuer_url" {
  value = module.aks_cluster.oidc_issuer_url
}

output "kubelet_identity_object_id" {
  value = module.aks_cluster.kubelet_identity_object_id
}

output "kubelet_identity_client_id" {
  value = module.aks_cluster.kubelet_identity_client_id
}

output "node_resource_group" {
  value = module.aks_cluster.node_resource_group
}

output "resource_group_name" {
  value = module.aks_cluster.resource_group_name
}

output "vnet_id" {
  value = module.aks_cluster.vnet_id
}

output "node_subnet_id" {
  value = module.aks_cluster.node_subnet_id
}
