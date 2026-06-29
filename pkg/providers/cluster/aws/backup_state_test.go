package aws

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
			spec: &cluster.BackupBucketSpec{Name: "b", ForceDestroy: true},
			want: nil,
		},
		{
			name: "retain returns all dependent addresses, dependents first",
			spec: &cluster.BackupBucketSpec{Name: "b", ForceDestroy: false},
			want: []string{
				"aws_s3_bucket_public_access_block.longhorn_backup[0]",
				"aws_s3_bucket_server_side_encryption_configuration.longhorn_backup[0]",
				"aws_s3_bucket_versioning.longhorn_backup[0]",
				"aws_s3_bucket.longhorn_backup[0]",
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
			// The bucket itself must always be last so its dependents are
			// removed first.
			if len(got) > 0 && got[len(got)-1] != "aws_s3_bucket.longhorn_backup[0]" {
				t.Fatalf("expected bucket address last, got %q", got[len(got)-1])
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
						{Address: "aws_s3_bucket.longhorn_backup[0]"},
						{Address: "aws_vpc.main"},
						nil, // defensively skipped
						{Address: ""},
					},
					ChildModules: []*tfjson.StateModule{
						{Resources: []*tfjson.StateResource{
							{Address: "module.eks.aws_eks_cluster.this[0]"},
						}},
					},
				},
			},
		}
		got := presentAddresses(state)
		want := []string{
			"aws_s3_bucket.longhorn_backup[0]",
			"aws_vpc.main",
			"module.eks.aws_eks_cluster.this[0]",
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
