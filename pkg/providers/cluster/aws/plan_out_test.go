package aws

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// TestAppendPlanOutOpt asserts the mechanism Deploy relies on to honor
// cluster.DeployOptions.PlanOut: when the operator passes --plan-out, the AWS
// provider must append a tfexec.Out option carrying that path to the Plan
// call, and otherwise leave the option slice alone. Verified via reflection on
// *tfexec.OutOption's unexported path field because tfexec gives us no public
// accessor, and the alternative (running real tofu) is far beyond a unit test.
func TestAppendPlanOutOpt(t *testing.T) {
	tests := []struct {
		name     string
		existing []tfexec.PlanOption
		path     string
		wantLen  int
		wantPath string // empty means no OutOption should be present
	}{
		{
			name:    "empty path on empty slice is a no-op",
			path:    "",
			wantLen: 0,
		},
		{
			name:     "empty path preserves existing opts untouched",
			existing: []tfexec.PlanOption{tfexec.Var("x=1")},
			path:     "",
			wantLen:  1,
		},
		{
			name:     "non-empty path on empty slice appends one OutOption",
			path:     "/abs/plan.tfplan",
			wantLen:  1,
			wantPath: "/abs/plan.tfplan",
		},
		{
			name:     "non-empty path appends after existing opts without disturbing them",
			existing: []tfexec.PlanOption{tfexec.Var("x=1"), tfexec.Var("y=2")},
			path:     "/abs/plan.tfplan",
			wantLen:  3,
			wantPath: "/abs/plan.tfplan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendPlanOutOpt(tt.existing, tt.path)
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantPath == "" {
				for i, opt := range got {
					if _, ok := opt.(*tfexec.OutOption); ok {
						t.Fatalf("opts[%d] is an unexpected *tfexec.OutOption", i)
					}
				}
				return
			}
			// The OutOption must be the last entry (appended), and must carry the path.
			last := got[len(got)-1]
			outOpt, ok := last.(*tfexec.OutOption)
			if !ok {
				t.Fatalf("final opt is %T, want *tfexec.OutOption", last)
			}
			if gotPath := outOptionPath(t, outOpt); gotPath != tt.wantPath {
				t.Fatalf("OutOption path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

// outOptionPath reads the unexported `path` field off a *tfexec.OutOption via
// reflection. tfexec exposes no accessor, and the whole point of the test is
// to confirm that the operator-supplied path reaches the option, so peeking
// at the field is intentional and the only way to assert without executing
// tofu.
func outOptionPath(t *testing.T, opt *tfexec.OutOption) string {
	t.Helper()
	v := reflect.ValueOf(opt).Elem().FieldByName("path")
	if !v.IsValid() {
		t.Fatalf("tfexec.OutOption no longer has a 'path' field; upstream API changed")
	}
	// path is a string field; reflection returns an unexported-string Value
	// that is safe to read via String() even without SetAccessor tricks.
	return v.String()
}
