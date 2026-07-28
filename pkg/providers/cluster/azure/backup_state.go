package azure

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// backupStateAddrs returns the Terraform state addresses for a NIC-provisioned
// Longhorn backup storage account + container that must be removed from state
// before `tofu destroy` so they (and their backups) survive teardown. It
// returns nil when there is nothing to retain: no NIC-provisioned container
// (spec == nil) or retain_on_destroy is off (spec.ForceDestroy == true), in
// which case the storage account should be destroyed normally.
//
// Addresses are ordered dependents-first (container before account) so a
// removal that processes them in order never references an already-removed
// parent.
func backupStateAddrs(spec *cluster.BackupBucketSpec) []string {
	if spec == nil || !spec.Create || spec.ForceDestroy {
		return nil
	}
	// These resources live in the aks_cluster module's own longhorn-backup
	// submodule, so the address is nested twice: module.aks_cluster (the shim's
	// call) then module.longhorn_backup (the module's call). Only the submodule
	// call is count-gated — the resources inside it are unconditional — so the
	// single [0] sits on module.longhorn_backup and the resources take no index.
	// If either ever moves to `for_each`, these addresses (e.g. [0] ->
	// ["<key>"]) must be updated to match.
	//
	// Verified against terraform-azurerm-aks-cluster v0.2.0, the version pinned
	// in templates/main.tf. Re-check on every module bump — a mismatch here is
	// silent (see cluster.RetainBackupResources).
	return []string{
		"module.aks_cluster.module.longhorn_backup[0].azurerm_storage_container.this",
		"module.aks_cluster.module.longhorn_backup[0].azurerm_storage_account.this",
	}
}
