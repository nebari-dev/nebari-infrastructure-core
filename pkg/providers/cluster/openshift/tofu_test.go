package openshift

import "testing"

func TestToTFVars(t *testing.T) {
	c := &Config{
		Region:            "us-east-1",
		OpenShiftVersion:  "4.20.25",
		AvailabilityZones: []string{"us-east-1a"},
		Compute:           Compute{InstanceType: "m5.xlarge", Replicas: 2},
		MachineCIDR:       "10.0.0.0/16",
	}
	v, err := c.toTFVars("nebari-ocp-poc")
	if err != nil {
		t.Fatalf("toTFVars: %v", err)
	}
	if v.ClusterName != "nebari-ocp-poc" {
		t.Errorf("ClusterName = %q, want nebari-ocp-poc", v.ClusterName)
	}
	if v.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", v.Region)
	}
	if v.ComputeMachineType != "m5.xlarge" {
		t.Errorf("ComputeMachineType = %q, want m5.xlarge", v.ComputeMachineType)
	}
	if v.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", v.Replicas)
	}
	if len(v.AvailabilityZones) != 1 || v.AvailabilityZones[0] != "us-east-1a" {
		t.Errorf("AvailabilityZones = %v, want [us-east-1a]", v.AvailabilityZones)
	}
}

// TestTemplatesEmbedded ensures the .tf files are embedded (so tofu.Setup can
// extract them at runtime).
func TestTemplatesEmbedded(t *testing.T) {
	for _, f := range []string{"templates/main.tf", "templates/provider.tf", "templates/variables.tf", "templates/outputs.tf", "templates/backend.tf"} {
		if _, err := tofuTemplates.ReadFile(f); err != nil {
			t.Errorf("expected embedded %s: %v", f, err)
		}
	}
}
