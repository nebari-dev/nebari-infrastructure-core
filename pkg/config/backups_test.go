package config

import (
	"strings"
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

func TestLonghornBackupValidate(t *testing.T) {
	valid := func() *LonghornBackupConfig {
		return &LonghornBackupConfig{
			S3: &S3BackupTarget{
				Bucket: "b", Region: "us-east-1",
				AccessKeyIDEnv: "K", SecretAccessKeyEnv: "S",
			},
			Schedules: BackupSchedules{
				Snapshot: ScheduleConfig{Cron: "0 * * * *", Retain: 24, Concurrency: 5},
				Backup:   ScheduleConfig{Cron: "0 3 * * *", Retain: 30, Concurrency: 3},
			},
		}
	}

	tests := []struct {
		name     string
		provider string
		mutate   func(*LonghornBackupConfig)
		wantErr  string // substring; "" means no error
	}{
		{name: "valid s3 on aws", provider: "aws", mutate: func(*LonghornBackupConfig) {}},
		{name: "both targets set", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.Azure = &AzureBackupTarget{Container: "c", StorageAccount: "sa", AccountNameEnv: "N", AccountKeyEnv: "K"}
		}, wantErr: "exactly one"},
		{name: "no target set", provider: "aws", mutate: func(c *LonghornBackupConfig) { c.S3 = nil }, wantErr: "exactly one"},
		{name: "bad snapshot cron", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.Schedules.Snapshot.Cron = "not a cron"
		}, wantErr: "snapshot.cron"},
		{name: "bad backup cron", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.Schedules.Backup.Cron = "0 3 * *"
		}, wantErr: "backup.cron"},
		{name: "non-positive snapshot retain", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.Schedules.Snapshot.Retain = 0
		}, wantErr: "snapshot.retain"},
		{name: "non-positive backup concurrency", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.Schedules.Backup.Concurrency = 0
		}, wantErr: "backup.concurrency"},
		{name: "create_bucket on unsupported provider", provider: "gcp", mutate: func(c *LonghornBackupConfig) {
			c.S3.CreateBucket = true
		}, wantErr: "create_bucket"},
		{name: "create_bucket on aws ok", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3.CreateBucket = true
		}},
		{name: "endpoint with create_bucket", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3.CreateBucket = true
			c.S3.Endpoint = "https://minio.example.com"
		}, wantErr: "endpoint"},
		{name: "missing access_key_id_env", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3.AccessKeyIDEnv = ""
		}, wantErr: "access_key_id_env"},
		{name: "keyless s3 on aws ok", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3.AccessKeyIDEnv = ""
			c.S3.SecretAccessKeyEnv = ""
		}},
		{name: "keyless requires aws provider", provider: "gcp", mutate: func(c *LonghornBackupConfig) {
			c.S3.AccessKeyIDEnv = ""
			c.S3.SecretAccessKeyEnv = ""
		}, wantErr: "keyless"},
		{name: "keyless not allowed with endpoint", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3.AccessKeyIDEnv = ""
			c.S3.SecretAccessKeyEnv = ""
			c.S3.Endpoint = "https://minio.example.com"
		}, wantErr: "keyless"},
		{name: "azure create_container requires azure provider", provider: "aws", mutate: func(c *LonghornBackupConfig) {
			c.S3 = nil
			c.Azure = &AzureBackupTarget{Container: "c", StorageAccount: "sa", AccountNameEnv: "N", AccountKeyEnv: "K", CreateContainer: true}
		}, wantErr: "azure provider"},
		{name: "azure endpoint with create_container", provider: "azure", mutate: func(c *LonghornBackupConfig) {
			c.S3 = nil
			c.Azure = &AzureBackupTarget{Container: "c", StorageAccount: "sa", AccountNameEnv: "N", AccountKeyEnv: "K", CreateContainer: true, Endpoint: "https://blob.example.com"}
		}, wantErr: "endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(c)
			err := c.Validate(tt.provider)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestBackupsValidateNilSafe(t *testing.T) {
	var c *BackupsConfig
	if err := c.Validate("aws"); err != nil {
		t.Fatalf("nil BackupsConfig should validate clean: %v", err)
	}
}

func TestCACertRefValidate(t *testing.T) {
	tests := []struct {
		name    string
		ref     CACertRef
		wantErr string // substring; "" means no error
	}{
		{name: "valid secret ref", ref: CACertRef{Kind: "secret", Name: "ca", Key: "ca.crt"}},
		{name: "valid configmap ref", ref: CACertRef{Kind: "configmap", Name: "ca", Key: "ca.crt"}},
		{name: "bad kind", ref: CACertRef{Kind: "bogus", Name: "ca", Key: "ca.crt"}, wantErr: "kind"},
		{name: "missing name", ref: CACertRef{Kind: "secret", Key: "ca.crt"}, wantErr: "name"},
		{name: "missing key", ref: CACertRef{Kind: "secret", Name: "ca"}, wantErr: "key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ref.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
