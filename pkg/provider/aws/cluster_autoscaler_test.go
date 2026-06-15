package aws

import "testing"

func TestClusterAutoscalerHelmValues(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		clusterName string
		checkValues map[string]any
		absentKeys  []string
	}{
		{
			name:        "populates discovery, region, service account, and image tag",
			cfg:         &Config{Region: "us-west-2", KubernetesVersion: "1.35"},
			clusterName: "my-cluster",
			checkValues: map[string]any{
				"cloudProvider":                         "aws",
				"awsRegion":                             "us-west-2",
				"autoDiscovery.clusterName":             "my-cluster",
				"rbac.serviceAccount.create":            true,
				"rbac.serviceAccount.name":              "cluster-autoscaler",
				"extraArgs.balance-similar-node-groups": true,
				"extraArgs.expander":                    "least-waste",
				"image.tag":                             "v1.35.0",
			},
		},
		{
			name:        "explicit image tag overrides derived",
			cfg:         &Config{Region: "eu-central-1", KubernetesVersion: "1.35", ClusterAutoscaler: &ClusterAutoscalerConfig{ImageTag: "v1.34.2"}},
			clusterName: "prod-eks",
			checkValues: map[string]any{
				"awsRegion":                 "eu-central-1",
				"autoDiscovery.clusterName": "prod-eks",
				"image.tag":                 "v1.34.2",
			},
		},
		{
			name:        "no kubernetes version omits image block",
			cfg:         &Config{Region: "us-west-2"},
			clusterName: "my-cluster",
			absentKeys:  []string{"image.tag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := clusterAutoscalerHelmValues(tt.cfg, tt.clusterName)

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

			for _, key := range tt.absentKeys {
				if got := getNestedValue(values, key); got != nil {
					t.Errorf("key %q expected absent, got %v", key, got)
				}
			}
		})
	}
}
