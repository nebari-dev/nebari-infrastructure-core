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
