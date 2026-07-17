package git

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// TestSecurityDependencyFloors guards minimum versions of dependencies whose
// security fixes either cannot be exercised faithfully through NIC's own code
// paths or are covered deterministically by a version floor:
//   - github.com/go-git/go-git/v5 >= v5.19.1 covers BOTH go-git CVEs:
//   - CVE-2026-41506 (GHSA-3xc5-wrhm-f963, fixed v5.18.0): cross-host
//     redirect credential leak. A behavioral test cannot distinguish patched
//     from vulnerable because Go net/http already strips Authorization on
//     cross-host redirects for NIC's single-request auth check; the go-git
//     fix only affects a second request during a full clone. Guarded by this
//     floor plus the govulncheck gate.
//   - CVE-2026-45571 (GHSA-crhj-59gh-8x96, fixed v5.19.1): crafted-repo path
//     validation; exploit vectors are platform-specific, so a portable
//     behavioral test is unreliable.
//   - helm.sh/helm/v3 >= v3.20.2 (CVE-2026-35206, GHSA-hr2v-4r36-88hr): the
//     `helm pull --untar` chart-name traversal bug. NIC uses LocateChart +
//     loader.Load, not `pull --untar`, so it is not on the vulnerable path.
//
// Colocated in one table because go.mod is module-global.
func TestSecurityDependencyFloors(t *testing.T) {
	tests := []struct {
		module   string
		minFixed string
	}{
		{module: "github.com/go-git/go-git/v5", minFixed: "v5.19.1"},
		{module: "helm.sh/helm/v3", minFixed: "v3.20.2"},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			got := moduleVersion(t, tt.module)
			if semver.Compare(got, tt.minFixed) < 0 {
				t.Fatalf("%s is %s, below the security floor %s; do not downgrade", tt.module, got, tt.minFixed)
			}
		})
	}
}

// moduleVersion returns the required version of module from the module-root
// go.mod, walking up from the test's working directory to locate it.
func moduleVersion(t *testing.T, module string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "go.mod")
		if b, err := os.ReadFile(p); err == nil { //nolint:gosec // G304: walks up from the test's own working directory to find the module-root go.mod, not user input
			f, err := modfile.Parse(p, b, nil)
			if err != nil {
				t.Fatalf("parse %s: %v", p, err)
			}
			for _, r := range f.Require {
				if r.Mod.Path == module {
					return r.Mod.Version
				}
			}
			t.Fatalf("module %s not found in %s", module, p)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from working directory")
		}
		dir = parent
	}
}
