package aws

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// backupStateModuleVersion is the terraform-aws-eks-cluster release the
// addresses in backupStateAddrs were verified against (via `tofu graph` on the
// real module). TestBackupStateAddrsVerifiedAgainstPinnedModule asserts it
// matches the version pinned in templates/main.tf, so a module bump fails the
// test and forces the addresses to be re-verified — a drifted address list is
// otherwise silent and destroys the backups retain_on_destroy promised to keep
// (cluster.RetainBackupResources warns but cannot fail the destroy).
const backupStateModuleVersion = "0.7.0"

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
	// These resources live in the eks_cluster module's own longhorn-backup
	// submodule, so the address is nested twice: module.eks_cluster (the shim's
	// call) then module.longhorn_backup (the module's call). Both the submodule
	// call and the resources inside it are count-gated, hence the two [0]
	// indices; if either ever moves to `for_each`, these addresses (e.g. [0] ->
	// ["<key>"]) must be updated to match.
	//
	// Verified against terraform-aws-eks-cluster backupStateModuleVersion; the
	// test tying that constant to templates/main.tf forces re-verification on
	// every module bump.
	return []string{
		"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_public_access_block.this[0]",
		"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_server_side_encryption_configuration.this[0]",
		"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_lifecycle_configuration.this[0]",
		"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_versioning.this[0]",
		"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket.this[0]",
	}
}
