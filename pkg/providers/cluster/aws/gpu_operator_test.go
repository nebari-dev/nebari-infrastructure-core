package aws

import "testing"

func TestHasGPUNodeGroups(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "empty config",
			cfg:  Config{},
			want: false,
		},
		{
			name: "no gpu node groups",
			cfg:  Config{NodeGroups: map[string]NodeGroup{"user": {Instance: "m7i.xlarge"}}},
			want: false,
		},
		{
			name: "mixed cpu and gpu node groups",
			cfg: Config{NodeGroups: map[string]NodeGroup{
				"user": {Instance: "m7i.xlarge"},
				"gpu":  {Instance: "g4dn.2xlarge", GPU: true},
			}},
			want: true,
		},
		{
			name: "only a gpu node group",
			cfg: Config{NodeGroups: map[string]NodeGroup{
				"gpu": {Instance: "g4dn.2xlarge", GPU: true},
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

	// On the EKS NVIDIA AMI the driver and toolkit are preinstalled, so the
	// operator only layers on the device plugin.
	componentEnabled := []struct {
		component string
		want      bool
	}{
		{"driver", false},
		{"toolkit", false},
		{"devicePlugin", true},
	}
	for _, tt := range componentEnabled {
		t.Run(tt.component, func(t *testing.T) {
			m, ok := v[tt.component].(map[string]any)
			if !ok {
				t.Fatalf("%s should be a map, got %v", tt.component, v[tt.component])
			}
			if m[enabledKey] != tt.want {
				t.Errorf("%s.enabled = %v, want %v", tt.component, m[enabledKey], tt.want)
			}
		})
	}

	t.Run("MOFED disabled for EFA safety", func(t *testing.T) {
		dp := v["devicePlugin"].(map[string]any)
		env, ok := dp["env"].([]any)
		if !ok || len(env) == 0 {
			t.Fatalf("devicePlugin.env should set MOFED_ENABLED, got %v", dp["env"])
		}
		first, _ := env[0].(map[string]any)
		if first["name"] != "MOFED_ENABLED" || first["value"] != "false" {
			t.Errorf("expected MOFED_ENABLED=false, got %v", first)
		}
	})
}
