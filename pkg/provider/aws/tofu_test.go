package aws

import (
	"testing"
)

func TestResolveNodeGroupAMIs(t *testing.T) {
	nvidiaAMI := "AL2023_x86_64_NVIDIA"
	standardAMI := "AL2023_x86_64_STANDARD"
	customAMI := "AL2023_ARM_64_NVIDIA"

	tests := []struct {
		name     string
		input    map[string]NodeGroup
		expected map[string]*string // expected AMIType per node group name
	}{
		{
			name: "gpu node group without ami_type gets default NVIDIA AMI",
			input: map[string]NodeGroup{
				"gpu": {Instance: "g4dn.xlarge", GPU: true},
			},
			expected: map[string]*string{
				"gpu": &nvidiaAMI,
			},
		},
		{
			name: "gpu node group with explicit ami_type keeps it",
			input: map[string]NodeGroup{
				"gpu": {Instance: "g5g.xlarge", GPU: true, AMIType: &customAMI},
			},
			expected: map[string]*string{
				"gpu": &customAMI,
			},
		},
		{
			name: "non-gpu node group without ami_type gets default standard AMI",
			input: map[string]NodeGroup{
				"worker": {Instance: "m7i.xlarge"},
			},
			expected: map[string]*string{
				"worker": &standardAMI,
			},
		},
		{
			name: "non-gpu node group with explicit ami_type keeps it",
			input: map[string]NodeGroup{
				"worker": {Instance: "m7i.xlarge", AMIType: &customAMI},
			},
			expected: map[string]*string{
				"worker": &customAMI,
			},
		},
		{
			name: "mixed node groups resolved independently",
			input: map[string]NodeGroup{
				"worker": {Instance: "m7i.xlarge"},
				"gpu":    {Instance: "g4dn.xlarge", GPU: true},
			},
			expected: map[string]*string{
				"worker": &standardAMI,
				"gpu":    &nvidiaAMI,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveNodeGroupAMIs(tt.input)

			for name, expectedAMI := range tt.expected {
				g, ok := result[name]
				if !ok {
					t.Errorf("node group %q missing from result", name)
					continue
				}
				switch {
				case expectedAMI == nil && g.AMIType != nil:
					t.Errorf("node group %q: expected no AMIType, got %q", name, *g.AMIType)
				case expectedAMI != nil && g.AMIType == nil:
					t.Errorf("node group %q: expected AMIType %q, got nil", name, *expectedAMI)
				case expectedAMI != nil && *g.AMIType != *expectedAMI:
					t.Errorf("node group %q: expected AMIType %q, got %q", name, *expectedAMI, *g.AMIType)
				}
			}
		})
	}

	t.Run("does not mutate input", func(t *testing.T) {
		original := map[string]NodeGroup{
			"gpu":    {Instance: "g4dn.xlarge", GPU: true},
			"worker": {Instance: "m7i.xlarge"},
		}
		resolveNodeGroupAMIs(original)
		if original["gpu"].AMIType != nil {
			t.Error("resolveNodeGroupAMIs mutated the gpu node group in the input map")
		}
		if original["worker"].AMIType != nil {
			t.Error("resolveNodeGroupAMIs mutated the worker node group in the input map")
		}
	})
}
