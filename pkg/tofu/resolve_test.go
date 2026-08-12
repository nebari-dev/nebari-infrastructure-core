package tofu

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// fakeFileInfo implements os.FileInfo for stat mocks.
type fakeFileInfo struct {
	mode os.FileMode
	dir  bool
}

func (f fakeFileInfo) Name() string       { return "tofu" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func TestResolve(t *testing.T) {
	execInfo := fakeFileInfo{mode: 0755}

	tests := []struct {
		name string
		// env maps NIC_TOFU_PATH to its value ("" means unset).
		env string
		// pathHit is the path lookPath returns for "tofu"; "" means not on PATH.
		pathHit string
		// statErr is returned by stat for the override path.
		statErr error
		// statInfo is returned by stat when statErr is nil.
		statInfo os.FileInfo
		// version and versionErr are returned by the binary version probe.
		version    string
		versionErr error

		want *ResolvedBinary
		// wantNote is a substring expected in the resolution notes; when
		// empty, no notes are expected.
		wantNote    string
		wantErr     string
		skipWindows bool
	}{
		{
			// This row deliberately pins compatibleVersion(Version): it feeds
			// NIC's own pinned download Version through the external-binary
			// compatibility check. If a Version bump ever falls outside
			// [MinVersion, 2.0.0), this row fails, and that failure is a
			// prompt to revisit the floor deliberately, not an accident.
			name:     "NIC_TOFU_PATH hit",
			env:      "/opt/tofu/bin/tofu",
			statInfo: execInfo,
			version:  Version,
			want:     &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: Version, Source: SourceOverride},
		},
		{
			name:     "NIC_TOFU_PATH with surrounding whitespace is trimmed",
			env:      "  /opt/tofu/bin/tofu\n",
			statInfo: execInfo,
			version:  Version,
			want:     &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: Version, Source: SourceOverride},
		},
		{
			name:    "whitespace-only NIC_TOFU_PATH is treated as unset",
			env:     "   ",
			pathHit: "/usr/local/bin/tofu",
			version: "1.12.5",
			want:    &ResolvedBinary{Path: "/usr/local/bin/tofu", Version: "1.12.5", Source: SourcePath},
		},
		{
			name:     "NIC_TOFU_PATH hit with newer compatible version",
			env:      "/opt/tofu/bin/tofu",
			statInfo: execInfo,
			version:  "1.12.5",
			want:     &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: "1.12.5", Source: SourceOverride},
		},
		{
			name:    "NIC_TOFU_PATH pointing at missing file",
			env:     "/does/not/exist",
			statErr: os.ErrNotExist,
			wantErr: "NIC_TOFU_PATH points to /does/not/exist",
		},
		{
			name:        "NIC_TOFU_PATH pointing at non-executable file",
			env:         "/opt/tofu/README",
			statInfo:    fakeFileInfo{mode: 0644},
			wantErr:     "not executable",
			skipWindows: true,
		},
		{
			name:     "NIC_TOFU_PATH pointing at directory",
			env:      "/opt/tofu",
			statInfo: fakeFileInfo{mode: os.ModeDir | 0755, dir: true},
			wantErr:  "is a directory",
		},
		{
			name:     "NIC_TOFU_PATH below version floor",
			env:      "/opt/tofu/bin/tofu",
			statInfo: execInfo,
			version:  "1.6.0",
			wantErr:  "below the minimum supported version",
		},
		{
			name:     "NIC_TOFU_PATH above supported major version",
			env:      "/opt/tofu/bin/tofu",
			statInfo: execInfo,
			version:  "2.0.0",
			wantErr:  "must be below 2.0.0",
		},
		{
			name:       "NIC_TOFU_PATH version probe failure",
			env:        "/opt/tofu/bin/tofu",
			statInfo:   execInfo,
			versionErr: errors.New("exec format error"),
			wantErr:    "failed to determine version",
		},
		{
			name:    "PATH discovery hit",
			pathHit: "/usr/local/bin/tofu",
			version: "1.12.5",
			want:    &ResolvedBinary{Path: "/usr/local/bin/tofu", Version: "1.12.5", Source: SourcePath},
		},
		{
			name:     "PATH binary below version floor falls back to download",
			pathHit:  "/usr/local/bin/tofu",
			version:  "1.6.0",
			want:     nil,
			wantNote: "below the minimum supported version",
		},
		{
			name:       "PATH binary version probe failure falls back to download",
			pathHit:    "/usr/local/bin/tofu",
			versionErr: errors.New("exec format error"),
			want:       nil,
			wantNote:   "failed to determine its version",
		},
		{
			name: "neither override nor PATH binary falls back to download",
			want: nil,
		},
		{
			name:     "NIC_TOFU_PATH takes precedence over PATH",
			env:      "/opt/tofu/bin/tofu",
			pathHit:  "/usr/local/bin/tofu",
			statInfo: execInfo,
			version:  "1.12.5",
			want:     &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: "1.12.5", Source: SourceOverride},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipWindows && runtime.GOOS == "windows" {
				t.Skip("executable-bit check does not apply on windows")
			}

			// Record the paths handed to stat and the version probe so a
			// regression that validates one binary but resolves another is
			// caught, not silently absorbed by path-agnostic mocks.
			var statPaths, versionPaths []string
			r := &resolver{
				getenv: func(key string) string {
					if key == EnvTofuPath {
						return tt.env
					}
					return ""
				},
				lookPath: func(name string) (string, error) {
					if tt.pathHit == "" {
						return "", errors.New("executable file not found in $PATH")
					}
					return tt.pathHit, nil
				},
				stat: func(path string) (os.FileInfo, error) {
					statPaths = append(statPaths, path)
					if tt.statErr != nil {
						return nil, tt.statErr
					}
					return tt.statInfo, nil
				},
				binaryVersion: func(ctx context.Context, path string) (string, error) {
					versionPaths = append(versionPaths, path)
					return tt.version, tt.versionErr
				},
			}

			res, err := r.resolve(context.Background())

			// stat is only ever aimed at the (trimmed) override; the version
			// probe targets the override when set, otherwise the PATH hit.
			override := strings.TrimSpace(tt.env)
			for _, p := range statPaths {
				if p != override {
					t.Errorf("stat called with %q, want %q", p, override)
				}
			}
			wantProbe := override
			if wantProbe == "" {
				wantProbe = tt.pathHit
			}
			for _, p := range versionPaths {
				if p != wantProbe {
					t.Errorf("binaryVersion called with %q, want %q", p, wantProbe)
				}
			}

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("resolve() error = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolve() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve() error = %v", err)
			}

			if tt.wantNote == "" {
				if len(res.Notes) != 0 {
					t.Errorf("resolve() notes = %q, want none", res.Notes)
				}
			} else if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], tt.wantNote) {
				t.Errorf("resolve() notes = %q, want one note containing %q", res.Notes, tt.wantNote)
			}

			if tt.want == nil {
				if res.Binary != nil {
					t.Fatalf("resolve() binary = %+v, want nil (download fallback)", res.Binary)
				}
				return
			}
			if res.Binary == nil {
				t.Fatalf("resolve() binary = nil, want %+v", tt.want)
			}
			if *res.Binary != *tt.want {
				t.Errorf("resolve() binary = %+v, want %+v", res.Binary, tt.want)
			}
		})
	}
}

// TestAnnounce covers both announcement branches: an external version matching
// the pin is a plain notice, a differing in-range version names the pin. Both
// are info-level so routine pixi/distro version skew does not spam warnings.
func TestAnnounce(t *testing.T) {
	capture := func(t *testing.T, b *ResolvedBinary) []status.Update {
		t.Helper()
		var updates []status.Update
		ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
			updates = append(updates, u)
		})
		b.announce(ctx)
		cleanup()
		return updates
	}

	t.Run("version matching the pin", func(t *testing.T) {
		updates := capture(t, &ResolvedBinary{Path: "/usr/local/bin/tofu", Version: Version, Source: SourcePath})
		if len(updates) != 1 {
			t.Fatalf("announce() sent %d updates, want 1", len(updates))
		}
		if updates[0].Level != status.LevelInfo {
			t.Errorf("announce() level = %q, want %q", updates[0].Level, status.LevelInfo)
		}
		if strings.Contains(updates[0].Message, "tested against") {
			t.Errorf("announce() message = %q, want no tested-against note for the pinned version", updates[0].Message)
		}
	})

	t.Run("version differing from the pin", func(t *testing.T) {
		updates := capture(t, &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: "1.12.5", Source: SourceOverride})
		if len(updates) != 1 {
			t.Fatalf("announce() sent %d updates, want 1", len(updates))
		}
		if updates[0].Level != status.LevelInfo {
			t.Errorf("announce() level = %q, want %q", updates[0].Level, status.LevelInfo)
		}
		wantParts := []string{"1.12.5", EnvTofuPath, "NIC is tested against " + Version}
		for _, part := range wantParts {
			if !strings.Contains(updates[0].Message, part) {
				t.Errorf("announce() message = %q, want it to contain %q", updates[0].Message, part)
			}
		}
	})
}

func TestCompatibleVersion(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		// Pins compatibleVersion(Version): NIC's own pinned download version
		// must always sit inside the supported range.
		{version: Version, wantErr: false},
		{version: MinVersion, wantErr: false},
		{version: "1.11.2", wantErr: true},
		{version: "1.12.5", wantErr: false},
		{version: "1.99.0", wantErr: false},
		{version: "2.0.0", wantErr: true},
		{version: "2.1.0", wantErr: true},
		{version: "0.9.0", wantErr: true},
		{version: "not-a-version", wantErr: true},
		// Pre-releases are compared by their base version (see compatibleVersion):
		// an rc of the floor is accepted, a preview of 2.0 is rejected.
		{version: "1.11.3-rc1", wantErr: false},
		{version: "1.12.0-beta1", wantErr: false},
		{version: "2.0.0-beta1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			err := compatibleVersion(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("compatibleVersion(%q) error = %v, wantErr %v", tt.version, err, tt.wantErr)
			}
		})
	}
}

// TestQueryBinaryVersion exercises the real subprocess probe against a fake
// tofu script, keeping the test network-free.
func TestQueryBinaryVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake tofu shell script is not runnable on windows")
	}

	writeScript := func(t *testing.T, body string) string {
		t.Helper()
		path := t.TempDir() + "/tofu"
		script := "#!/bin/sh\n" + body + "\n"
		// #nosec G306 -- the fake tofu script must be executable to be exec'd by the probe.
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatalf("failed to write fake tofu script: %v", err)
		}
		return path
	}

	t.Run("parses version banner", func(t *testing.T) {
		path := writeScript(t, "echo 'OpenTofu v1.12.5'\necho 'on linux_amd64'")

		got, err := queryBinaryVersion(context.Background(), path)
		if err != nil {
			t.Fatalf("queryBinaryVersion() error = %v", err)
		}
		if got != "1.12.5" {
			t.Errorf("queryBinaryVersion() = %q, want %q", got, "1.12.5")
		}
	})

	t.Run("rejects Terraform masquerading as tofu", func(t *testing.T) {
		path := writeScript(t, `echo 'Terraform v1.14.8'`)

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want Terraform rejection")
		}
		if !strings.Contains(err.Error(), "Terraform, not OpenTofu") {
			t.Errorf("queryBinaryVersion() error = %v, want mention of Terraform, not OpenTofu", err)
		}
	})

	t.Run("errors on unrecognized output", func(t *testing.T) {
		path := writeScript(t, `echo 'definitely not tofu'`)

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want identification error")
		}
	})

	t.Run("errors on command failure and includes stderr", func(t *testing.T) {
		path := writeScript(t, "echo 'linker error: missing libfoo' >&2\nexit 1")

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want exec error")
		}
		if !strings.Contains(err.Error(), "missing libfoo") {
			t.Errorf("queryBinaryVersion() error = %v, want stderr content included", err)
		}
	})
}
