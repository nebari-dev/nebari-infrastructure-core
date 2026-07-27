package cluster

import (
	"context"
	"errors"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
	"go.opentelemetry.io/otel"
)

// fakeStateEditor is a test double for stateEditor that records StateRm calls
// and serves a canned Show result.
type fakeStateEditor struct {
	state    *tfjson.State
	showErr  error
	rmErrFor map[string]error // address -> error to return from StateRm
	rmCalls  []string
}

func (f *fakeStateEditor) Show(_ context.Context) (*tfjson.State, error) {
	return f.state, f.showErr
}

func (f *fakeStateEditor) StateRm(_ context.Context, address string) error {
	f.rmCalls = append(f.rmCalls, address)
	if f.rmErrFor != nil {
		if err, ok := f.rmErrFor[address]; ok {
			return err
		}
	}
	return nil
}

// stateWith builds a minimal state whose root module contains the given
// resource addresses.
func stateWith(addrs ...string) *tfjson.State {
	resources := make([]*tfjson.StateResource, 0, len(addrs))
	for _, a := range addrs {
		resources = append(resources, &tfjson.StateResource{Address: a})
	}
	return &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{Resources: resources},
		},
	}
}

func TestRetainBackupResources(t *testing.T) {
	ctx := context.Background()
	// A real (no-op) span satisfies trace.Span; RecordError is a no-op.
	_, sp := otel.Tracer("test").Start(ctx, "test")

	addrs := []string{
		"aws_s3_bucket_public_access_block.longhorn_backup[0]",
		"aws_s3_bucket_versioning.longhorn_backup[0]",
		"aws_s3_bucket.longhorn_backup[0]",
	}

	t.Run("nil spec removes nothing", func(t *testing.T) {
		f := &fakeStateEditor{state: stateWith(addrs...)}
		RetainBackupResources(ctx, sp, f, nil, addrs)
		if len(f.rmCalls) != 0 {
			t.Fatalf("expected no StateRm calls, got %v", f.rmCalls)
		}
	})

	t.Run("force destroy removes nothing", func(t *testing.T) {
		f := &fakeStateEditor{state: stateWith(addrs...)}
		RetainBackupResources(ctx, sp, f, &BackupBucketSpec{ForceDestroy: true}, addrs)
		if len(f.rmCalls) != 0 {
			t.Fatalf("expected no StateRm calls, got %v", f.rmCalls)
		}
	})

	t.Run("empty addrs removes nothing and skips Show", func(t *testing.T) {
		f := &fakeStateEditor{state: stateWith(addrs...)}
		RetainBackupResources(ctx, sp, f, &BackupBucketSpec{ForceDestroy: false}, nil)
		if len(f.rmCalls) != 0 {
			t.Fatalf("expected no StateRm calls, got %v", f.rmCalls)
		}
	})

	t.Run("retain removes only addresses present in state", func(t *testing.T) {
		// State holds only 2 of the 3 candidate addresses.
		present := []string{
			"aws_s3_bucket_public_access_block.longhorn_backup[0]",
			"aws_s3_bucket.longhorn_backup[0]",
		}
		f := &fakeStateEditor{state: stateWith(present...)}
		RetainBackupResources(ctx, sp, f, &BackupBucketSpec{ForceDestroy: false}, addrs)
		if len(f.rmCalls) != 2 {
			t.Fatalf("expected 2 StateRm calls, got %v", f.rmCalls)
		}
		for _, addr := range present {
			found := false
			for _, c := range f.rmCalls {
				if c == addr {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected StateRm for present address %q, calls=%v", addr, f.rmCalls)
			}
		}
		// The absent address must never be passed to StateRm.
		for _, c := range f.rmCalls {
			if c == "aws_s3_bucket_versioning.longhorn_backup[0]" {
				t.Fatalf("StateRm called for absent address %q", c)
			}
		}
	})

	t.Run("Show error aborts removal without panic", func(t *testing.T) {
		f := &fakeStateEditor{showErr: errors.New("boom")}
		RetainBackupResources(ctx, sp, f, &BackupBucketSpec{ForceDestroy: false}, addrs)
		if len(f.rmCalls) != 0 {
			t.Fatalf("expected no StateRm calls after Show error, got %v", f.rmCalls)
		}
	})

	t.Run("per-address StateRm error still attempts the rest", func(t *testing.T) {
		f := &fakeStateEditor{
			state: stateWith(addrs...),
			rmErrFor: map[string]error{
				"aws_s3_bucket_public_access_block.longhorn_backup[0]": errors.New("locked"),
			},
		}
		RetainBackupResources(ctx, sp, f, &BackupBucketSpec{ForceDestroy: false}, addrs)
		// All three present addresses are attempted despite the first failing.
		if len(f.rmCalls) != 3 {
			t.Fatalf("expected all 3 addresses attempted, got %v", f.rmCalls)
		}
	})
}

func TestPresentStateAddresses(t *testing.T) {
	t.Run("nil and empty states return empty set", func(t *testing.T) {
		for _, s := range []*tfjson.State{
			nil,
			{},
			{Values: &tfjson.StateValues{}},
		} {
			if got := PresentStateAddresses(s); len(got) != 0 {
				t.Fatalf("expected empty set, got %v", got)
			}
		}
	})

	t.Run("collects root and child module addresses", func(t *testing.T) {
		state := &tfjson.State{
			Values: &tfjson.StateValues{
				RootModule: &tfjson.StateModule{
					Resources: []*tfjson.StateResource{
						{Address: "aws_s3_bucket.longhorn_backup[0]"},
						{Address: "aws_vpc.main"},
						nil, // defensively skipped
						{Address: ""},
					},
					ChildModules: []*tfjson.StateModule{
						{Resources: []*tfjson.StateResource{
							{Address: "module.eks.aws_eks_cluster.this[0]"},
						}},
					},
				},
			},
		}
		got := PresentStateAddresses(state)
		want := []string{
			"aws_s3_bucket.longhorn_backup[0]",
			"aws_vpc.main",
			"module.eks.aws_eks_cluster.this[0]",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d addresses, want %d: %v", len(got), len(want), got)
		}
		for _, addr := range want {
			if !got[addr] {
				t.Fatalf("missing address %q in %v", addr, got)
			}
		}
	})
}
