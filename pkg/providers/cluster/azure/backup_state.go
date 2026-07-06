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
	if spec == nil || spec.ForceDestroy {
		return nil
	}
	// These resources live inside the aks_cluster module (see the module's
	// longhorn_backup.tf), so addresses are prefixed with module.aks_cluster.
	// The [0] indices correspond to the `count = ... ? 1 : 0` form the module
	// uses; if it ever moves to `for_each`, these addresses (e.g. [0] ->
	// ["<key>"]) must be updated to match.
	return []string{
		"module.aks_cluster.azurerm_storage_container.longhorn_backup[0]",
		"module.aks_cluster.azurerm_storage_account.longhorn_backup[0]",
	}
}
