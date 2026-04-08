package hetzner

import (
	"context"
	"testing"
)

func TestResolveK3sVersion(t *testing.T) {
	releases := []string{
		"v1.32.12+k3s1",
		"v1.32.12-rc1+k3s1",
		"v1.32.11+k3s3",
		"v1.31.5+k3s1",
	}

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
			name:    "skips prerelease rc",
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
			got, err := resolveK3sVersion(context.Background(), tt.version, releases)
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

func TestResolveK3sVersion_SkipsAllPrereleaseTypes(t *testing.T) {
	releases := []string{
		"v1.32.12-alpha1+k3s1",
		"v1.32.12-beta1+k3s1",
		"v1.32.12-rc1+k3s1",
		"v1.32.11+k3s1", // first stable
	}

	got, err := resolveK3sVersion(context.Background(), "1.32", releases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v1.32.11+k3s1" {
		t.Errorf("resolveK3sVersion() = %q, want %q (should skip alpha, beta, rc)", got, "v1.32.11+k3s1")
	}
}

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"v1.32.12+k3s1", false},
		{"v1.32.12-rc1+k3s1", true},
		{"v1.32.12-alpha1+k3s1", true},
		{"v1.32.12-beta1+k3s1", true},
		{"v1.32.12-rc+k3s1", true},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := isPrerelease(tt.tag); got != tt.want {
				t.Errorf("isPrerelease(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}
