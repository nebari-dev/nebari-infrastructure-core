package argocd

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/imageinventory"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

func TestDeployedImageInventory(t *testing.T) {
	inv := imageinventory.New(t)
	dir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "inventory.example.com", Backups: &config.BackupsConfig{Longhorn: &config.LonghornBackupConfig{
		S3:        &config.S3BackupTarget{Bucket: "inventory", Region: "us-east-1"},
		Schedules: config.BackupSchedules{Snapshot: config.ScheduleConfig{Cron: "0 * * * *", Retain: 2, Concurrency: 1}, Backup: config.ScheduleConfig{Cron: "0 0 * * *", Retain: 2, Concurrency: 1}},
	}}}
	src := repository.RemoteSource{URL: "https://example.com/inventory.git", Branch: "main"}
	settings := cluster.InfraSettings{StorageClass: "longhorn", LonghornEnabled: true}
	if err := WriteAllToGit(t.Context(), dir, cfg, src, settings, "inventory-placeholder-ca"); err != nil {
		t.Fatal(err)
	}
	inv.Applications(t, dir, src.URL)
	err := filepath.WalkDir(filepath.Join(dir, "manifests"), func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(name, ".yaml") {
			rel, err := filepath.Rel(dir, name)
			if err != nil {
				return err
			}
			return inv.Manifest(imageinventory.Read(t, name), "pkg/argocd/templates/"+rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	argo := DefaultConfig()
	inv.Chart(t, "pkg/argocd/helm.go", repoURL, strings.TrimPrefix(chartName, repoName+"/"), argo.Version, argo.ReleaseName, argo.Namespace, argo.Values)
	inv.Notes = append(inv.Notes, "Argo CD templates: Longhorn, backup jobs and trust-manager enabled; Kubernetes 1.34; no user/pack overlays. Local repo-server CA init container uses the same Argo CD image.")
	inv.Save(t, "argocd")
}
