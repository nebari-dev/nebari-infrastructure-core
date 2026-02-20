package tofu

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version should not be empty")
	}

	parts := strings.Split(Version, ".")
	if len(parts) < 2 {
		t.Errorf("Version = %v, expected semver format (x.y.z)", Version)
	}
}
