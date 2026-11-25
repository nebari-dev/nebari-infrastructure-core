package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteQuickstartConfig(t *testing.T) {
	const rendered = "project_name: nebari-quickstart\n"

	tests := []struct {
		name        string
		existing    *string
		force       bool
		wantErr     string
		wantContent string
	}{
		{name: "missing file is written", wantContent: rendered},
		{name: "identical file is left alone", existing: strPtr(rendered), wantContent: rendered},
		{name: "different file is refused without force", existing: strPtr("project_name: mine\n"), wantErr: "already exists with different content", wantContent: "project_name: mine\n"},
		{name: "different file is replaced with force", existing: strPtr("project_name: mine\n"), force: true, wantContent: rendered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if tt.existing != nil {
				if err := os.WriteFile(path, []byte(*tt.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			err := writeQuickstartConfig(path, []byte(rendered), tt.force)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("writeQuickstartConfig() error = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("writeQuickstartConfig() error = %v, want one containing %q", err, tt.wantErr)
			}

			got, err := os.ReadFile(path) //nolint:gosec // test-owned temp path
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantContent {
				t.Errorf("file content = %q, want %q", got, tt.wantContent)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
