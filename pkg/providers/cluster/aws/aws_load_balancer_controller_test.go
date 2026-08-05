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
				"keepTLSSecret":         true,
			},
		},
		{
			name:        "disables the Gateway API feature gates",
			cfg:         &Config{Region: "us-west-2"},
			clusterName: "my-cluster",
			vpcID:       "vpc-abc123",
			checkValues: map[string]any{
				"controllerConfig.featureGates.ALBGatewayAPI": false,
				"controllerConfig.featureGates.NLBGatewayAPI": false,
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

// TestAWSLoadBalancerControllerHelmValuesOmitsListenerSetGate holds the
// deliberate omission: GatewayListenerSet is a fatal unknown flag before chart
// 3.2.2, and upstream reads it only when ALBGatewayAPI or NLBGatewayAPI is
// enabled, so setting it would break old pins and buy nothing (#383).
func TestAWSLoadBalancerControllerHelmValuesOmitsListenerSetGate(t *testing.T) {
	values := awsLoadBalancerControllerHelmValues(&Config{Region: "us-west-2"}, "my-cluster", "vpc-abc123")

	if got := getNestedValue(values, "controllerConfig.featureGates.GatewayListenerSet"); got != nil {
		t.Errorf("GatewayListenerSet must not be set, got %v", got)
	}
}
