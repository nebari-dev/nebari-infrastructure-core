package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/fingerprint"
)

// TestVersionOutputMatchesRecordedBuild is the drift guard #386 asks for: the
// values NIC writes into a cluster must be the ones `nic version` prints.
//
// It captures runVersion's real stdout and compares the parsed lines against
// buildInfo(), the struct Deploy records. An earlier version of this test built
// both sides from the same three vars and so proved nothing - rewriting
// buildInfo() to return garbage left it green. This one fails on exactly that
// mutation, which is the point.
func TestVersionOutputMatchesRecordedBuild(t *testing.T) {
	printed := captureVersionOutput(t)
	recorded := fingerprint.Info{Build: buildInfo()}.Data()

	// Map the "nic version" label to the ConfigMap key carrying the same fact.
	for label, key := range map[string]string{
		"Version": "nic-version",
		"Commit":  "nic-commit",
		"Built":   "nic-build-date",
	} {
		got, ok := printed[label]
		if !ok {
			t.Errorf("`nic version` printed no %q line; the drift guard cannot see that field", label)
			continue
		}
		if got != recorded[key] {
			t.Errorf("`nic version` prints %s = %q but the cluster records %s = %q", label, got, key, recorded[key])
		}
	}
}

// captureVersionOutput runs the version command and returns its "Label: value"
// lines. runVersion writes to os.Stdout directly rather than cmd.OutOrStdout(),
// so the pipe has to be installed around the call.
func captureVersionOutput(t *testing.T) map[string]string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	runErr := runVersion(cmd, nil)

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if runErr != nil {
		t.Fatalf("runVersion() error = %v", runErr)
	}

	fields := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		label, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields[strings.TrimSpace(label)] = strings.TrimSpace(value)
	}
	return fields
}
