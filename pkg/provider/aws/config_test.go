package aws

import "testing"

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
