package openshift

import "testing"

func TestInfraSettingsDefaults(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "provision", "region": "us-east-1"})
	s := NewProvider().InfraSettings(cc)

	if s.StorageClass != "gp3-csi" {
		t.Errorf("StorageClass = %q, want gp3-csi", s.StorageClass)
	}
	if s.NeedsMetalLB {
		t.Error("NeedsMetalLB = true, want false (ROSA has cloud LB)")
	}
	if s.SupportsLocalGitOps {
		t.Error("SupportsLocalGitOps = true, want false (cloud cluster)")
	}
	if s.LoadBalancerAnnotations["service.beta.kubernetes.io/aws-load-balancer-type"] != "nlb" {
		t.Errorf("aws-load-balancer-type = %q, want nlb",
			s.LoadBalancerAnnotations["service.beta.kubernetes.io/aws-load-balancer-type"])
	}
}

func TestInfraSettingsStorageClassOverride(t *testing.T) {
	cc := clusterConfig(map[string]any{"mode": "existing", "context": "c", "storage_class": "managed-csi"})
	s := NewProvider().InfraSettings(cc)
	if s.StorageClass != "managed-csi" {
		t.Errorf("StorageClass = %q, want managed-csi", s.StorageClass)
	}
}
