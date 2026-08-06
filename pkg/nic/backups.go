package nic

import (
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// ensureBackupsHaveLonghorn returns an error when Longhorn backups are enabled
// but the selected cluster provider's storage layer is not Longhorn (i.e.
// Longhorn is not installed for this cluster). Longhorn backups layer
// BackupTarget/RecurringJob/Setting (longhorn.io/v1beta2) CRs and Longhorn
// volumes, which only exist when the provider installs Longhorn (AWS/Hetzner/
// existing today; not Azure, which uses managed-csi). Without this guard the
// deploy half-succeeds and the ArgoCD app never syncs because the longhorn.io
// CRDs are absent, so we fail fast with a clear error.
func ensureBackupsHaveLonghorn(cfg *config.NebariConfig, storageClass string) error {
	if !cfg.Backups.LonghornEnabled() {
		return nil
	}
	if storageClass != longhorn.StorageClassName {
		return fmt.Errorf("backups.longhorn is enabled but the %q provider's storage layer is %q, not Longhorn; Longhorn backups require Longhorn to be the cluster storage class (%q). Longhorn is not installed on this provider/config — see docs/longhorn-backups.md",
			cfg.Cluster.ProviderName(), storageClass, longhorn.StorageClassName)
	}
	return nil
}
