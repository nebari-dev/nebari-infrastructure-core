package hetzner

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

func (f fakeFileInfo) Name() string       { return binaryName }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func TestResolve(t *testing.T) {
	execInfo := fakeFileInfo{mode: 0755}

	tests := []struct {
		name string
		// env maps NIC_HETZNER_K3S_PATH to its value ("" means unset).
		env string
		// pathHit is the path lookPath returns for "hetzner-k3s"; "" means not on PATH.
		pathHit string
		// statErr is returned by stat for the override path.
		statErr error
		// statInfo is returned by stat when statErr is nil.
		statInfo os.FileInfo

		want        *resolvedBinary
		wantErr     string
		skipWindows bool
	}{
		{
			name:     "NIC_HETZNER_K3S_PATH hit",
			env:      "/opt/hetzner-k3s/bin/hetzner-k3s",
			statInfo: execInfo,
			want:     &resolvedBinary{Path: "/opt/hetzner-k3s/bin/hetzner-k3s", Source: sourceOverride},
		},
		{
			name:    "NIC_HETZNER_K3S_PATH pointing at missing file",
			env:     "/does/not/exist",
			statErr: os.ErrNotExist,
			wantErr: "NIC_HETZNER_K3S_PATH points to /does/not/exist",
		},
		{
			name:        "NIC_HETZNER_K3S_PATH pointing at non-executable file",
			env:         "/opt/hetzner-k3s/README",
			statInfo:    fakeFileInfo{mode: 0644},
			wantErr:     "not executable",
			skipWindows: true,
		},
		{
			name:     "NIC_HETZNER_K3S_PATH pointing at directory",
			env:      "/opt/hetzner-k3s",
			statInfo: fakeFileInfo{mode: os.ModeDir | 0755, dir: true},
			wantErr:  "is a directory",
		},
		{
			name:    "PATH discovery hit",
			pathHit: "/usr/local/bin/hetzner-k3s",
			want:    &resolvedBinary{Path: "/usr/local/bin/hetzner-k3s", Source: sourcePath},
		},
		{
			name: "neither override nor PATH binary falls back to download",
			want: nil,
		},
		{
			name:     "NIC_HETZNER_K3S_PATH takes precedence over PATH",
			env:      "/opt/hetzner-k3s/bin/hetzner-k3s",
			pathHit:  "/usr/local/bin/hetzner-k3s",
			statInfo: execInfo,
			want:     &resolvedBinary{Path: "/opt/hetzner-k3s/bin/hetzner-k3s", Source: sourceOverride},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipWindows && runtime.GOOS == "windows" {
				t.Skip("executable-bit check does not apply on windows")
			}

			r := &resolver{
				getenv: func(key string) string {
					if key == EnvHetznerK3sPath {
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
