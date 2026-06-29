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
	return []string{
		"azurerm_storage_container.longhorn_backup[0]",
		"azurerm_storage_account.longhorn_backup[0]",
	}
}
