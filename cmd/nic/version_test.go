package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	out := versionString("v9.9.9", "deadbeef", "2020-01-01T00:00:00Z", "1.2.3")

	wants := []string{
		"Nebari Infrastructure Core (NIC)",
		"Version: v9.9.9",
		"Commit: deadbeef",
		"Built: 2020-01-01T00:00:00Z",
		"OpenTofu version: 1.2.3",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("versionString() output missing %q\ngot:\n%s", want, out)
		}
	}

	if !strings.HasSuffix(out, "\n") {
		t.Errorf("versionString() should end with a newline, got: %q", out)
	}
}
