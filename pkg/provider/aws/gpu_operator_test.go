package aws

import "testing"

func TestIsGPUInstanceType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"g4dn nvidia t4", "g4dn.2xlarge", true},
		{"g5 nvidia a10g", "g5.xlarge", true},
		{"g6 nvidia l4", "g6.48xlarge", true},
		{"gr6 nvidia l4 high-mem", "gr6.4xlarge", true},
		{"p3 nvidia v100", "p3.2xlarge", true},
		{"p4d nvidia a100", "p4d.24xlarge", true},
		{"p5 nvidia h100", "p5.48xlarge", true},
		{"case-insensitive", "G5.XLARGE", true},

		{"g4ad is amd, not nvidia", "g4ad.xlarge", false},
		{"general purpose", "m7i.xlarge", false},
		{"compute graviton", "c7g.2xlarge", false},
		{"memory", "r6i.large", false},
		{"neuron inferentia", "inf2.xlarge", false},
		{"neuron trainium", "trn1.2xlarge", false},
		{"storage", "i4i.large", false},
		{"empty", "", false},
		{"too short", "x", false},
		{"graviton word, no digit", "graviton", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGPUInstanceType(tt.input); got != tt.want {
				t.Errorf("isGPUInstanceType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasGPUNodeGroups(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "no gpu node groups",
			cfg:  Config{NodeGroups: map[string]NodeGroup{"user": {Instance: "m7i.xlarge"}}},
			want: false,
		},
		{
			name: "one gpu node group",
			cfg: Config{NodeGroups: map[string]NodeGroup{
				"user": {Instance: "m7i.xlarge"},
				"gpu":  {Instance: "g4dn.2xlarge", GPU: true},
			}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.hasGPUNodeGroups(); got != tt.want {
				t.Errorf("hasGPUNodeGroups() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGPUOperatorHelmValues(t *testing.T) {
	v := gpuOperatorHelmValues()

	driver, ok := v["driver"].(map[string]any)
	if !ok || driver[enabledKey] != false {
		t.Errorf("driver.enabled should be false (AMI ships the driver), got %v", v["driver"])
	}
	toolkit, ok := v["toolkit"].(map[string]any)
	if !ok || toolkit[enabledKey] != false {
		t.Errorf("toolkit.enabled should be false (AMI ships the toolkit), got %v", v["toolkit"])
	}

	dp, ok := v["devicePlugin"].(map[string]any)
	if !ok || dp[enabledKey] != true {
		t.Fatalf("devicePlugin.enabled should be true, got %v", v["devicePlugin"])
	}

	env, ok := dp["env"].([]any)
	if !ok || len(env) == 0 {
		t.Fatalf("devicePlugin.env should set MOFED_ENABLED, got %v", dp["env"])
	}
	first, _ := env[0].(map[string]any)
	if first["name"] != "MOFED_ENABLED" || first["value"] != "false" {
		t.Errorf("expected MOFED_ENABLED=false for EFA safety, got %v", first)
	}
}
