package aws

import "testing"

func TestFSxOpenZFSCSIHelmValues(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		checkValues map[string]any
	}{
		{
			name: "sets fsGroup policy, region, controller service account, and node tolerations",
			cfg:  &Config{Region: "us-west-2", FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}},
			checkValues: map[string]any{
				"csidriver.fsGroupPolicy":          "File",
				"controller.region":                "us-west-2",
				"controller.serviceAccount.create": true,
				"controller.serviceAccount.name":   "fsx-openzfs-csi-controller-sa",
				"node.tolerateAllTaints":           true,
			},
		},
		{
			name: "region follows the cluster config",
			cfg:  &Config{Region: "eu-central-1", FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}},
			checkValues: map[string]any{
				"controller.region": "eu-central-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := fsxOpenZFSCSIHelmValues(tt.cfg)

			for key, want := range tt.checkValues {
				got := getNestedValue(values, key)
				if got == nil {
					t.Errorf("key %q not found in values", key)
					continue
				}
				if got != want {
					t.Errorf("values[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
				}
			}
		})
	}
}

func TestFSxOpenZFSCSIChartVersion(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{name: "nil FSxOpenZFS returns default", config: Config{}, expected: defaultFSxOpenZFSCSIChartVersion},
		{name: "empty CSIChartVersion returns default", config: Config{FSxOpenZFS: &FSxOpenZFSConfig{Enabled: true}}, expected: defaultFSxOpenZFSCSIChartVersion},
		{name: "custom CSIChartVersion is used", config: Config{FSxOpenZFS: &FSxOpenZFSConfig{CSIChartVersion: "1.3.0"}}, expected: "1.3.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.FSxOpenZFSCSIChartVersion(); got != tt.expected {
				t.Errorf("FSxOpenZFSCSIChartVersion() = %q, want %q", got, tt.expected)
			}
		})
	}
}
