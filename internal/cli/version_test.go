package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// writeFakeTofu writes an executable script that reports the given version
// banner, so resolution runs its real subprocess probe without a network.
func writeFakeTofu(t *testing.T, banner string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tofu shell script is not runnable on windows")
	}

	path := filepath.Join(t.TempDir(), "tofu")
	script := "#!/bin/sh\necho '" + banner + "'\n"
	// #nosec G306 -- the fake tofu script must be executable to be exec'd by the probe.
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatalf("failed to write fake tofu script: %v", err)
	}
	return path
}

func TestTofuVersionLine(t *testing.T) {
	// Neutralize the host environment so each subtest states its own inputs.
	t.Setenv(tofu.EnvTofuPath, "")
	t.Setenv("PATH", t.TempDir())

	t.Run("override hit", func(t *testing.T) {
		path := writeFakeTofu(t, "OpenTofu v"+tofu.Version)
		t.Setenv(tofu.EnvTofuPath, path)

		got := tofuVersionLine(context.Background())
		want := tofu.Version + " (from " + tofu.EnvTofuPath + ": " + path + ")"
		if got != want {
			t.Errorf("tofuVersionLine() = %q, want %q", got, want)
		}
	})

	t.Run("override hit with differing version notes the pin", func(t *testing.T) {
		path := writeFakeTofu(t, "OpenTofu v1.12.5")
		t.Setenv(tofu.EnvTofuPath, path)

		got := tofuVersionLine(context.Background())
		want := "1.12.5 (from " + tofu.EnvTofuPath + ": " + path + "; NIC is tested against " + tofu.Version + ")"
		if got != want {
			t.Errorf("tofuVersionLine() = %q, want %q", got, want)
		}
	})

	t.Run("PATH hit", func(t *testing.T) {
		path := writeFakeTofu(t, "OpenTofu v"+tofu.Version)
		t.Setenv("PATH", filepath.Dir(path))

		got := tofuVersionLine(context.Background())
		want := tofu.Version + " (from PATH: " + path + ")"
		if got != want {
			t.Errorf("tofuVersionLine() = %q, want %q", got, want)
		}
	})

	t.Run("download fallback", func(t *testing.T) {
		got := tofuVersionLine(context.Background())
		want := tofu.Version + " (downloaded by nic)"
		if got != want {
			t.Errorf("tofuVersionLine() = %q, want %q", got, want)
		}
	})

	t.Run("rejected PATH binary is explained", func(t *testing.T) {
		path := writeFakeTofu(t, "OpenTofu v1.6.0")
		t.Setenv("PATH", filepath.Dir(path))

		got := tofuVersionLine(context.Background())
		for _, part := range []string{"downloaded by nic", "1.6.0", path, "below the minimum supported version"} {
			if !strings.Contains(got, part) {
				t.Errorf("tofuVersionLine() = %q, want it to contain %q", got, part)
			}
		}
	})

	t.Run("resolution error is reported inline", func(t *testing.T) {
		t.Setenv(tofu.EnvTofuPath, "/does/not/exist")

		got := tofuVersionLine(context.Background())
		if !strings.HasPrefix(got, "unresolved (") || !strings.Contains(got, "/does/not/exist") {
			t.Errorf("tofuVersionLine() = %q, want unresolved(...) naming the bad path", got)
		}
	})
}
