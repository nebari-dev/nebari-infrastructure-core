package aws

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
			spec: &cluster.BackupBucketSpec{Create: true, ForceDestroy: true},
			want: nil,
		},
		{
			name: "create false returns no addresses (pod-identity only / external bucket)",
			spec: &cluster.BackupBucketSpec{Create: false, ForceDestroy: false, PodIdentity: true},
			want: nil,
		},
		{
			name: "retain returns all dependent addresses, dependents first",
			spec: &cluster.BackupBucketSpec{Create: true, ForceDestroy: false},
			want: []string{
				"module.eks_cluster.aws_s3_bucket_public_access_block.longhorn_backup[0]",
				"module.eks_cluster.aws_s3_bucket_server_side_encryption_configuration.longhorn_backup[0]",
				"module.eks_cluster.aws_s3_bucket_versioning.longhorn_backup[0]",
				"module.eks_cluster.aws_s3_bucket.longhorn_backup[0]",
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
			if len(got) > 0 && got[len(got)-1] != "module.eks_cluster.aws_s3_bucket.longhorn_backup[0]" {
				t.Fatalf("expected bucket address last, got %q", got[len(got)-1])
			}
		})
	}
}
