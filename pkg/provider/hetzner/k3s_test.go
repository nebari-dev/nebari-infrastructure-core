package hetzner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createFakeHetznerK3s creates a shell script that mimics `hetzner-k3s releases`.
func createFakeHetznerK3s(t *testing.T, releases []string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "hetzner-k3s")
	content := "#!/bin/sh\ncat <<'RELEASES'\n" + strings.Join(releases, "\n") + "\nRELEASES\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil { //nolint:gosec // Test helper script needs execute permission
		t.Fatal(err)
	}
	return script
}

func TestResolveK3sVersion(t *testing.T) {
	releases := []string{
		"v1.32.12+k3s1",
		"v1.32.12-rc1+k3s1",
		"v1.32.11+k3s3",
		"v1.31.5+k3s1",
	}
	binary := createFakeHetznerK3s(t, releases)

	tests := []struct {
		name    string
		version string
		want    string
		wantErr bool
	}{
		{
			name:    "resolves minor version to latest patch",
			version: "1.32",
			want:    "v1.32.12+k3s1",
		},
		{
			name:    "resolves minor.patch version",
			version: "1.32.11",
			want:    "v1.32.11+k3s3",
		},
		{
			name:    "explicit k3s version passes through",
			version: "v1.32.0+k3s1",
			want:    "v1.32.0+k3s1",
		},
		{
			name:    "skips prerelease",
			version: "1.32.12",
			want:    "v1.32.12+k3s1",
		},
		{
			name:    "no matching version",
			version: "1.99",
			wantErr: true,
		},
		{
			name:    "invalid format",
			version: "1",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveK3sVersion(context.Background(), tt.version, binary)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveK3sVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("resolveK3sVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveK3sVersion_BinaryError(t *testing.T) {
	_, err := resolveK3sVersion(context.Background(), "1.32", "/nonexistent/hetzner-k3s")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "failed to get hetzner-k3s releases") {
		t.Errorf("error should mention hetzner-k3s releases, got: %v", err)
	}
}
