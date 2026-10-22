package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunDeploy_PlanOutRequiresDryRun checks that --plan-out is rejected
// without --dry-run before any config is read: a saved plan only makes sense
// for a preview, and failing early keeps a mistyped invocation off the apply
// path.
func TestRunDeploy_PlanOutRequiresDryRun(t *testing.T) {
	deployPlanOut = "plan.txt"
	deployDryRun = false
	t.Cleanup(func() { deployPlanOut = "" })

	err := runDeploy(newTestCommand(t), nil)
	if err == nil {
		t.Fatal("runDeploy returned nil, want an error")
	}
	if !strings.Contains(err.Error(), "--plan-out requires --dry-run") {
		t.Errorf("error %q does not mention --plan-out requires --dry-run", err)
	}
}

// TestCheckPlanOutWritable covers the early --plan-out path check that keeps a
// mistyped directory from throwing away a finished plan.
func TestCheckPlanOutWritable(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("old plan"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "new file in existing directory", path: filepath.Join(dir, "plan.txt")},
		{name: "existing file", path: existing},
		{name: "missing directory", path: filepath.Join(dir, "missing", "plan.txt"), wantErr: "directory"},
		{name: "parent is a file", path: filepath.Join(file, "plan.txt"), wantErr: "not a directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPlanOutWritable(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkPlanOutWritable(%q) unexpected error: %v", tt.path, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkPlanOutWritable(%q) expected error, got nil", tt.path)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}

	// A file the probe created must not be left behind.
	if _, err := os.Stat(filepath.Join(dir, "plan.txt")); !os.IsNotExist(err) {
		t.Errorf("probe left %s behind (stat err = %v)", filepath.Join(dir, "plan.txt"), err)
	}

	// The check must not truncate an existing plan: the old content stays until
	// the new plan replaces it.
	got, err := os.ReadFile(existing) //nolint:gosec // test-owned temp path
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old plan" {
		t.Errorf("existing file content = %q, want %q", got, "old plan")
	}
}
