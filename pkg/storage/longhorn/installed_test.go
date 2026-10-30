package longhorn

import (
	"context"
	"errors"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	helmtime "helm.sh/helm/v3/pkg/time"
)

// failingDriver is a release store whose every read fails, standing in for a
// cluster API that can't be reached.
type failingDriver struct{ *driver.Memory }

func (*failingDriver) Query(map[string]string) ([]*release.Release, error) {
	return nil, errors.New("connection refused")
}

func storeReleases(t *testing.T, cfg *action.Configuration, statuses ...release.Status) {
	t.Helper()
	for i, s := range statuses {
		rel := &release.Release{
			Name:      ReleaseName,
			Namespace: Namespace,
			Version:   i + 1,
			Info: &release.Info{
				FirstDeployed: helmtime.Time{Time: time.Now()},
				LastDeployed:  helmtime.Time{Time: time.Now()},
				Status:        s,
			},
			Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "longhorn", Version: "1.0.0"}},
		}
		if err := cfg.Releases.Create(rel); err != nil {
			t.Fatalf("store release: %v", err)
		}
	}
}

func TestReleaseInstalled(t *testing.T) {
	tests := []struct {
		name     string
		statuses []release.Status
		failing  bool
		want     bool
		wantErr  bool
	}{
		{name: "no release", want: false},
		{name: "deployed release", statuses: []release.Status{release.StatusDeployed}, want: true},
		{name: "failed upgrade still counts", statuses: []release.Status{release.StatusSuperseded, release.StatusFailed}, want: true},
		{name: "uninstalled with kept history", statuses: []release.Status{release.StatusSuperseded, release.StatusUninstalled}, want: false},
		{name: "unreachable cluster is an error", failing: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestActionConfig(t)
			if tt.failing {
				cfg.Releases = storage.Init(&failingDriver{Memory: driver.NewMemory()})
			}
			storeReleases(t, cfg, tt.statuses...)

			got, err := releaseInstalled(context.Background(), cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("releaseInstalled() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("releaseInstalled() = %v, want %v", got, tt.want)
			}
		})
	}
}
