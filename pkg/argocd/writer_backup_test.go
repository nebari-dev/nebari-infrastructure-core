package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
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

func TestIsBackupPath(t *testing.T) {
	cases := map[string]bool{
		"apps/longhorn-backup.yaml":                                  true,
		"manifests/storage/longhorn-backup/backuptarget.yaml":        true,
		"manifests/storage/longhorn-backup/recurringjob-backup.yaml": true,
		"apps/cert-manager.yaml":                                     false,
		"manifests/networking/gateway.yaml":                          false,
	}
	for path, want := range cases {
		if got := isBackupPath(path); got != want {
			t.Errorf("isBackupPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestWriteAllToGitRendersBackupManifests(t *testing.T) {
	workDir := t.TempDir()
	gitClient := &mockGitClient{workDir: workDir}

	enabled := true
	cfg := &config.NebariConfig{
		ProjectName: "p",
		Backups: &config.BackupsConfig{Longhorn: &config.LonghornBackupConfig{
			Enabled: &enabled,
			S3:      &config.S3BackupTarget{Bucket: "b", Region: "us-east-1", Prefix: "c/"},
			Schedules: config.BackupSchedules{
				Snapshot: config.ScheduleConfig{Cron: "0 * * * *", Retain: 24, Concurrency: 5},
				Backup:   config.ScheduleConfig{Cron: "0 3 * * *", Retain: 30, Concurrency: 3},
			},
		}},
	}
	gitCfg := &git.Config{URL: "https://example.com/repo.git", Branch: "main"}
	if err := WriteAllToGit(context.Background(), gitClient, cfg, gitCfg, cluster.InfraSettings{}); err != nil {
		t.Fatalf("WriteAllToGit: %v", err)
	}

	bt, err := os.ReadFile(filepath.Join(workDir, "manifests/storage/longhorn-backup/backuptarget.yaml")) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatalf("read backuptarget: %v", err)
	}
	if !strings.Contains(string(bt), "s3://b@us-east-1/c/") {
		t.Errorf("backuptarget missing URL: %s", bt)
	}
	// rendered manifest must be valid YAML
	var obj map[string]any
	if err := goyaml.Unmarshal(bt, &obj); err != nil {
		t.Fatalf("backuptarget not valid YAML: %v", err)
	}

	// The app template must be written too.
	if _, err := os.Stat(filepath.Join(workDir, "apps/longhorn-backup.yaml")); err != nil {
		t.Errorf("longhorn-backup app should be written when enabled: %v", err)
	}
}

func TestWriteAllToGitSkipsBackupWhenDisabled(t *testing.T) {
	workDir := t.TempDir()
	gitClient := &mockGitClient{workDir: workDir}

	cfg := &config.NebariConfig{ProjectName: "p"}
	gitCfg := &git.Config{URL: "https://example.com/repo.git", Branch: "main"}
	if err := WriteAllToGit(context.Background(), gitClient, cfg, gitCfg, cluster.InfraSettings{}); err != nil {
		t.Fatalf("WriteAllToGit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "apps/longhorn-backup.yaml")); !os.IsNotExist(err) {
		t.Errorf("longhorn-backup app should not be written when disabled (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "manifests/storage/longhorn-backup")); !os.IsNotExist(err) {
		t.Errorf("longhorn-backup manifests dir should not be written when disabled (err=%v)", err)
	}
}
