package config

import (
	"testing"

	"github.com/goccy/go-yaml"
)

func TestBackupsConfigParse(t *testing.T) {
	const in = `
longhorn:
  enabled: true
  s3:
    bucket: my-nebari-backups
    region: us-east-1
    prefix: clusterA/
    create_bucket: true
    endpoint: ""
    virtual_hosted_style: false
    access_key_id_env: LONGHORN_S3_ACCESS_KEY_ID
    secret_access_key_env: LONGHORN_S3_SECRET_ACCESS_KEY
    ca_cert:
      kind: secret
      name: longhorn-s3-ca
      namespace: longhorn-system
      key: ca.crt
  allow_recurring_job_while_volume_detached: true
  schedules:
    snapshot: { cron: "0 * * * *", retain: 24, concurrency: 5 }
    backup:   { cron: "0 3 * * *", retain: 30, concurrency: 3 }
`
	var c BackupsConfig
	if err := yaml.Unmarshal([]byte(in), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !c.LonghornEnabled() {
		t.Fatal("expected longhorn backups enabled")
	}
	lh := c.Longhorn
	if lh.S3 == nil || lh.S3.Bucket != "my-nebari-backups" || lh.S3.Region != "us-east-1" {
		t.Fatalf("s3 target not parsed: %+v", lh.S3)
	}
	if lh.S3.Prefix != "clusterA/" {
		t.Fatalf("prefix not parsed: %q", lh.S3.Prefix)
	}
	if !lh.S3.CreateBucket {
		t.Fatal("create_bucket not parsed")
	}
	if lh.S3.CACert == nil || lh.S3.CACert.Name != "longhorn-s3-ca" || lh.S3.CACert.Key != "ca.crt" {
		t.Fatalf("ca_cert not parsed: %+v", lh.S3.CACert)
	}
	if lh.S3.AccessKeyIDEnv != "LONGHORN_S3_ACCESS_KEY_ID" {
		t.Fatalf("access_key_id_env not parsed: %q", lh.S3.AccessKeyIDEnv)
	}
	if !lh.AllowDetached() {
		t.Fatal("expected allow detached true")
	}
	if lh.Schedules.Snapshot.Cron != "0 * * * *" || lh.Schedules.Snapshot.Retain != 24 || lh.Schedules.Snapshot.Concurrency != 5 {
		t.Fatalf("snapshot schedule not parsed: %+v", lh.Schedules.Snapshot)
	}
	if lh.Schedules.Backup.Cron != "0 3 * * *" || lh.Schedules.Backup.Retain != 30 || lh.Schedules.Backup.Concurrency != 3 {
		t.Fatalf("backup schedule not parsed: %+v", lh.Schedules.Backup)
	}
}

func TestRetainOnDestroyDefaultsTrue(t *testing.T) {
	s3 := &S3BackupTarget{}
	if !s3.RetainOnDestroyEnabled() {
		t.Fatal("retain_on_destroy should default to true")
	}
	f := false
	s3.RetainOnDestroy = &f
	if s3.RetainOnDestroyEnabled() {
		t.Fatal("retain_on_destroy=false should disable retain")
	}

	az := &AzureBackupTarget{}
	if !az.RetainOnDestroyEnabled() {
		t.Fatal("retain_on_destroy should default to true")
	}
	az.RetainOnDestroy = &f
	if az.RetainOnDestroyEnabled() {
		t.Fatal("retain_on_destroy=false should disable retain")
	}
}

func TestBackupTargetURL(t *testing.T) {
	tests := []struct {
		name string
		lh   *LonghornBackupConfig
		want string
	}{
		{
			name: "s3 with prefix",
			lh:   &LonghornBackupConfig{S3: &S3BackupTarget{Bucket: "b", Region: "us-east-1", Prefix: "clusterA/"}},
			want: "s3://b@us-east-1/clusterA/",
		},
		{
			name: "s3 no prefix",
			lh:   &LonghornBackupConfig{S3: &S3BackupTarget{Bucket: "b", Region: "us-east-1"}},
			want: "s3://b@us-east-1/",
		},
		{
			name: "s3 prefix without trailing slash gets one",
			lh:   &LonghornBackupConfig{S3: &S3BackupTarget{Bucket: "b", Region: "eu-west-1", Prefix: "p"}},
			want: "s3://b@eu-west-1/p/",
		},
		{
			name: "azure with prefix",
			lh:   &LonghornBackupConfig{Azure: &AzureBackupTarget{Container: "c", Prefix: "clusterA/"}},
			want: "azblob://c@core.windows.net/clusterA/",
		},
		{
			name: "azure no prefix",
			lh:   &LonghornBackupConfig{Azure: &AzureBackupTarget{Container: "c"}},
			want: "azblob://c@core.windows.net/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lh.BackupTargetURL(); got != tt.want {
				t.Fatalf("BackupTargetURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
