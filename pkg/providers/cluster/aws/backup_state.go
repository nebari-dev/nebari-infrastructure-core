package aws

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// backupStateAddrs returns the Terraform state addresses for a NIC-provisioned
// Longhorn backup S3 bucket that must be removed from state before `tofu
// destroy` so the bucket and its backups survive teardown. It returns nil when
// there is nothing to retain: no spec (spec == nil), a spec that did not create
// the bucket (spec.Create == false — e.g. a keyless Pod Identity association
// scoped to a pre-existing bucket, or an external bucket), or retain_on_destroy
// is off (spec.ForceDestroy == true), in which case the bucket is destroyed
// normally.
//
// Addresses are ordered dependents-first so a removal that processes them in
// order never references an already-removed parent.
func backupStateAddrs(spec *cluster.BackupBucketSpec) []string {
	if spec == nil || !spec.Create || spec.ForceDestroy {
		return nil
	}
	// These resources live inside the eks_cluster module (see the module's
	// longhorn_backup.tf), so addresses are prefixed with module.eks_cluster.
	// The [0] indices correspond to the `count = ... ? 1 : 0` form the module
	// uses; if it ever moves to `for_each`, these addresses (e.g. [0] ->
	// ["<key>"]) must be updated to match.
	return []string{
		"module.eks_cluster.aws_s3_bucket_public_access_block.longhorn_backup[0]",
		"module.eks_cluster.aws_s3_bucket_server_side_encryption_configuration.longhorn_backup[0]",
		"module.eks_cluster.aws_s3_bucket_versioning.longhorn_backup[0]",
		"module.eks_cluster.aws_s3_bucket.longhorn_backup[0]",
	}
}
