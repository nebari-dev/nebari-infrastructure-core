package nic

import (
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

func TestEnsureBackupsHaveLonghorn(t *testing.T) {
	enabled := true
	disabled := false

	// clusterFor builds a minimal ClusterConfig whose ProviderName() returns name.
	clusterFor := func(name string) *config.ClusterConfig {
		return &config.ClusterConfig{Providers: map[string]any{name: map[string]any{}}}
	}
	backups := func(en *bool) *config.BackupsConfig {
		return &config.BackupsConfig{Longhorn: &config.LonghornBackupConfig{Enabled: en}}
	}

	tests := []struct {
		name         string
		provider     string
		backups      *config.BackupsConfig
		storageClass string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "backups disabled with non-longhorn storage class is allowed",
			provider:     "azure",
			backups:      backups(&disabled),
			storageClass: "managed-csi",
			wantErr:      false,
		},
		{
			name:         "no backups block is allowed",
			provider:     "aws",
			backups:      nil,
			storageClass: "gp2",
			wantErr:      false,
		},
		{
			name:         "backups enabled with longhorn storage class is allowed",
			provider:     "aws",
			backups:      backups(&enabled),
			storageClass: longhorn.StorageClassName,
			wantErr:      false,
		},
		{
			name:         "backups enabled on azure managed-csi is rejected",
			provider:     "azure",
			backups:      backups(&enabled),
			storageClass: "managed-csi",
			wantErr:      true,
			errContains:  "not Longhorn",
		},
		{
			name:         "backups enabled on aws with longhorn disabled (gp2) is rejected",
			provider:     "aws",
			backups:      backups(&enabled),
			storageClass: "gp2",
			wantErr:      true,
			errContains:  "not Longhorn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{
				Cluster: clusterFor(tt.provider),
				Backups: tt.backups,
			}
			err := ensureBackupsHaveLonghorn(cfg, tt.storageClass)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}
