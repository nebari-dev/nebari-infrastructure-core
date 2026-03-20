package aws

import "testing"

func TestToTFVarsLonghornSGRules(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		wantRuleCount int
		wantRuleKeys  []string
	}{
		{
			name: "longhorn enabled by default populates SG rules",
			config: Config{
				Region:            "us-west-2",
				KubernetesVersion: "1.33",
				NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
			},
			wantRuleCount: 2,
			wantRuleKeys:  []string{"longhorn_webhook_admission", "longhorn_webhook_conversion"},
		},
		{
			name: "longhorn explicitly enabled populates SG rules",
			config: Config{
				Region:            "us-west-2",
				KubernetesVersion: "1.33",
				NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
				Longhorn:          &LonghornConfig{Enabled: boolPtr(true)},
			},
			wantRuleCount: 2,
			wantRuleKeys:  []string{"longhorn_webhook_admission", "longhorn_webhook_conversion"},
		},
		{
			name: "longhorn disabled omits SG rules",
			config: Config{
				Region:            "us-west-2",
				KubernetesVersion: "1.33",
				NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
				Longhorn:          &LonghornConfig{Enabled: boolPtr(false)},
			},
			wantRuleCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := tt.config.toTFVars("test-project")

			if len(vars.NodeSGAdditionalRules) != tt.wantRuleCount {
				t.Errorf("NodeSGAdditionalRules length = %d, want %d", len(vars.NodeSGAdditionalRules), tt.wantRuleCount)
			}

			for _, key := range tt.wantRuleKeys {
				rule, ok := vars.NodeSGAdditionalRules[key]
				if !ok {
					t.Errorf("NodeSGAdditionalRules missing key %q", key)
					continue
				}

				ruleMap, ok := rule.(map[string]any)
				if !ok {
					t.Errorf("NodeSGAdditionalRules[%q] is not map[string]any", key)
					continue
				}

				if ruleMap["protocol"] != "tcp" {
					t.Errorf("rule %q protocol = %v, want tcp", key, ruleMap["protocol"])
				}
				if ruleMap["type"] != "ingress" {
					t.Errorf("rule %q type = %v, want ingress", key, ruleMap["type"])
				}
				if ruleMap["source_cluster_security_group"] != true {
					t.Errorf("rule %q source_cluster_security_group = %v, want true", key, ruleMap["source_cluster_security_group"])
				}
			}
		})
	}
}

func TestToTFVarsLonghornSGRulePorts(t *testing.T) {
	cfg := Config{
		Region:            "us-west-2",
		KubernetesVersion: "1.33",
		NodeGroups:        map[string]NodeGroup{"general": {Instance: "m5.xlarge"}},
	}

	vars := cfg.toTFVars("test-project")

	tests := []struct {
		ruleKey  string
		wantPort int
	}{
		{ruleKey: "longhorn_webhook_admission", wantPort: 9502},
		{ruleKey: "longhorn_webhook_conversion", wantPort: 9501},
	}

	for _, tt := range tests {
		t.Run(tt.ruleKey, func(t *testing.T) {
			rule, ok := vars.NodeSGAdditionalRules[tt.ruleKey]
			if !ok {
				t.Fatalf("missing rule %q", tt.ruleKey)
			}

			ruleMap := rule.(map[string]any)
			if ruleMap["from_port"] != tt.wantPort {
				t.Errorf("from_port = %v, want %d", ruleMap["from_port"], tt.wantPort)
			}
			if ruleMap["to_port"] != tt.wantPort {
				t.Errorf("to_port = %v, want %d", ruleMap["to_port"], tt.wantPort)
			}
		})
	}
}

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
