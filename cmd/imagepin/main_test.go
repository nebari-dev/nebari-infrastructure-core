package main

import "testing"

func TestPinStatus(t *testing.T) {
	const d = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	tests := []struct {
		ref, want string
	}{
		{"ghcr.io/org/app:1.2.3", statusTag},
		{"ghcr.io/org/app:1.2.3@" + d, statusDigest},
		{"ghcr.io/org/app@" + d, statusDigest},
		{"ghcr.io/org/app:latest", statusLatest},
		{"ghcr.io/org/app", statusUntagged},
		{"busybox", statusUntagged},
		{"busybox:1.37", statusTag},
		// A ':' before the last '/' is a registry port, not a tag.
		{"localhost:5000/app", statusUntagged},
		{"localhost:5000/app:1.0", statusTag},
		{"localhost:5000/app:latest", statusLatest},
		// A tag that merely starts with "latest" is a fixed tag.
		{"ghcr.io/org/app:latest-alpine", statusTag},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := pinStatus(tt.ref); got != tt.want {
				t.Errorf("pinStatus(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}
