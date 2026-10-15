package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogFilePath(t *testing.T) {
	start := time.Date(2026, 10, 14, 10, 0, 38, 0, time.FixedZone("BRT", -3*3600))
	got := LogFilePath("/home/u", "acme-nebari", "deploy", start)
	want := filepath.Join("/home/u", ".nic", "logs", "acme-nebari", "20261014T130038Z-deploy.jsonl")
	if got != want {
		t.Errorf("LogFilePath() = %q, want %q", got, want)
	}
}

func TestOpenLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "acme", "run.jsonl")

	f, err := OpenLogFile(path)
	if err != nil {
		t.Fatalf("OpenLogFile() error = %v", err)
	}
	if _, err := f.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want os.FileMode
	}{
		{path: path, want: 0o600},
		{path: filepath.Dir(path), want: 0o700 | os.ModeDir},
	}
	for _, tt := range tests {
		info, err := os.Stat(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode() != tt.want {
			t.Errorf("mode of %s = %v, want %v", tt.path, info.Mode(), tt.want)
		}
	}
}

// TestOpenLogFileExisting: a log file that already exists (a --log-file the
// user picked) is still made readable by its owner only.
func TestOpenLogFileExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil { //nolint:gosec // the world-readable mode is what the test expects OpenLogFile to tighten
		t.Fatal(err)
	}
	f, err := OpenLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}
