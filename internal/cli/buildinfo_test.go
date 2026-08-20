package cli

import (
	"embed"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/fingerprint"
)

// clientConstructors embeds the sources of every command that builds a
// nic.Client. Embedding rather than reading from disk makes a renamed or deleted
// command a compile error here, instead of a test that quietly stops checking
// the file it was pointed at.
//
//go:embed deploy.go destroy.go validate.go kubeconfig.go version.go
var clientConstructors embed.FS

// TestBuildOptionMatchesVersionOutput is the drift guard required by #386: the
// values NIC records in the cluster must be the same ones `nic version` prints.
//
// Both sides read the package-level version/commit/date vars that -ldflags -X
// writes, and this test pins that they stay wired to the same source. It fails
// if someone changes runVersion to print from somewhere else, or gives
// buildOption its own values - either of which would make a cluster's recorded
// provenance disagree with the binary that produced it, which is precisely the
// confusion this feature exists to remove.
//
// The vars are their unset defaults under `go test` (no ldflags), which is fine:
// the assertion is that the two paths agree, not what the values are.
func TestBuildOptionMatchesVersionOutput(t *testing.T) {
	// What buildOption hands to pkg/nic, rendered the way it reaches the cluster.
	data := fingerprint.Info{
		Build: fingerprint.Build{Version: version, Commit: commit, Date: date},
	}.Data()

	// What `nic version` prints, keyed by the ConfigMap field it corresponds to.
	printed := map[string]string{
		"nic-version":    version,
		"nic-commit":     commit,
		"nic-build-date": date,
	}

	for key, want := range printed {
		if got := data[key]; got != want {
			t.Errorf("ConfigMap %q = %q, but `nic version` reports %q", key, got, want)
		}
	}
}

// TestBuildOptionIsTheOnlyClientConstructor pins that every command builds its
// client through buildOption. A NewClient(ctx) with no option silently disables
// the metadata write for that command, which is the failure mode the single
// helper exists to prevent - and it would not show up in any behavioural test,
// because a missing ConfigMap looks the same as a cluster deployed by an older
// NIC.
func TestBuildOptionIsTheOnlyClientConstructor(t *testing.T) {
	entries, err := clientConstructors.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no sources embedded; the go:embed pattern stopped matching")
	}

	for _, entry := range entries {
		src, err := clientConstructors.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(src), "nic.NewClient(ctx)") {
			t.Errorf("%s calls nic.NewClient(ctx) without buildOption(); the build identity would be dropped", entry.Name())
		}
	}
}
