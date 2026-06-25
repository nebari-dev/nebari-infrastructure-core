package openshift

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

func TestConfigDefaults(t *testing.T) {
	raw := map[string]any{"mode": "provision", "region": "us-east-1"}
	var c Config
	if err := config.UnmarshalProviderConfig(context.Background(), raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Mode() != ModeProvision {
		t.Errorf("Mode() = %q, want %q", c.Mode(), ModeProvision)
	}
	if got := c.StorageClassOrDefault(); got != "gp3-csi" {
		t.Errorf("StorageClassOrDefault() = %q, want gp3-csi", got)
	}
	if c.LonghornEnabled() {
		t.Error("LonghornEnabled() = true, want false by default")
	}
}

func TestConfigModeDefaultsToProvision(t *testing.T) {
	var c Config
	if c.Mode() != ModeProvision {
		t.Errorf("empty Mode() = %q, want %q", c.Mode(), ModeProvision)
	}
}

func TestConfigExistingMode(t *testing.T) {
	raw := map[string]any{
		"mode":          "existing",
		"kubeconfig":    "/tmp/kc",
		"context":       "my-ctx",
		"storage_class": "managed-csi",
		"longhorn":      map[string]any{"enabled": true},
	}
	var c Config
	if err := config.UnmarshalProviderConfig(context.Background(), raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Mode() != ModeExisting {
		t.Errorf("Mode() = %q, want existing", c.Mode())
	}
	if c.Context != "my-ctx" {
		t.Errorf("Context = %q, want my-ctx", c.Context)
	}
	if got := c.StorageClassOrDefault(); got != "managed-csi" {
		t.Errorf("StorageClassOrDefault() = %q, want managed-csi", got)
	}
	if !c.LonghornEnabled() {
		t.Error("LonghornEnabled() = false, want true")
	}
}

func TestConfigProvisionComputeParsing(t *testing.T) {
	raw := map[string]any{
		"mode":   "provision",
		"region": "us-east-1",
		"compute": map[string]any{
			"instance_type": "m5.xlarge",
			"replicas":      2,
		},
		"availability_zones": []any{"us-east-1a"},
	}
	var c Config
	if err := config.UnmarshalProviderConfig(context.Background(), raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Compute.InstanceType != "m5.xlarge" {
		t.Errorf("InstanceType = %q, want m5.xlarge", c.Compute.InstanceType)
	}
	if c.Compute.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", c.Compute.Replicas)
	}
	if len(c.AvailabilityZones) != 1 || c.AvailabilityZones[0] != "us-east-1a" {
		t.Errorf("AvailabilityZones = %v, want [us-east-1a]", c.AvailabilityZones)
	}
}
