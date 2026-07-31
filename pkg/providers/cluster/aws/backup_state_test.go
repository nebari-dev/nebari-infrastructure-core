package aws

import (
	"regexp"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// eksClusterModulePin extracts the version pinned for the eks_cluster module
// call. `\s+` spans the newline between the source and version lines.
var eksClusterModulePin = regexp.MustCompile(`source\s*=\s*"nebari-dev/eks-cluster/aws"\s+version\s*=\s*"([^"]+)"`)

// TestBackupStateAddrsVerifiedAgainstPinnedModule fails on every
// terraform-aws-eks-cluster bump, on purpose: backupStateAddrs is a hardcoded
// list that only a human re-running `tofu graph` against the new module
// version can re-verify, and a stale list silently destroys the backups
// retain_on_destroy promised to keep. TestBackupStateAddrs cannot catch that —
// it compares the list to itself. On a bump: re-verify the five addresses
// against the new module, then update backupStateModuleVersion.
func TestBackupStateAddrsVerifiedAgainstPinnedModule(t *testing.T) {
	mainTF, err := tofuTemplates.ReadFile("templates/main.tf")
	if err != nil {
		t.Fatalf("read embedded templates/main.tf: %v", err)
	}
	m := eksClusterModulePin.FindSubmatch(mainTF)
	if m == nil {
		t.Fatal("templates/main.tf no longer pins nebari-dev/eks-cluster/aws with a literal version; update eksClusterModulePin and re-verify backupStateAddrs")
	}
	if got := string(m[1]); got != backupStateModuleVersion {
		t.Fatalf("templates/main.tf pins terraform-aws-eks-cluster %s but backupStateAddrs was verified against %s; re-verify the state addresses against %s (tofu graph) and update backupStateModuleVersion",
			got, backupStateModuleVersion, got)
	}
}

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
				"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_public_access_block.this[0]",
				"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_server_side_encryption_configuration.this[0]",
				"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_lifecycle_configuration.this[0]",
				"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket_versioning.this[0]",
				"module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket.this[0]",
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
			if len(got) > 0 && got[len(got)-1] != "module.eks_cluster.module.longhorn_backup[0].aws_s3_bucket.this[0]" {
				t.Fatalf("expected bucket address last, got %q", got[len(got)-1])
			}
		})
	}
}
