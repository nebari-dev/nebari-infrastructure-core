package argocd

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestNewTemplateDataBackups(t *testing.T) {
	enabled := true
	cfg := &config.NebariConfig{
		ProjectName: "p",
		Backups: &config.BackupsConfig{
			Longhorn: &config.LonghornBackupConfig{
				Enabled: &enabled,
				S3:      &config.S3BackupTarget{Bucket: "b", Region: "us-east-1", Prefix: "c/"},
				Schedules: config.BackupSchedules{
					Snapshot: config.ScheduleConfig{Cron: "0 * * * *", Retain: 24, Concurrency: 5},
					Backup:   config.ScheduleConfig{Cron: "0 3 * * *", Retain: 30, Concurrency: 3},
				},
			},
		},
	}
	d := NewTemplateData(cfg, nil, cluster.InfraSettings{})
	if !d.LonghornBackupEnabled {
		t.Fatal("expected LonghornBackupEnabled true")
	}
	if d.LonghornBackupTargetURL != "s3://b@us-east-1/c/" {
		t.Fatalf("target url = %q", d.LonghornBackupTargetURL)
	}
	if d.LonghornBackupCredentialSecret != "longhorn-backup-credentials" {
		t.Fatalf("secret name = %q", d.LonghornBackupCredentialSecret)
	}
	if d.LonghornSnapshotCron != "0 * * * *" || d.LonghornSnapshotRetain != 24 || d.LonghornSnapshotConcurrency != 5 {
		t.Fatalf("snapshot fields wrong: %+v", d)
	}
	if d.LonghornBackupCron != "0 3 * * *" || d.LonghornBackupRetain != 30 || d.LonghornBackupConcurrency != 3 {
		t.Fatalf("backup fields wrong: %+v", d)
	}
	if d.LonghornAllowDetached != "true" {
		t.Fatalf("allow detached = %q", d.LonghornAllowDetached)
	}
}

func TestNewTemplateDataBackupsDisabled(t *testing.T) {
	d := NewTemplateData(&config.NebariConfig{ProjectName: "p"}, nil, cluster.InfraSettings{})
	if d.LonghornBackupEnabled {
		t.Fatal("expected disabled when no backups block")
	}
}
