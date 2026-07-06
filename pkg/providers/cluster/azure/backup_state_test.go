package azure

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestBackupStateAddrs(t *testing.T) {
	tests := []struct {
		name string
		spec *cluster.BackupBucketSpec
		want []string
	}{
		{
			name: "nil spec returns no addresses",
			spec: nil,
			want: nil,
		},
		{
			name: "force destroy returns no addresses (delete on destroy)",
			spec: &cluster.BackupBucketSpec{ForceDestroy: true},
			want: nil,
		},
		{
			name: "retain returns container before account",
			spec: &cluster.BackupBucketSpec{ForceDestroy: false},
			want: []string{
				"module.aks_cluster.azurerm_storage_container.longhorn_backup[0]",
				"module.aks_cluster.azurerm_storage_account.longhorn_backup[0]",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := backupStateAddrs(tt.spec)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("addr[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
			// The storage account must always be last so the container (its
			// dependent) is removed first.
			if len(got) > 0 && got[len(got)-1] != "module.aks_cluster.azurerm_storage_account.longhorn_backup[0]" {
				t.Fatalf("expected storage account address last, got %q", got[len(got)-1])
			}
		})
	}
}
