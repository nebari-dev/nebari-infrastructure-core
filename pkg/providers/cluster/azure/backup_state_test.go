package azure

import (
	"testing"

	tfjson "github.com/hashicorp/terraform-json"

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
			spec: &cluster.BackupBucketSpec{Name: "c", StorageAccount: "sa", ForceDestroy: true},
			want: nil,
		},
		{
			name: "retain returns container before account",
			spec: &cluster.BackupBucketSpec{Name: "c", StorageAccount: "sa", ForceDestroy: false},
			want: []string{
				"azurerm_storage_container.longhorn_backup[0]",
				"azurerm_storage_account.longhorn_backup[0]",
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
			if len(got) > 0 && got[len(got)-1] != "azurerm_storage_account.longhorn_backup[0]" {
				t.Fatalf("expected storage account address last, got %q", got[len(got)-1])
			}
		})
	}
}

func TestPresentAddresses(t *testing.T) {
	t.Run("nil and empty states return empty set", func(t *testing.T) {
		for _, s := range []*tfjson.State{
			nil,
			{},
			{Values: &tfjson.StateValues{}},
		} {
			if got := presentAddresses(s); len(got) != 0 {
				t.Fatalf("expected empty set, got %v", got)
			}
		}
	})

	t.Run("collects root and child module addresses", func(t *testing.T) {
		state := &tfjson.State{
			Values: &tfjson.StateValues{
				RootModule: &tfjson.StateModule{
					Resources: []*tfjson.StateResource{
						{Address: "azurerm_storage_account.longhorn_backup[0]"},
						{Address: "azurerm_storage_container.longhorn_backup[0]"},
						nil,
						{Address: ""},
					},
					ChildModules: []*tfjson.StateModule{
						{Resources: []*tfjson.StateResource{
							{Address: "module.aks_cluster.azurerm_kubernetes_cluster.this"},
						}},
					},
				},
			},
		}
		got := presentAddresses(state)
		want := []string{
			"azurerm_storage_account.longhorn_backup[0]",
			"azurerm_storage_container.longhorn_backup[0]",
			"module.aks_cluster.azurerm_kubernetes_cluster.this",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d addresses, want %d: %v", len(got), len(want), got)
		}
		for _, addr := range want {
			if !got[addr] {
				t.Fatalf("missing address %q in %v", addr, got)
			}
		}
	})
}
