package aws

import (
	"reflect"
	"testing"
)

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
			if got := tt.cfg.HasGPUNodeGroups(); got != tt.want {
				t.Errorf("HasGPUNodeGroups() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGPUOperatorHelmValues(t *testing.T) {
	v := gpuOperatorHelmValues(&Config{})

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

func TestGPUOperatorHelmValuesTolerations(t *testing.T) {
	tenant := func(team string) Taint {
		return Taint{Key: "tenant.nebari.dev/group", Value: team, Effect: "NO_SCHEDULE"}
	}
	equal := func(key, value, effect string) any {
		return toleration(key, "Equal", value, effect)
	}

	tests := []struct {
		name       string
		nodeGroups map[string]NodeGroup
		// want is the tolerations added to the chart defaults; nil means
		// neither list is set and the chart defaults apply.
		want []any
	}{
		{
			name:       "no gpu node groups",
			nodeGroups: map[string]NodeGroup{"user": {Instance: "m7i.xlarge", Taints: []Taint{tenant("a")}}},
			want:       nil,
		},
		{
			name:       "gpu node group with only the default taint",
			nodeGroups: map[string]NodeGroup{"gpu": {Instance: "g4dn.xlarge", GPU: true}},
			want:       nil,
		},
		{
			name: "explicit nvidia.com/gpu NoSchedule taint is already tolerated",
			nodeGroups: map[string]NodeGroup{"gpu": {Instance: "g4dn.xlarge", GPU: true, Taints: []Taint{
				{Key: gpuTaintKey, Value: "present", Effect: "NO_SCHEDULE"},
			}}},
			want: nil,
		},
		{
			name: "tainted gpu node group",
			nodeGroups: map[string]NodeGroup{
				"team-a":     {Instance: "m6i.4xlarge", Taints: []Taint{tenant("a")}},
				"team-a-gpu": {Instance: "g6.12xlarge", GPU: true, Taints: []Taint{tenant("a")}},
			},
			want: []any{equal("tenant.nebari.dev/group", "a", "NoSchedule")},
		},
		{
			name: "several gpu node groups, sorted and deduplicated, cpu taints ignored",
			nodeGroups: map[string]NodeGroup{
				"team-b-gpu": {Instance: "g6.4xlarge", GPU: true, Taints: []Taint{tenant("b")}},
				"team-a-gpu": {Instance: "g6.12xlarge", GPU: true, Taints: []Taint{tenant("a")}},
				"team-a-big": {Instance: "p5.4xlarge", GPU: true, Taints: []Taint{tenant("a")}},
				"storage": {Instance: "m7g.large", Taints: []Taint{
					{Key: "node.longhorn.io/storage", Value: "true", Effect: "NO_SCHEDULE"},
				}},
			},
			want: []any{
				equal("tenant.nebari.dev/group", "a", "NoSchedule"),
				equal("tenant.nebari.dev/group", "b", "NoSchedule"),
			},
		},
		{
			name: "taint without a value",
			nodeGroups: map[string]NodeGroup{"gpu": {Instance: "g5.xlarge", GPU: true, Taints: []Taint{
				{Key: "dedicated", Effect: "NO_SCHEDULE"},
			}}},
			want: []any{equal("dedicated", "", "NoSchedule")},
		},
		{
			name: "nvidia.com/gpu taint with another effect, and other effects",
			nodeGroups: map[string]NodeGroup{"gpu": {Instance: "g5.xlarge", GPU: true, Taints: []Taint{
				{Key: gpuTaintKey, Value: "true", Effect: "NO_EXECUTE"},
				{Key: "dedicated", Value: "ml", Effect: "PREFER_NO_SCHEDULE"},
			}}},
			want: []any{
				equal("dedicated", "ml", "PreferNoSchedule"),
				equal(gpuTaintKey, "true", "NoExecute"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := gpuOperatorHelmValues(&Config{NodeGroups: tt.nodeGroups})

			if tt.want == nil {
				if _, ok := v["daemonsets"]; ok {
					t.Errorf("daemonsets should be unset, got %v", v["daemonsets"])
				}
				if _, ok := v["node-feature-discovery"]; ok {
					t.Errorf("node-feature-discovery should be unset, got %v", v["node-feature-discovery"])
				}
				return
			}

			operands := v["daemonsets"].(map[string]any)["tolerations"]
			if want := append(chartOperandTolerations(), tt.want...); !reflect.DeepEqual(operands, want) {
				t.Errorf("daemonsets.tolerations = %v, want %v", operands, want)
			}
			worker := v["node-feature-discovery"].(map[string]any)["worker"].(map[string]any)["tolerations"]
			if want := append(chartNFDWorkerTolerations(), tt.want...); !reflect.DeepEqual(worker, want) {
				t.Errorf("node-feature-discovery.worker.tolerations = %v, want %v", worker, want)
			}
		})
	}
}

// TestGPUOperatorHelmValuesTolerationsLiteral pins the exact Helm values for one
// tainted GPU node group, spelled out rather than built from the helpers the
// code uses, so a change to the restated chart defaults or to the toleration
// shape fails here. The defaults are the gpu-operator v26.3.2 values.yaml lists.
func TestGPUOperatorHelmValuesTolerationsLiteral(t *testing.T) {
	v := gpuOperatorHelmValues(&Config{NodeGroups: map[string]NodeGroup{
		"team-a-gpu": {Instance: "g6.4xlarge", GPU: true, Taints: []Taint{
			{Key: "tenant.nebari.dev/group", Value: "a", Effect: "NO_SCHEDULE"},
			{Key: "dedicated", Effect: "NO_EXECUTE"},
		}},
	}})

	gpuDefault := map[string]any{"key": "nvidia.com/gpu", "operator": "Exists", "effect": "NoSchedule"}
	extra := []any{
		// A taint without a value keeps an explicit empty value: Equal with
		// value "" matches only a taint whose value is empty.
		map[string]any{"key": "dedicated", "operator": "Equal", "value": "", "effect": "NoExecute"},
		map[string]any{"key": "tenant.nebari.dev/group", "operator": "Equal", "value": "a", "effect": "NoSchedule"},
	}

	wantOperands := append([]any{gpuDefault}, extra...)
	if got := v["daemonsets"].(map[string]any)["tolerations"]; !reflect.DeepEqual(got, wantOperands) {
		t.Errorf("daemonsets.tolerations = %v, want %v", got, wantOperands)
	}

	wantWorker := append([]any{
		map[string]any{"key": "node-role.kubernetes.io/control-plane", "operator": "Equal", "value": "", "effect": "NoSchedule"},
		gpuDefault,
	}, extra...)
	got := v["node-feature-discovery"].(map[string]any)["worker"].(map[string]any)["tolerations"]
	if !reflect.DeepEqual(got, wantWorker) {
		t.Errorf("node-feature-discovery.worker.tolerations = %v, want %v", got, wantWorker)
	}
}

func TestGPUOperatorHelmValuesNilConfig(t *testing.T) {
	v := gpuOperatorHelmValues(nil)
	if _, ok := v["daemonsets"]; ok {
		t.Errorf("nil config should leave daemonsets unset, got %v", v["daemonsets"])
	}
}
