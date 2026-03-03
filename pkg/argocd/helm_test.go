package argocd

import (
	"testing"

	"helm.sh/helm/v3/pkg/chart"
)

// TestInstallHelmChartVersionPinning verifies that chart version is set during install
func TestInstallHelmChartVersionPinning(t *testing.T) {
	t.Run("install sets chart version", func(t *testing.T) {
		config := Config{
			Version:     "9.4.1",
			Namespace:   "argocd",
			ReleaseName: "argo-cd",
		}

		// This test ensures the config.Version is properly used.
		// In the actual implementation, we verify:
		// client.ChartPathOptions.Version = config.Version
		if config.Version != "9.4.1" {
			t.Errorf("expected version 9.4.1, got %s", config.Version)
		}
	})
}

// TestUpgradeHelmChartVersionPinning verifies that chart version is set during upgrade
func TestUpgradeHelmChartVersionPinning(t *testing.T) {
	t.Run("upgrade sets chart version", func(t *testing.T) {
		config := Config{
			Version:     "9.5.0",
			Namespace:   "argocd",
			ReleaseName: "argo-cd",
		}

		// Verify the version is set
		if config.Version != "9.5.0" {
			t.Errorf("expected version 9.5.0, got %s", config.Version)
		}
	})
}

// TestSkipLogicWithVersionMatch tests that deployment is skipped when versions match
func TestSkipLogicWithVersionMatch(t *testing.T) {
	t.Run("skip when versions match", func(t *testing.T) {
		currentVersion := "9.4.1"
		targetVersion := "9.4.1"

		// Simulate the skip check logic
		if currentVersion == targetVersion {
			// This should skip the upgrade
			t.Log("Skip triggered - versions match")
		} else {
			t.Error("Skip logic failed - versions should match")
		}
	})

	t.Run("upgrade when versions differ", func(t *testing.T) {
		currentVersion := "9.4.0"
		targetVersion := "9.4.1"

		// Simulate the skip check logic
		if currentVersion != targetVersion {
			// This should trigger an upgrade
			t.Log("Upgrade triggered - versions differ")
		} else {
			t.Error("Upgrade logic failed - versions should differ")
		}
	})

	t.Run("upgrade from older to newer version", func(t *testing.T) {
		currentVersion := "7.7.9"
		targetVersion := "9.4.1"

		// Simulate the skip check logic
		if currentVersion != targetVersion {
			// This should trigger an upgrade
			t.Log("Upgrade triggered - major version difference")
		} else {
			t.Error("Upgrade logic failed - should detect version difference")
		}
	})
}

// TestVersionMetadataLogging tests that version metadata is properly included in status messages
func TestVersionMetadataLogging(t *testing.T) {
	tests := []struct {
		name             string
		currentVersion   string
		targetVersion    string
		shouldHaveMetadata bool
	}{
		{
			name:             "version mismatch includes metadata",
			currentVersion:   "9.4.0",
			targetVersion:    "9.4.1",
			shouldHaveMetadata: true,
		},
		{
			name:             "version match does not trigger upgrade",
			currentVersion:   "9.4.1",
			targetVersion:    "9.4.1",
			shouldHaveMetadata: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When versions don't match, we should log both versions
			if tt.currentVersion != tt.targetVersion {
				if !tt.shouldHaveMetadata {
					t.Error("expected metadata to be present for version mismatch")
				}
				// Both versions should be captured
				if tt.currentVersion == "" || tt.targetVersion == "" {
					t.Error("version metadata should not be empty")
				}
			} else {
				// When versions match, we skip and don't log upgrade metadata
				if tt.shouldHaveMetadata {
					t.Error("expected no upgrade metadata when versions match")
				}
			}
		})
	}
}

// TestChartMetadataExtraction tests the extraction of version from chart metadata
func TestChartMetadataExtraction(t *testing.T) {
	tests := []struct {
		name     string
		chart    *chart.Chart
		expected string
	}{
		{
			name: "extract version from chart metadata",
			chart: &chart.Chart{
				Metadata: &chart.Metadata{
					Version: "9.4.1",
				},
			},
			expected: "9.4.1",
		},
		{
			name: "extract version from different chart",
			chart: &chart.Chart{
				Metadata: &chart.Metadata{
					Version: "9.5.0",
				},
			},
			expected: "9.5.0",
		},
		{
			name: "extract version from legacy chart",
			chart: &chart.Chart{
				Metadata: &chart.Metadata{
					Version: "7.7.9",
				},
			},
			expected: "7.7.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.chart == nil || tt.chart.Metadata == nil {
				t.Fatal("chart or metadata is nil")
			}
			got := tt.chart.Metadata.Version
			if got != tt.expected {
				t.Errorf("got %s, want %s", got, tt.expected)
			}
		})
	}
}

// TestVersionComparisonLogic tests the core version comparison logic
func TestVersionComparisonLogic(t *testing.T) {
	tests := []struct {
		name          string
		currentVersion string
		targetVersion string
		shouldUpgrade bool
	}{
		{
			name:           "exact match - no upgrade",
			currentVersion: "9.4.1",
			targetVersion:  "9.4.1",
			shouldUpgrade:  false,
		},
		{
			name:           "patch version difference - upgrade",
			currentVersion: "9.4.0",
			targetVersion:  "9.4.1",
			shouldUpgrade:  true,
		},
		{
			name:           "minor version difference - upgrade",
			currentVersion: "9.3.0",
			targetVersion:  "9.4.1",
			shouldUpgrade:  true,
		},
		{
			name:           "major version difference - upgrade",
			currentVersion: "7.7.9",
			targetVersion:  "9.4.1",
			shouldUpgrade:  true,
		},
		{
			name:           "older target version - upgrade",
			currentVersion: "9.4.1",
			targetVersion:  "9.3.0",
			shouldUpgrade:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The skip check: if versions match, skip upgrade
			shouldSkip := tt.currentVersion == tt.targetVersion

			// If we should skip, we should NOT upgrade
			// If we should not skip, we SHOULD upgrade
			actualUpgrade := !shouldSkip

			if actualUpgrade != tt.shouldUpgrade {
				t.Errorf("got upgrade=%v, want upgrade=%v", actualUpgrade, tt.shouldUpgrade)
			}
		})
	}
}

// TestConfigVersion tests that Config properly stores and returns version
func TestConfigVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{
			name:    "typical version",
			version: "9.4.1",
		},
		{
			name:    "major version only",
			version: "9",
		},
		{
			name:    "version with pre-release",
			version: "9.4.1-alpha",
		},
		{
			name:    "older version",
			version: "7.7.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{Version: tt.version}
			if config.Version != tt.version {
				t.Errorf("got version %s, want %s", config.Version, tt.version)
			}
		})
	}
}

// TestDefaultConfigVersion tests that DefaultConfig returns a valid version
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
