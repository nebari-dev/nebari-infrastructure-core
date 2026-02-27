package hetzner

import (
	"strings"
	"testing"
)

func TestGenerateClusterYAML(t *testing.T) {
	cfg := Config{
		Location:          "ash",
		KubernetesVersion: "1.32",
		MastersPool: MastersPool{
			InstanceType:  "cpx21",
			InstanceCount: 1,
		},
		WorkerNodePools: []WorkerNodePool{
			{Name: "workers", InstanceType: "cpx31", InstanceCount: 2},
		},
	}

	params := clusterParams{
		ClusterName:    "test-cluster",
		K3sVersion:     "v1.32.12+k3s1",
		HetznerToken:   "test-token",
		SSHPublicKey:   "/tmp/key.pub",
		SSHPrivateKey:  "/tmp/key",
		KubeconfigPath: "/tmp/kubeconfig",
		Config:         cfg,
	}

	yaml, err := generateClusterYAML(params)
	if err != nil {
		t.Fatalf("generateClusterYAML() error = %v", err)
	}

	// Verify key fields are present
	checks := []string{
		"cluster_name: test-cluster",
		"k3s_version: v1.32.12+k3s1",
		"instance_type: cpx21",
		"instance_count: 1",
		"instance_type: cpx31",
		"instance_count: 2",
		"location: ash",
		"kubeconfig_path:",
		"traefik:",
		"enabled: false",
	}
	for _, check := range checks {
		if !strings.Contains(yaml, check) {
			t.Errorf("generated YAML missing %q", check)
		}
	}

	// Should contain the token
	if !strings.Contains(yaml, "test-token") {
		t.Error("expected hetzner_token in generated YAML")
	}
}

func TestGenerateClusterYAML_WithAutoscaling(t *testing.T) {
	cfg := Config{
		Location:          "ash",
		KubernetesVersion: "1.32",
		MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
		WorkerNodePools: []WorkerNodePool{
			{
				Name: "gpu", InstanceType: "ccx33", InstanceCount: 1,
				Autoscaling: &Autoscaling{Enabled: true, MinInstances: 1, MaxInstances: 5},
			},
		},
	}

	params := clusterParams{
		ClusterName:    "test",
		K3sVersion:     "v1.32.12+k3s1",
		HetznerToken:   "tok",
		SSHPublicKey:   "/tmp/key.pub",
		SSHPrivateKey:  "/tmp/key",
		KubeconfigPath: "/tmp/kubeconfig",
		Config:         cfg,
	}

	yaml, err := generateClusterYAML(params)
	if err != nil {
		t.Fatalf("generateClusterYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "min_instances: 1") {
		t.Error("expected autoscaling min_instances")
	}
	if !strings.Contains(yaml, "max_instances: 5") {
		t.Error("expected autoscaling max_instances")
	}
}

func TestGenerateClusterYAML_WorkerLocationFallback(t *testing.T) {
	cfg := Config{
		Location:          "ash",
		KubernetesVersion: "1.32",
		MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
		WorkerNodePools: []WorkerNodePool{
			{Name: "w1", InstanceType: "cpx31", InstanceCount: 1},
			{Name: "w2", InstanceType: "cpx31", InstanceCount: 1, Location: "fsn1"},
		},
	}

	params := clusterParams{
		ClusterName:    "test",
		K3sVersion:     "v1.32.12+k3s1",
		HetznerToken:   "tok",
		SSHPublicKey:   "/tmp/key.pub",
		SSHPrivateKey:  "/tmp/key",
		KubeconfigPath: "/tmp/kubeconfig",
		Config:         cfg,
	}

	yaml, err := generateClusterYAML(params)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	// w1 should use top-level location "ash", w2 should use "fsn1"
	if !strings.Contains(yaml, "location: ash") {
		t.Error("expected fallback location 'ash'")
	}
	if !strings.Contains(yaml, "location: fsn1") {
		t.Error("expected explicit location 'fsn1'")
	}
}
