package tofu

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// planTestTemplates is the smallest module that plans without a provider
// download: terraform_data is built into OpenTofu.
var planTestTemplates = fstest.MapFS{
	"templates/main.tf": &fstest.MapFile{Data: []byte(`
variable "v" { type = string }
resource "terraform_data" "x" { input = var.v }
`)},
}

// setupPlanTestExecutor returns an initialized executor over
// planTestTemplates, or skips when no tofu is on PATH.
func setupPlanTestExecutor(t *testing.T) *TerraformExecutor {
	t.Helper()
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu not on PATH")
	}
	// Keep the plugin cache and any download out of the developer's real cache.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(EnvTofuPath, "")

	ctx := context.Background()
	te, err := Setup(ctx, planTestTemplates, map[string]string{"v": "hello"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = te.Cleanup() })
	if err := te.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return te
}

func TestPlanToFile(t *testing.T) {
	te := setupPlanTestExecutor(t)
	dir := t.TempDir()

	tests := []struct {
		name        string
		path        string
		wantErr     string
		wantContent string
	}{
		{
			name: "empty path is a plain plan",
			path: "",
		},
		{
			name:        "writes the rendered plan",
			path:        filepath.Join(dir, "plan.txt"),
			wantContent: "terraform_data.x will be created",
		},
		{
			name:    "unwritable path",
			path:    filepath.Join(dir, "missing", "plan.txt"),
			wantErr: "write plan to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasChanges, err := te.PlanToFile(context.Background(), tt.path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("PlanToFile() expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanToFile() unexpected error: %v", err)
			}
			if !hasChanges {
				t.Error("PlanToFile() hasChanges = false, want true for a new resource")
			}
			if tt.path == "" {
				return
			}

			got, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read plan file: %v", err)
			}
			if !strings.Contains(string(got), tt.wantContent) {
				t.Errorf("plan file does not contain %q:\n%s", tt.wantContent, got)
			}
			if !strings.Contains(string(got), `input  = "hello"`) {
				t.Errorf("plan file lacks the field-level diff:\n%s", got)
			}

			if runtime.GOOS != windowsOS {
				info, err := os.Stat(tt.path)
				if err != nil {
					t.Fatal(err)
				}
				if mode := info.Mode().Perm(); mode != 0600 {
					t.Errorf("new plan file mode = %o, want 0600", mode)
				}
			}
		})
	}

	// The binary plan stays in the working directory so Cleanup removes it.
	if _, err := os.Stat(filepath.Join(te.workingDir, "nic.tfplan")); err != nil {
		t.Errorf("binary plan not in working dir: %v", err)
	}
}
