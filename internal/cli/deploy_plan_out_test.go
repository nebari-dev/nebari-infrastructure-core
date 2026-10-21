package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolvePlanOut covers the dry-run gate, absolute-path resolution against
// the operator's working directory, and the early-fail on a missing parent
// directory. These are the three guarantees runDeploy relies on before any
// provider API call.
func TestResolvePlanOut(t *testing.T) {
	// A directory that is guaranteed to exist across platforms so a relative
	// path's parent resolves cleanly. Each subtest that needs a non-existent
	// parent invents one under t.TempDir() instead.
	existingDir := t.TempDir()

	tests := []struct {
		name string
		// setup, when non-nil, returns the planOut value to pass. This lets a
		// case compute a path (absolute, relative, or under a temp dir) at
		// test time without hard-coding filesystem assumptions.
		setup       func(t *testing.T) string
		planOut     string // ignored when setup is non-nil
		dryRun      bool
		wantEmpty   bool
		wantAbs     bool
		wantErrSub  string
		wantMatches func(resolved string) bool
	}{
		{
			name:      "empty is a no-op even without dry-run",
			planOut:   "",
			dryRun:    false,
			wantEmpty: true,
		},
		{
			name:       "set without dry-run is rejected",
			planOut:    "/tmp/plan.tfplan",
			dryRun:     false,
			wantErrSub: "requires --dry-run",
		},
		{
			name: "absolute path with existing parent is returned as-is",
			setup: func(t *testing.T) string {
				return filepath.Join(existingDir, "plan.tfplan")
			},
			dryRun:  true,
			wantAbs: true,
			wantMatches: func(got string) bool {
				return got == filepath.Join(existingDir, "plan.tfplan")
			},
		},
		{
			name: "relative path is resolved against the operator cwd",
			// filepath.Abs("plan.tfplan") on an unknown cwd is fragile to assert
			// against exactly, so we only check that the returned path is
			// absolute. The parent of filepath.Abs("plan.tfplan") is the test
			// process's working directory, which always exists.
			planOut: "plan.tfplan",
			dryRun:  true,
			wantAbs: true,
		},
		{
			name: "missing parent directory errors early",
			setup: func(t *testing.T) string {
				return filepath.Join(existingDir, "does-not-exist", "plan.tfplan")
			},
			dryRun:     true,
			wantErrSub: "not accessible",
		},
		{
			name: "parent is a file, not a directory",
			setup: func(t *testing.T) string {
				// Point the parent at the test binary itself: a regular file that
				// definitely exists but is not a directory.
				regularFile := filepath.Join(existingDir, "parent-is-a-file")
				if err := writeFile(regularFile, "x"); err != nil {
					t.Fatalf("seed file: %v", err)
				}
				return filepath.Join(regularFile, "plan.tfplan")
			},
			dryRun: true,
			// filepath.Dir of "<file>/plan.tfplan" is just "<file>"; stat on
			// that regular file succeeds, so the "not a directory" guard is
			// what fires here (as opposed to the "not accessible" wrap, which
			// catches a missing path).
			wantErrSub: "is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planOut := tt.planOut
			if tt.setup != nil {
				planOut = tt.setup(t)
			}

			got, err := resolvePlanOut(planOut, tt.dryRun)
			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("resolvePlanOut(%q, %v) = %q, nil; want error containing %q", planOut, tt.dryRun, got, tt.wantErrSub)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("resolvePlanOut error = %v; want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePlanOut(%q, %v) unexpected error: %v", planOut, tt.dryRun, err)
			}
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("resolvePlanOut(%q, %v) = %q; want empty", planOut, tt.dryRun, got)
				}
				return
			}
			if tt.wantAbs && !filepath.IsAbs(got) {
				t.Fatalf("resolvePlanOut returned %q; want absolute path", got)
			}
			if tt.wantMatches != nil && !tt.wantMatches(got) {
				t.Fatalf("resolvePlanOut returned %q; did not match expectation", got)
			}
		})
	}
}

// writeFile is a tiny helper for the test above. 0o600 is deliberate: this
// writes a throwaway marker file under t.TempDir(), gosec flags anything
// looser as a potential leak.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
