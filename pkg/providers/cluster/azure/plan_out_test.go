package azure

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// TestAppendPlanOutOpt covers the Azure provider's path for honoring
// cluster.DeployOptions.PlanOut. When the operator passes --plan-out, Deploy
// must forward a tfexec.Out(path) option to tofu Plan; without it, Plan must
// run with no extra options. Verified via reflection on *tfexec.OutOption's
// unexported path field because tfexec exposes no accessor.
func TestAppendPlanOutOpt(t *testing.T) {
	tests := []struct {
		name     string
		existing []tfexec.PlanOption
		path     string
		wantLen  int
		wantPath string // empty means no OutOption should be present
	}{
		{
			name:    "empty path on nil slice is a no-op",
			path:    "",
			wantLen: 0,
		},
		{
			name:     "non-empty path on nil slice appends one OutOption",
			path:     "/abs/plan.tfplan",
			wantLen:  1,
			wantPath: "/abs/plan.tfplan",
		},
		{
			name:     "non-empty path appends to existing opts without disturbing them",
			existing: []tfexec.PlanOption{tfexec.Var("subscription_id=0000")},
			path:     "/abs/plan.tfplan",
			wantLen:  2,
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
// reflection. tfexec exposes no accessor, so this is the only way to assert
// that the operator-supplied path reaches the option.
func outOptionPath(t *testing.T, opt *tfexec.OutOption) string {
	t.Helper()
	v := reflect.ValueOf(opt).Elem().FieldByName("path")
	if !v.IsValid() {
		t.Fatalf("tfexec.OutOption no longer has a 'path' field; upstream API changed")
	}
	return v.String()
}
