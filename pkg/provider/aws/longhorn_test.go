package aws

import (
	"testing"
)

func TestLonghornHelmValues(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		checkValues map[string]any // nested key checks
	}{
		{
			name:   "default config produces base values",
			config: &Config{},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount":        2,
				"persistence.defaultFsType":                   "ext4",
				"persistence.defaultClass":                    true,
				"defaultSettings.replicaZoneSoftAntiAffinity": "true",
				"defaultSettings.replicaAutoBalance":          "best-effort",
			},
		},
		{
			name: "custom replica count",
			config: &Config{
				Longhorn: &LonghornConfig{ReplicaCount: 3},
			},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount": 3,
			},
		},
		{
			name: "dedicated nodes adds nodeSelector and tolerations",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: true,
					NodeSelector:   map[string]string{"node.longhorn.io/storage": "true"},
				},
			},
			checkValues: map[string]any{
				"defaultSettings.createDefaultDiskLabeledNodes": true,
			},
		},
		{
			name: "dedicated nodes without custom nodeSelector uses default",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: true,
				},
			},
			checkValues: map[string]any{
				"defaultSettings.createDefaultDiskLabeledNodes": true,
			},
		},
		{
			name: "non-dedicated nodes omits nodeSelector and tolerations",
			config: &Config{
				Longhorn: &LonghornConfig{
					DedicatedNodes: false,
					ReplicaCount:   2,
				},
			},
			checkValues: map[string]any{
				"persistence.defaultClassReplicaCount": 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := longhornHelmValues(tt.config)

			for key, want := range tt.checkValues {
				got := getNestedValue(values, key)
				if got == nil {
					t.Errorf("key %q not found in values", key)
					continue
				}
				if got != want {
					t.Errorf("values[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
				}
			}
		})
	}
}

func TestLonghornHelmValuesDedicatedNodesStructure(t *testing.T) {
	cfg := &Config{
		Longhorn: &LonghornConfig{
			DedicatedNodes: true,
			NodeSelector:   map[string]string{"node.longhorn.io/storage": "true"},
		},
	}

	values := longhornHelmValues(cfg)

	// Check longhornManager has nodeSelector
	manager, ok := values["longhornManager"].(map[string]any)
	if !ok {
		t.Fatal("longhornManager not found or not a map")
	}
	ns, ok := manager["nodeSelector"].(map[string]string)
	if !ok {
		t.Fatal("longhornManager.nodeSelector not found or not a map[string]string")
	}
	if ns["node.longhorn.io/storage"] != "true" {
		t.Errorf("longhornManager.nodeSelector[node.longhorn.io/storage] = %q, want %q", ns["node.longhorn.io/storage"], "true")
	}

	// Check longhornManager has tolerations
	tolerations, ok := manager["tolerations"].([]map[string]string)
	if !ok {
		t.Fatal("longhornManager.tolerations not found or not a []map[string]string")
	}
	if len(tolerations) != 1 {
		t.Fatalf("longhornManager.tolerations length = %d, want 1", len(tolerations))
	}
	if tolerations[0]["key"] != "node.longhorn.io/storage" {
		t.Errorf("toleration key = %q, want %q", tolerations[0]["key"], "node.longhorn.io/storage")
	}
	if tolerations[0]["operator"] != "Exists" {
		t.Errorf("toleration operator = %q, want %q", tolerations[0]["operator"], "Exists")
	}
	if tolerations[0]["effect"] != "NoSchedule" {
		t.Errorf("toleration effect = %q, want %q", tolerations[0]["effect"], "NoSchedule")
	}

	// Check longhornDriver has the same structure
	driver, ok := values["longhornDriver"].(map[string]any)
	if !ok {
		t.Fatal("longhornDriver not found or not a map")
	}
	_, ok = driver["nodeSelector"].(map[string]string)
	if !ok {
		t.Fatal("longhornDriver.nodeSelector not found or not a map[string]string")
	}
	_, ok = driver["tolerations"].([]map[string]string)
	if !ok {
		t.Fatal("longhornDriver.tolerations not found or not a []map[string]string")
	}
}

func TestLonghornHelmValuesNonDedicatedOmitsNodeSelector(t *testing.T) {
	cfg := &Config{
		Longhorn: &LonghornConfig{
			DedicatedNodes: false,
		},
	}

	values := longhornHelmValues(cfg)

	if _, ok := values["longhornManager"]; ok {
		t.Error("longhornManager should not be set when DedicatedNodes is false")
	}
	if _, ok := values["longhornDriver"]; ok {
		t.Error("longhornDriver should not be set when DedicatedNodes is false")
	}
}

// getNestedValue retrieves a value from a nested map using a dot-separated path.
func getNestedValue(m map[string]any, path string) any {
	parts := splitDotPath(path)
	var current any = m
	for _, part := range parts {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = cm[part]
		if !ok {
			return nil
		}
	}
	return current
}

// splitDotPath splits a dot-separated path into parts.
func splitDotPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			parts = append(parts, path[start:i])
			start = i + 1
		}
	}
	parts = append(parts, path[start:])
	return parts
}
