package longhorn

import (
	"context"
	"io"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
)

// newTestActionConfig builds a helm action.Configuration backed by an
// in-memory release store and a no-op kube client. Used to exercise
// Uninstall logic without a real cluster.
func newTestActionConfig(t *testing.T) *action.Configuration {
	t.Helper()
	return &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartutil.DefaultCapabilities,
		Log:          func(format string, v ...any) { t.Logf(format, v...) },
	}
}

func TestUninstallReleaseNoOpWhenAbsent(t *testing.T) {
	cfg := newTestActionConfig(t)

	if err := uninstallRelease(context.Background(), cfg); err != nil {
		t.Fatalf("uninstallRelease() with no release present should be a no-op, got error: %v", err)
	}
}

func TestUninstallReleaseRemovesExistingRelease(t *testing.T) {
	cfg := newTestActionConfig(t)

	rel := &release.Release{
		Name:      ReleaseName,
		Namespace: Namespace,
		Version:   1,
		Info: &release.Info{
			FirstDeployed: helmtime.Time{Time: time.Now()},
			LastDeployed:  helmtime.Time{Time: time.Now()},
			Status:        release.StatusDeployed,
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{Name: "longhorn", Version: "1.0.0"},
		},
	}
	if err := cfg.Releases.Create(rel); err != nil {
		t.Fatalf("seed release: %v", err)
	}

	if err := uninstallRelease(context.Background(), cfg); err != nil {
		t.Fatalf("uninstallRelease() = %v, want nil", err)
	}

	hist := action.NewHistory(cfg)
	hist.Max = 1
	if _, err := hist.Run(ReleaseName); err == nil {
		t.Error("expected release to be gone after uninstallRelease(), still found in history")
	}
}

