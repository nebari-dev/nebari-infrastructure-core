package hetzner

import (
	"context"
	"testing"
)

func TestResolveK3sVersion(t *testing.T) {
	// Releases are oldest-first, matching real `hetzner-k3s releases` output.
	// The function iterates in reverse to find the newest match.
	releases := []string{
		"v1.31.5+k3s1",
		"v1.32.11+k3s3",
		"v1.32.12-rc1+k3s1",
		"v1.32.12+k3s1",
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
	// Releases are oldest-first. The function iterates in reverse,
	// so it will hit the pre-releases first and skip them.
	releases := []string{
		"v1.32.11+k3s1", // oldest stable
		"v1.32.12-alpha1+k3s1",
		"v1.32.12-beta1+k3s1",
		"v1.32.12-rc1+k3s1",
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
		// Ensure loose strings don't match (e.g., a hypothetical tag with "rc" elsewhere)
		{"v1.32.12+k3s1-rc", false}, // rc after +k3s should not match
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := isPrerelease(tt.tag); got != tt.want {
				t.Errorf("isPrerelease(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestK3sVersionPattern(t *testing.T) {
	tests := []struct {
		line  string
		valid bool
	}{
		// Valid release tags
		{"v1.32.12+k3s1", true},
		{"v1.32.12-rc1+k3s1", true},
		{"v1.32.12-alpha1+k3s1", true},
		{"v1.32.12-beta+k3s1", true},
		{"v0.1.0-rc1+k3s1", true},
		{"v1.35.3+k3s2", true},

		// Invalid - banner and header lines from hetzner-k3s output
		{"╭─────────────────────────────────────────╮", false},
		{"│            hetzner-k3s                  │", false},
		{"│   Production-ready K8s on Hetzner       │", false},
		{"╰─────────────────────────────────────────╯", false},
		{"Version: 2.4.6", false},
		{"Available k3s releases:", false},
		{"", false},

		// Invalid - malformed versions
		{"1.32.12+k3s1", false},      // missing v prefix
		{"v1.32+k3s1", false},        // missing patch
		{"v1.32.12", false},          // missing +k3s suffix
		{"v1.32.12+k3s", false},      // missing k3s number
		{"v1.32.12-foo+k3s1", false}, // unknown pre-release type
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := k3sVersionPattern.MatchString(tt.line); got != tt.valid {
				t.Errorf("k3sVersionPattern.MatchString(%q) = %v, want %v", tt.line, got, tt.valid)
			}
		})
	}
}

func TestExtractAvailableMinors(t *testing.T) {
	releases := []string{
		"v1.31.5+k3s1",
		"v1.32.11+k3s3",
		"v1.32.12-rc1+k3s1", // pre-release, should be skipped
		"v1.32.12+k3s1",
		"v1.33.1+k3s1",
	}

	got := extractAvailableMinors(releases)
	want := []string{"1.31", "1.32", "1.33"}

	if len(got) != len(want) {
		t.Fatalf("extractAvailableMinors() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("extractAvailableMinors()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
