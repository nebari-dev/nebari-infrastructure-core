package cluster

import (
	"context"

	tfjson "github.com/hashicorp/terraform-json"
	"go.opentelemetry.io/otel/trace"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// stateEditor is the subset of the tofu executor RetainBackupResources needs.
// *tofu.TerraformExecutor satisfies it via its signal-safe Show/StateRm
// wrappers; a fake implementation makes RetainBackupResources unit-testable
// without a live tofu binary.
type stateEditor interface {
	Show(ctx context.Context) (*tfjson.State, error)
	StateRm(ctx context.Context, address string) error
}

// RetainBackupResources drops the given Terraform state addresses (a
// NIC-provisioned Longhorn backup bucket/container and its dependents) from
// state so a subsequent `tofu destroy` leaves them — and their backups —
// intact. Callers pass the provider-specific addresses via addrs.
//
// It is a no-op when there is nothing to retain: no NIC-provisioned bucket
// (spec == nil), retain_on_destroy is off (spec.ForceDestroy == true), or no
// addresses were supplied.
//
// It is best-effort: only addresses actually present in state are removed (so
// `tofu state rm` never errors on a missing address — e.g. a bucket that was
// never created), and a Show failure or a per-address StateRm failure is
// reported as a warning and skipped rather than aborting teardown. Orphaning a
// bucket that should have been retained is preferable to failing the whole
// destroy.
func RetainBackupResources(ctx context.Context, span trace.Span, tf stateEditor, spec *BackupBucketSpec, addrs []string) {
	if spec == nil || spec.ForceDestroy || len(addrs) == 0 {
		return
	}

	state, err := tf.Show(ctx)
	if err != nil {
		// Without state we can't tell which addresses are present. Skip rather
		// than risk `tofu state rm` erroring on an absent address and aborting.
		span.RecordError(err)
		status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not inspect Terraform state to retain Longhorn backup resources; they may be destroyed").
			WithResource("backup-bucket").WithAction("retain").
			WithMetadata("error", err.Error()))
		return
	}

	present := PresentStateAddresses(state)
	for _, addr := range addrs {
		if !present[addr] {
			continue
		}
		if err := tf.StateRm(ctx, addr); err != nil {
			span.RecordError(err)
			status.Send(ctx, status.NewUpdate(status.LevelWarning, "Could not retain Longhorn backup resource in Terraform state; it may be destroyed").
				WithResource("backup-bucket").WithAction("retain").
				WithMetadata("error", err.Error()).WithMetadata("address", addr))
			continue
		}
		status.Send(ctx, status.NewUpdate(status.LevelInfo, "Retained Longhorn backup resource (removed from Terraform state)").
			WithResource("backup-bucket").WithAction("retain").
			WithMetadata("address", addr))
	}
}

// PresentStateAddresses collects the absolute addresses of every resource in
// the state's root module and any child modules. The Longhorn backup resources
// live in the root module, but walking child modules keeps the helper correct
// if the module layout changes.
func PresentStateAddresses(state *tfjson.State) map[string]bool {
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
