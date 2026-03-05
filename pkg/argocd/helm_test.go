package argocd

import (
	"testing"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

// TestShouldSkipUpgrade tests the shouldSkipUpgrade function with real release objects
func TestShouldSkipUpgrade(t *testing.T) {
	tests := []struct {
		name          string
		release       *release.Release
		targetVersion string
		want          bool
	}{
		{
			name:          "nil release",
			release:       nil,
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "nil chart",
			release: &release.Release{
				Info:  &release.Info{Status: release.StatusDeployed},
				Chart: nil,
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "nil metadata",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusDeployed},
				Chart: &chart.Chart{
					Metadata: nil,
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "nil info",
			release: &release.Release{
				Info: nil,
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "deployed and version matches - skip upgrade",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusDeployed},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          true,
		},
		{
			name: "deployed but version differs - upgrade needed",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusDeployed},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.0",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "failed status even with matching version - upgrade needed",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusFailed},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "pending-upgrade status even with matching version - upgrade needed",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusPendingUpgrade},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "major version difference - upgrade needed",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusDeployed},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "7.7.9",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
		{
			name: "downgrade - still triggers upgrade",
			release: &release.Release{
				Info: &release.Info{Status: release.StatusDeployed},
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.5.0",
					},
				},
			},
			targetVersion: "9.4.1",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipUpgrade(tt.release, tt.targetVersion)
			if got != tt.want {
				t.Errorf("shouldSkipUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetCurrentVersion tests the getCurrentVersion function
func TestGetCurrentVersion(t *testing.T) {
	tests := []struct {
		name    string
		release *release.Release
		want    string
	}{
		{
			name:    "nil release",
			release: nil,
			want:    "unknown",
		},
		{
			name: "nil chart",
			release: &release.Release{
				Chart: nil,
			},
			want: "unknown",
		},
		{
			name: "nil metadata",
			release: &release.Release{
				Chart: &chart.Chart{
					Metadata: nil,
				},
			},
			want: "unknown",
		},
		{
			name: "valid version",
			release: &release.Release{
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1",
					},
				},
			},
			want: "9.4.1",
		},
		{
			name: "legacy version",
			release: &release.Release{
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "7.7.9",
					},
				},
			},
			want: "7.7.9",
		},
		{
			name: "pre-release version",
			release: &release.Release{
				Chart: &chart.Chart{
					Metadata: &chart.Metadata{
						Version: "9.4.1-alpha",
					},
				},
			},
			want: "9.4.1-alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getCurrentVersion(tt.release)
			if got != tt.want {
				t.Errorf("getCurrentVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDefaultConfigVersion tests that DefaultConfig returns valid configuration
func TestDefaultConfigVersion(t *testing.T) {
	config := DefaultConfig()
	if config.Version == "" {
		t.Error("DefaultConfig should return a non-empty version")
	}
	if config.Namespace == "" {
		t.Error("DefaultConfig should return a non-empty namespace")
	}
	if config.ReleaseName == "" {
		t.Error("DefaultConfig should return a non-empty release name")
	}
}
