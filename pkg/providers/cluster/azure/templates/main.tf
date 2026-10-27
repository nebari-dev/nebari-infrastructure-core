module "aks_cluster" {
  # TEMPORARY: pinned to the head of nebari-dev/terraform-azurerm-aks-cluster#6
  # to test the azurerm 5.x changes. Switch back to the registry source at the
  # release that PR produces before merging.
  source = "git::https://github.com/nebari-dev/terraform-azurerm-aks-cluster.git?ref=010c23ea3b7af5d51222585e64d29804bb9b9a09"

  project_name                 = var.project_name
  location                     = var.location
  tags                         = var.tags
  create_resource_group        = var.create_resource_group
  existing_resource_group_name = var.existing_resource_group_name
  create_vnet                  = var.create_vnet
  vnet_cidr_block              = var.vnet_cidr_block
  node_subnet_cidr_block       = var.node_subnet_cidr_block
  existing_vnet_id             = var.existing_vnet_id
  existing_node_subnet_id      = var.existing_node_subnet_id
  network_plugin               = var.network_plugin
  network_plugin_mode          = var.network_plugin_mode
  network_data_plane           = var.network_data_plane
  node_provisioning_mode       = var.node_provisioning_mode
  pod_cidr                     = var.pod_cidr
  service_cidr                 = var.service_cidr
  dns_service_ip               = var.dns_service_ip
  kubernetes_version           = var.kubernetes_version
  private_cluster_enabled      = var.private_cluster_enabled
  authorized_ip_ranges         = var.authorized_ip_ranges
  sku_tier                     = var.sku_tier
  identity_type                = var.identity_type
  node_groups                  = var.node_groups

  longhorn_backup_container_create = var.backup_container_create
  longhorn_backup_storage_account  = var.backup_storage_account
  longhorn_backup_container_name   = var.backup_container_name
}
