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

func TestLoadBalancerControllerEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "nil AWSLoadBalancerController defaults to enabled",
			config:   Config{AWSLoadBalancerController: nil},
			expected: true,
		},
		{
			name:     "empty AWSLoadBalancerController defaults to enabled",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{}},
			expected: true,
		},
		{
			name:     "explicitly enabled",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{Enabled: boolPtr(true)}},
			expected: true,
		},
		{
			name:     "explicitly disabled",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{Enabled: boolPtr(false)}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.LoadBalancerControllerEnabled()
			if got != tt.expected {
				t.Errorf("LoadBalancerControllerEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLoadBalancerControllerChartVersion(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "nil AWSLoadBalancerController returns default",
			config:   Config{AWSLoadBalancerController: nil},
			expected: defaultLBCChartVersion,
		},
		{
			name:     "empty ChartVersion returns default",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{}},
			expected: defaultLBCChartVersion,
		},
		{
			name:     "custom ChartVersion is used",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{ChartVersion: "3.1.0"}},
			expected: "3.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.LoadBalancerControllerChartVersion()
			if got != tt.expected {
				t.Errorf("LoadBalancerControllerChartVersion() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestProviderInfraSettingsStorageClass(t *testing.T) {
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
			providers := map[string]any{"aws": map[string]any{}}
			if tt.config != nil {
				providers["aws"] = tt.config
			}
			cfg := &config.ClusterConfig{
				Providers: providers,
			}

			got := p.InfraSettings(cfg).StorageClass
			if got != tt.expected {
				t.Errorf("InfraSettings().StorageClass = %q, want %q", got, tt.expected)
			}
		})
	}
}
