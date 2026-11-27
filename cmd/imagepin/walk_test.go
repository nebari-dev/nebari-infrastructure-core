package main

import "testing"

func TestSplitRef(t *testing.T) {
	const d = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	tests := []struct {
		ref, name, tag, digest string
	}{
		{"busybox", "busybox", "", ""},
		{"busybox:1.37", "busybox", "1.37", ""},
		{"ghcr.io/org/app:1.2.3@" + d, "ghcr.io/org/app", "1.2.3", d},
		{"ghcr.io/org/app@" + d, "ghcr.io/org/app", "", d},
		// A ':' before the last '/' is a registry port, not a tag.
		{"localhost:5000/app", "localhost:5000/app", "", ""},
		{"localhost:5000/app:1.0", "localhost:5000/app", "1.0", ""},
		{"localhost:5000/org/app:1.0@" + d, "localhost:5000/org/app", "1.0", d},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, tag, digest := splitRef(tt.ref)
			if name != tt.name || tag != tt.tag || digest != tt.digest {
				t.Errorf("splitRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.ref, name, tag, digest, tt.name, tt.tag, tt.digest)
			}
		})
	}
}
