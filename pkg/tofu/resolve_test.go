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

		want        *ResolvedBinary
		wantErr     string
		skipWindows bool
	}{
		{
			name:     "NIC_TOFU_PATH hit",
			env:      "/opt/tofu/bin/tofu",
			statInfo: execInfo,
			version:  Version,
			want:     &ResolvedBinary{Path: "/opt/tofu/bin/tofu", Version: Version, Source: SourceOverride},
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
			name:    "PATH binary below version floor falls back to download",
			pathHit: "/usr/local/bin/tofu",
			version: "1.6.0",
			want:    nil,
		},
		{
			name:       "PATH binary version probe failure falls back to download",
			pathHit:    "/usr/local/bin/tofu",
			versionErr: errors.New("exec format error"),
			want:       nil,
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
					if tt.statErr != nil {
						return nil, tt.statErr
					}
					return tt.statInfo, nil
				},
				binaryVersion: func(ctx context.Context, path string) (string, error) {
					return tt.version, tt.versionErr
				},
			}

			got, err := r.resolve(context.Background())

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

			if tt.want == nil {
				if got != nil {
					t.Fatalf("resolve() = %+v, want nil (download fallback)", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("resolve() = nil, want %+v", tt.want)
			}
			if *got != *tt.want {
				t.Errorf("resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCompatibleVersion(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		{version: MinVersion, wantErr: false},
		{version: "1.11.2", wantErr: true},
		{version: "1.12.5", wantErr: false},
		{version: "1.99.0", wantErr: false},
		{version: "2.0.0", wantErr: true},
		{version: "2.1.0", wantErr: true},
		{version: "0.9.0", wantErr: true},
		{version: "not-a-version", wantErr: true},
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

	t.Run("parses version json", func(t *testing.T) {
		path := writeScript(t, `echo '{"terraform_version":"1.12.5","platform":"linux_amd64"}'`)

		got, err := queryBinaryVersion(context.Background(), path)
		if err != nil {
			t.Fatalf("queryBinaryVersion() error = %v", err)
		}
		if got != "1.12.5" {
			t.Errorf("queryBinaryVersion() = %q, want %q", got, "1.12.5")
		}
	})

	t.Run("errors on non-json output", func(t *testing.T) {
		path := writeScript(t, `echo 'OpenTofu v1.12.5'`)

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want parse error")
		}
	})

	t.Run("errors on missing version key", func(t *testing.T) {
		path := writeScript(t, `echo '{}'`)

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want missing-version error")
		}
	})

	t.Run("errors on command failure", func(t *testing.T) {
		path := writeScript(t, `exit 1`)

		_, err := queryBinaryVersion(context.Background(), path)
		if err == nil {
			t.Fatal("queryBinaryVersion() error = nil, want exec error")
		}
	})
}
