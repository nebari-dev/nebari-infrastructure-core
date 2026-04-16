package aws

import "testing"

func TestAWSLoadBalancerControllerHelmValues(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		clusterName string
		vpcID       string
		checkValues map[string]any
	}{
		{
			name:        "populates cluster, region, vpc, and service account",
			cfg:         &Config{Region: "us-west-2"},
			clusterName: "my-cluster",
			vpcID:       "vpc-abc123",
			checkValues: map[string]any{
				"clusterName":           "my-cluster",
				"region":                "us-west-2",
				"vpcId":                 "vpc-abc123",
				"serviceAccount.create": true,
				"serviceAccount.name":   "aws-load-balancer-controller",
			},
		},
		{
			name:        "different region and cluster",
			cfg:         &Config{Region: "eu-central-1"},
			clusterName: "prod-eks",
			vpcID:       "vpc-xyz789",
			checkValues: map[string]any{
				"clusterName": "prod-eks",
				"region":      "eu-central-1",
				"vpcId":       "vpc-xyz789",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := awsLoadBalancerControllerHelmValues(tt.cfg, tt.clusterName, tt.vpcID)

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
