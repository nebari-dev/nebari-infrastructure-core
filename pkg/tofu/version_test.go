package tofu

import (
	"strings"
	"testing"
)

func TestDefaultVersion(t *testing.T) {
	if DefaultVersion == "" {
		t.Error("DefaultVersion should not be empty")
	}

	parts := strings.Split(DefaultVersion, ".")
	if len(parts) < 2 {
		t.Errorf("DefaultVersion = %v, expected semver format (x.y.z)", DefaultVersion)
	}
}
