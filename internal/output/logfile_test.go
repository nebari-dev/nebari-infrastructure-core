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
