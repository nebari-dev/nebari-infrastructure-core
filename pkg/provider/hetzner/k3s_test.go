package hetzner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveK3sVersion(t *testing.T) {
	releases := []ghRelease{
		{TagName: "v1.32.12+k3s1", Prerelease: false},
		{TagName: "v1.32.12-rc1+k3s1", Prerelease: true},
		{TagName: "v1.32.11+k3s3", Prerelease: false},
		{TagName: "v1.31.5+k3s1", Prerelease: false},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

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
			got, err := resolveK3sVersion(context.Background(), tt.version, server.URL)
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
