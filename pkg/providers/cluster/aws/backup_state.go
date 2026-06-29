package aws

import (
	"context"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
	"go.opentelemetry.io/otel/trace"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
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

// retainBackupBucket drops a retained Longhorn backup bucket (and its dependent
// resources) from Terraform state so a subsequent `tofu destroy` leaves the
// bucket — and its backups — intact. It is best-effort: a backup bucket that
// was never created (addresses absent from state) is silently skipped, and a
// removal failure is reported as a warning rather than aborting teardown, since
// orphaning a bucket that should have been retained is preferable to failing
// the whole destroy. Only the addresses actually present in state are removed,
// so `tofu state rm` never errors on a missing address.
func retainBackupBucket(ctx context.Context, span trace.Span, tf *tofu.TerraformExecutor, spec *cluster.BackupBucketSpec) {
	addrs := backupStateAddrs(spec)
	if len(addrs) == 0 {
		return
	}

	state, err := tf.Show(ctx)
	if err != nil {
		// Without state we can't tell which addresses are present. Skip rather
		// than risk `tofu state rm` erroring on an absent address and aborting.
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning,
			fmt.Sprintf("Could not inspect Terraform state to retain Longhorn backup bucket; it may be destroyed: %v", err)).
			WithResource("backup-bucket").WithAction("retain"))
		return
	}

	present := presentAddresses(state)
	for _, addr := range addrs {
		if !present[addr] {
			continue
		}
		if err := tf.StateRm(ctx, addr); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning,
				fmt.Sprintf("Failed to remove %s from Terraform state; the Longhorn backup bucket may be destroyed: %v", addr, err)).
				WithResource("backup-bucket").WithAction("retain"))
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo,
			fmt.Sprintf("Retained Longhorn backup resource %s (removed from Terraform state)", addr)).
			WithResource("backup-bucket").WithAction("retain"))
	}
}

// presentAddresses collects the absolute addresses of every resource in the
// state's root module (and any child modules, defensively). The Longhorn backup
// resources live in the root module, but walking child modules keeps the helper
// correct if the module layout changes.
func presentAddresses(state *tfjson.State) map[string]bool {
	out := make(map[string]bool)
	if state == nil || state.Values == nil || state.Values.RootModule == nil {
		return out
	}
	var walk func(m *tfjson.StateModule)
	walk = func(m *tfjson.StateModule) {
		if m == nil {
			return
		}
		for _, r := range m.Resources {
			if r != nil && r.Address != "" {
				out[r.Address] = true
			}
		}
		for _, child := range m.ChildModules {
			walk(child)
		}
	}
	walk(state.Values.RootModule)
	return out
}
