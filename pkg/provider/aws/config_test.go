package aws

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func boolPtr(b bool) *bool { return &b }

func TestLonghornEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "nil LonghornConfig defaults to enabled",
			config:   Config{Longhorn: nil},
			expected: true,
		},
		{
			name:     "empty LonghornConfig defaults to enabled",
			config:   Config{Longhorn: &LonghornConfig{}},
			expected: true,
		},
		{
			name:     "explicitly enabled",
			config:   Config{Longhorn: &LonghornConfig{Enabled: boolPtr(true)}},
			expected: true,
		},
		{
			name:     "explicitly disabled",
			config:   Config{Longhorn: &LonghornConfig{Enabled: boolPtr(false)}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.LonghornEnabled()
			if got != tt.expected {
				t.Errorf("LonghornEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLonghornReplicaCount(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected int
	}{
		{
			name:     "nil LonghornConfig defaults to 2",
			config:   Config{Longhorn: nil},
			expected: 2,
		},
		{
			name:     "zero replica count defaults to 2",
			config:   Config{Longhorn: &LonghornConfig{}},
			expected: 2,
		},
		{
			name:     "custom replica count",
			config:   Config{Longhorn: &LonghornConfig{ReplicaCount: 3}},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.LonghornReplicaCount()
			if got != tt.expected {
				t.Errorf("LonghornReplicaCount() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestProviderStorageClass(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		expected string
	}{
		{
			name:     "nil provider config defaults to longhorn",
			config:   nil,
			expected: "longhorn",
		},
		{
			name: "longhorn enabled returns longhorn",
			config: map[string]any{
				"region":      "us-west-2",
				"node_groups": map[string]any{},
			},
			expected: "longhorn",
		},
		{
			name: "longhorn disabled returns gp2",
			config: map[string]any{
				"region":      "us-west-2",
				"node_groups": map[string]any{},
				"longhorn": map[string]any{
					"enabled": false,
				},
			},
			expected: "gp2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{}
			cfg := &config.NebariConfig{
				Provider:       "aws",
				ProviderConfig: map[string]any{},
			}
			if tt.config != nil {
				cfg.ProviderConfig["amazon_web_services"] = tt.config
			}

			got := p.StorageClass(cfg)
			if got != tt.expected {
				t.Errorf("StorageClass() = %q, want %q", got, tt.expected)
			}
		})
	}
}
