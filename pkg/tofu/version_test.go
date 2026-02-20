package tofu

import (
	"strings"
	"testing"
)

func TestTofuVersion(t *testing.T) {
	if TofuVersion == "" {
		t.Error("TofuVersion should not be empty")
	}

	parts := strings.Split(TofuVersion, ".")
	if len(parts) < 2 {
		t.Errorf("TofuVersion = %v, expected semver format (x.y.z)", TofuVersion)
	}
}
