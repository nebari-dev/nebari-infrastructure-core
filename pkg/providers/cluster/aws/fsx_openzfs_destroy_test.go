package aws

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/fsx"
	fsxtypes "github.com/aws/aws-sdk-go-v2/service/fsx/types"
	"github.com/hashicorp/terraform-exec/tfexec"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// fakeFSxClient serves DescribeVolumes from pre-built pages, keyed by the
// NextToken that requests them ("" for the first page).
type fakeFSxClient struct {
	pages map[string]*fsx.DescribeVolumesOutput
	err   error
	calls int
}

func (f *fakeFSxClient) DescribeVolumes(_ context.Context, params *fsx.DescribeVolumesInput, _ ...func(*fsx.Options)) (*fsx.DescribeVolumesOutput, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	page, ok := f.pages[aws.ToString(params.NextToken)]
	if !ok {
		return &fsx.DescribeVolumesOutput{}, nil
	}
	return page, nil
}

func fsxVolume(id, name string) fsxtypes.Volume {
	return fsxtypes.Volume{VolumeId: aws.String(id), Name: aws.String(name)}
}

func TestCheckFSxOpenZFSChildVolumes(t *testing.T) {
	fs := fsxOpenZFSFilesystem{ID: "fs-123", RootVolumeID: "fsvol-root"}
	rootOnly := map[string]*fsx.DescribeVolumesOutput{
		"": {Volumes: []fsxtypes.Volume{fsxVolume("fsvol-root", "fsx")}},
	}
	withChildren := map[string]*fsx.DescribeVolumesOutput{
		"": {
			Volumes:   []fsxtypes.Volume{fsxVolume("fsvol-root", "fsx"), fsxVolume("fsvol-a", "pvc-a")},
			NextToken: aws.String("page2"),
		},
		"page2": {Volumes: []fsxtypes.Volume{fsxVolume("fsvol-b", "pvc-b")}},
	}

	tests := []struct {
		name        string
		cfg         *Config
		fs          fsxOpenZFSFilesystem
		client      *fakeFSxClient
		wantErr     []string // substrings; nil means no error expected
		wantNoCalls bool
	}{
		{
			name:        "omitted block skips the check",
			cfg:         &Config{},
			fs:          fs,
			client:      &fakeFSxClient{pages: withChildren},
			wantNoCalls: true,
		},
		{
			name:        "enabled false skips the check",
			cfg:         &Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: boolPtr(false)}},
			fs:          fs,
			client:      &fakeFSxClient{pages: withChildren},
			wantNoCalls: true,
		},
		{
			name:        "delete_child_volumes_on_destroy skips the check",
			cfg:         &Config{FSxOpenZFS: &FSxOpenZFSConfig{DeleteChildVolumesOnDestroy: true}},
			fs:          fs,
			client:      &fakeFSxClient{pages: withChildren},
			wantNoCalls: true,
		},
		{
			name:        "filesystem never created skips the check",
			cfg:         &Config{FSxOpenZFS: &FSxOpenZFSConfig{}},
			fs:          fsxOpenZFSFilesystem{},
			client:      &fakeFSxClient{pages: withChildren},
			wantNoCalls: true,
		},
		{
			name:   "root volume only passes",
			cfg:    &Config{FSxOpenZFS: &FSxOpenZFSConfig{}},
			fs:     fs,
			client: &fakeFSxClient{pages: rootOnly},
		},
		{
			name:    "child volumes across pages fail with every volume and the StorageClass",
			cfg:     &Config{FSxOpenZFS: &FSxOpenZFSConfig{}},
			fs:      fs,
			client:  &fakeFSxClient{pages: withChildren},
			wantErr: []string{"fs-123", "2 child volume(s)", "fsvol-a (pvc-a)", "fsvol-b (pvc-b)", `"fsx-openzfs-sc"`, "delete_child_volumes_on_destroy"},
		},
		{
			name:    "API error is returned",
			cfg:     &Config{FSxOpenZFS: &FSxOpenZFSConfig{}},
			fs:      fs,
			client:  &fakeFSxClient{err: errors.New("access denied")},
			wantErr: []string{"access denied"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkFSxOpenZFSChildVolumes(context.Background(), tt.client, tt.cfg, tt.fs)

			if tt.wantNoCalls && tt.client.calls != 0 {
				t.Errorf("DescribeVolumes called %d times, want 0", tt.client.calls)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %v, got nil", tt.wantErr)
			}
			for _, sub := range tt.wantErr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q does not contain %q", err.Error(), sub)
				}
			}
			if strings.Contains(err.Error(), "fsvol-root") {
				t.Errorf("error %q must not list the root volume", err.Error())
			}
		})
	}
}

// TestPrecheckFSxOpenZFSDestroySkips covers the paths that never reach the FSx
// API. They must not announce a check that does not run, and must still return
// the filesystem, which Destroy uses for the final backup notice.
func TestPrecheckFSxOpenZFSDestroySkips(t *testing.T) {
	outputs := map[string]tfexec.OutputMeta{
		"fsx_openzfs_id":             {Value: json.RawMessage(`"fs-123"`)},
		"fsx_openzfs_root_volume_id": {Value: json.RawMessage(`"fsvol-root"`)},
	}

	tests := []struct {
		name    string
		cfg     *Config
		outputs map[string]tfexec.OutputMeta
		wantFS  fsxOpenZFSFilesystem
	}{
		{
			name:    "delete_child_volumes_on_destroy",
			cfg:     &Config{FSxOpenZFS: &FSxOpenZFSConfig{DeleteChildVolumesOnDestroy: true}},
			outputs: outputs,
			wantFS:  fsxOpenZFSFilesystem{ID: "fs-123", RootVolumeID: "fsvol-root"},
		},
		{
			name:    "FSx disabled",
			cfg:     &Config{},
			outputs: outputs,
			wantFS:  fsxOpenZFSFilesystem{ID: "fs-123", RootVolumeID: "fsvol-root"},
		},
		{
			name:    "filesystem never created",
			cfg:     &Config{FSxOpenZFS: &FSxOpenZFSConfig{}},
			outputs: map[string]tfexec.OutputMeta{},
			wantFS:  fsxOpenZFSFilesystem{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updates := make(chan status.Update, 10)
			ctx := status.WithChannel(context.Background(), updates)

			fs, err := precheckFSxOpenZFSDestroy(ctx, tt.outputs, tt.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fs != tt.wantFS {
				t.Errorf("filesystem = %+v, want %+v", fs, tt.wantFS)
			}
			if n := len(updates); n != 0 {
				t.Errorf("sent %d status update(s), want none: %q", n, (<-updates).Message)
			}
		})
	}
}

func TestFSxOpenZFSFilesystemFromOutputs(t *testing.T) {
	raw := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	tests := []struct {
		name    string
		outputs map[string]tfexec.OutputMeta
		want    fsxOpenZFSFilesystem
		wantErr bool
	}{
		{name: "no outputs (empty state)", outputs: map[string]tfexec.OutputMeta{}},
		{
			name: "null outputs when FSx is disabled",
			outputs: map[string]tfexec.OutputMeta{
				"fsx_openzfs_id":             {Value: json.RawMessage("null")},
				"fsx_openzfs_root_volume_id": {Value: json.RawMessage("null")},
			},
		},
		{
			name: "both IDs are read",
			outputs: map[string]tfexec.OutputMeta{
				"fsx_openzfs_id":             {Value: raw("fs-123")},
				"fsx_openzfs_root_volume_id": {Value: raw("fsvol-root")},
			},
			want: fsxOpenZFSFilesystem{ID: "fs-123", RootVolumeID: "fsvol-root"},
		},
		{
			name:    "non-string output is an error",
			outputs: map[string]tfexec.OutputMeta{"fsx_openzfs_id": {Value: raw(42)}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fsxOpenZFSFilesystemFromOutputs(tt.outputs)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
