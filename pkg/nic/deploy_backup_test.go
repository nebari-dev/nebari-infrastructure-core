package nic

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestBackupBucketSpec(t *testing.T) {
	mk := func(lh *config.LonghornBackupConfig) *config.NebariConfig {
		return &config.NebariConfig{Backups: &config.BackupsConfig{Longhorn: lh}}
	}
	enabled := true

	t.Run("nil when no backups", func(t *testing.T) {
		if got := backupBucketSpec(&config.NebariConfig{}); got != nil {
			t.Fatalf("want nil, got %+v", got)
		}
	})
	t.Run("nil when create_bucket false", func(t *testing.T) {
		cfg := mk(&config.LonghornBackupConfig{Enabled: &enabled, S3: &config.S3BackupTarget{Bucket: "b", Region: "r"}})
		if got := backupBucketSpec(cfg); got != nil {
			t.Fatalf("want nil, got %+v", got)
		}
	})
	t.Run("nil when endpoint set", func(t *testing.T) {
		cfg := mk(&config.LonghornBackupConfig{Enabled: &enabled, S3: &config.S3BackupTarget{Bucket: "b", Region: "r", CreateBucket: true, Endpoint: "https://x"}})
		if got := backupBucketSpec(cfg); got != nil {
			t.Fatalf("want nil (external bucket), got %+v", got)
		}
	})
	t.Run("s3 create_bucket with retain default", func(t *testing.T) {
		cfg := mk(&config.LonghornBackupConfig{Enabled: &enabled, S3: &config.S3BackupTarget{Bucket: "b", Region: "r", CreateBucket: true}})
		got := backupBucketSpec(cfg)
		if got == nil || got.Name != "b" || got.ForceDestroy {
			t.Fatalf("want {Name:b ForceDestroy:false}, got %+v", got)
		}
	})
	t.Run("s3 retain_on_destroy false => force destroy", func(t *testing.T) {
		no := false
		cfg := mk(&config.LonghornBackupConfig{Enabled: &enabled, S3: &config.S3BackupTarget{Bucket: "b", Region: "r", CreateBucket: true, RetainOnDestroy: &no}})
		got := backupBucketSpec(cfg)
		if got == nil || !got.ForceDestroy {
			t.Fatalf("want ForceDestroy true, got %+v", got)
		}
	})
	t.Run("azure create_container", func(t *testing.T) {
		cfg := mk(&config.LonghornBackupConfig{Enabled: &enabled, Azure: &config.AzureBackupTarget{Container: "c", StorageAccount: "sa", CreateContainer: true}})
		got := backupBucketSpec(cfg)
		if got == nil || got.Name != "c" || got.StorageAccount != "sa" {
			t.Fatalf("want {Name:c StorageAccount:sa}, got %+v", got)
		}
	})
}
