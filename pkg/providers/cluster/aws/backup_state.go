package aws

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// backupStateAddrs returns the Terraform state addresses for a NIC-provisioned
// Longhorn backup S3 bucket that must be removed from state before `tofu
// destroy` so the bucket and its backups survive teardown. It returns nil when
// there is nothing to retain: no NIC-provisioned bucket (spec == nil) or
// retain_on_destroy is off (spec.ForceDestroy == true), in which case the
// bucket should be destroyed normally.
//
// Addresses are ordered dependents-first so a removal that processes them in
// order never references an already-removed parent.
func backupStateAddrs(spec *cluster.BackupBucketSpec) []string {
	if spec == nil || spec.ForceDestroy {
		return nil
	}
	return []string{
		"aws_s3_bucket_public_access_block.longhorn_backup[0]",
		"aws_s3_bucket_server_side_encryption_configuration.longhorn_backup[0]",
		"aws_s3_bucket_versioning.longhorn_backup[0]",
		"aws_s3_bucket.longhorn_backup[0]",
	}
}
