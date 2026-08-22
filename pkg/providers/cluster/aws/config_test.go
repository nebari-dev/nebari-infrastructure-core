package aws

import (
	"maps"
	"testing"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

func boolPtr(b bool) *bool { return &b }

func durPtr(d time.Duration) *time.Duration { return &d }

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
			config:   Config{Longhorn: &longhorn.Config{}},
			expected: true,
		},
		{
			name:     "explicitly enabled",
			config:   Config{Longhorn: &longhorn.Config{Enabled: boolPtr(true)}},
			expected: true,
		},
		{
			name:     "explicitly disabled",
			config:   Config{Longhorn: &longhorn.Config{Enabled: boolPtr(false)}},
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

func TestEnabledCrossplaneCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected map[string]bool
	}{
		{name: "nil config", config: nil, expected: nil},
		{name: "zero value", config: &Config{}, expected: nil},
		{name: "empty list", config: &Config{CrossplaneCapabilities: []string{}}, expected: nil},
		{
			name:     "s3 capability includes workload identity dependencies",
			config:   &Config{CrossplaneCapabilities: []string{"s3"}},
			expected: map[string]bool{"aws-s3": true, "aws-iam": true, "aws-eks": true},
		},
		{
			name:     "rds remains independent",
			config:   &Config{CrossplaneCapabilities: []string{"rds"}},
			expected: map[string]bool{"aws-rds": true},
		},
		{
			name:   "multiple capabilities",
			config: &Config{CrossplaneCapabilities: []string{"s3", "rds"}},
			expected: map[string]bool{
				"aws-s3":  true,
				"aws-iam": true,
				"aws-eks": true,
				"aws-rds": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.EnabledCrossplaneCapabilities()
			if !maps.Equal(got, tt.expected) {
				t.Errorf("EnabledCrossplaneCapabilities() = %v, want %v", got, tt.expected)
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
			config:   Config{Longhorn: &longhorn.Config{}},
			expected: 2,
		},
		{
			name:     "custom replica count",
			config:   Config{Longhorn: &longhorn.Config{ReplicaCount: 3}},
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

func TestLoadBalancerControllerDestroyTimeout(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected time.Duration
	}{
		{
			name:     "nil AWSLoadBalancerController defaults to 5m",
			config:   Config{AWSLoadBalancerController: nil},
			expected: 5 * time.Minute,
		},
		{
			name:     "nil DestroyTimeout defaults to 5m",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{}},
			expected: 5 * time.Minute,
		},
		{
			name:     "explicitly set to 90s",
			config:   Config{AWSLoadBalancerController: &AWSLoadBalancerControllerConfig{DestroyTimeout: durPtr(90 * time.Second)}},
			expected: 90 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.LoadBalancerControllerDestroyTimeout()
			if got != tt.expected {
				t.Errorf("LoadBalancerControllerDestroyTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClusterAutoscalerEnabled(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "nil ClusterAutoscaler defaults to enabled",
			config:   Config{ClusterAutoscaler: nil},
			expected: true,
		},
		{
			name:     "empty ClusterAutoscaler defaults to enabled",
			config:   Config{ClusterAutoscaler: &ClusterAutoscalerConfig{}},
			expected: true,
		},
		{
			name:     "explicitly enabled",
			config:   Config{ClusterAutoscaler: &ClusterAutoscalerConfig{Enabled: boolPtr(true)}},
			expected: true,
		},
		{
			name:     "explicitly disabled",
			config:   Config{ClusterAutoscaler: &ClusterAutoscalerConfig{Enabled: boolPtr(false)}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ClusterAutoscalerEnabled()
			if got != tt.expected {
				t.Errorf("ClusterAutoscalerEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClusterAutoscalerChartVersion(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "nil ClusterAutoscaler returns default",
			config:   Config{ClusterAutoscaler: nil},
			expected: defaultClusterAutoscalerChartVersion,
		},
		{
			name:     "empty ChartVersion returns default",
			config:   Config{ClusterAutoscaler: &ClusterAutoscalerConfig{}},
			expected: defaultClusterAutoscalerChartVersion,
		},
		{
			name:     "custom ChartVersion is used",
			config:   Config{ClusterAutoscaler: &ClusterAutoscalerConfig{ChartVersion: "9.50.0"}},
			expected: "9.50.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ClusterAutoscalerChartVersion()
			if got != tt.expected {
				t.Errorf("ClusterAutoscalerChartVersion() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestClusterAutoscalerImageTag(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "derives tag from kubernetes version",
			config:   Config{KubernetesVersion: "1.35"},
			expected: "v1.35.0",
		},
		{
			name:     "explicit image tag overrides derivation",
			config:   Config{KubernetesVersion: "1.35", ClusterAutoscaler: &ClusterAutoscalerConfig{ImageTag: "v1.34.2"}},
			expected: "v1.34.2",
		},
		{
			name:     "no kubernetes version and no override yields empty",
			config:   Config{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.ClusterAutoscalerImageTag()
			if got != tt.expected {
				t.Errorf("ClusterAutoscalerImageTag() = %q, want %q", got, tt.expected)
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
