# Longhorn backup storage account + container, provisioned only when
# create_container is set in backups.longhorn.azure. Backs Longhorn's native
# azblob:// BackupTarget. NOTE: retain_on_destroy for Azure is enforced via NIC
# (the bucket spec is omitted from the Destroy path); azurerm has no force_destroy
# equivalent for non-empty containers, so a normal `tofu destroy` removing the
# storage account also removes its backups — operators relying on retain must
# delete the cluster without destroying this storage account, or move it to a
# separate resource group. (See docs/longhorn-backups.md.)
resource "azurerm_storage_account" "longhorn_backup" {
  count                    = var.backup_container_create ? 1 : 0
  name                     = var.backup_storage_account
  resource_group_name      = module.aks_cluster.resource_group_name
  location                 = var.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
  tags                     = var.tags
}

resource "azurerm_storage_container" "longhorn_backup" {
  count              = var.backup_container_create ? 1 : 0
  name               = var.backup_container_name
  storage_account_id = azurerm_storage_account.longhorn_backup[0].id
}
