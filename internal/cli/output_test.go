package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// TestCommandOutputLogFile runs a short status stream through
// startCommandOutput and reads back what reached the log file.
func TestCommandOutputLogFile(t *testing.T) {
	tests := []struct {
		name      string
		project   string
		flags     logFileFlags
		err       error
		wantFile  bool
		wantFinal string
	}{
		{name: "default path under ~/.nic/logs", project: "acme", wantFile: true, wantFinal: "Command completed"},
		{name: "failure is recorded", project: "acme", err: errors.New("boom"), wantFile: true, wantFinal: "Command execution failed"},
		{name: "disabled", project: "acme", flags: logFileFlags{disabled: true}},
		{name: "unvalidated project name never escapes the log directory", project: "../../escaped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			outputFormat = output.FormatPlain
			t.Cleanup(func() { outputFormat = "" })

			cfg := &config.NebariConfig{ProjectName: tt.project, Cluster: &config.ClusterConfig{}}
			ctx, out := startCommandOutput(context.Background(), "deploy", cfg, output.Header{}, tt.flags, false)
			status.Phase(ctx, "Infrastructure")
			status.Success(ctx, "Infrastructure deployment completed")
			out.finish(tt.err)

			files, _ := filepath.Glob(filepath.Join(home, ".nic", "logs", "*", "*-deploy.jsonl"))
			escaped, _ := filepath.Glob(filepath.Join(home, "escaped", "*")) // where ../../escaped would land
			if len(escaped) > 0 {
				t.Fatalf("log file written outside ~/.nic/logs: %v", escaped)
			}
			if !tt.wantFile {
				if len(files) != 0 {
					t.Fatalf("want no log file, got %v", files)
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("want one log file, got %v", files)
			}
			info, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("log file mode = %v, want 0600", info.Mode().Perm())
			}

			var msgs []string
			f, err := os.Open(files[0])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var rec map[string]any
				if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
					t.Fatalf("log line is not JSON: %q", sc.Text())
				}
				msgs = append(msgs, rec["msg"].(string))
			}
			want := []string{"Infrastructure", "Infrastructure deployment completed", tt.wantFinal}
			if strings.Join(msgs, "|") != strings.Join(want, "|") {
				t.Errorf("log records = %q, want %q", msgs, want)
			}
		})
	}
}
