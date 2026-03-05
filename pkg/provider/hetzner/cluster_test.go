package hetzner

import (
	"os"
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
		SSHPublicKey:   "/tmp/key.pub",
		SSHPrivateKey:  "/tmp/key",
		KubeconfigPath: "/tmp/kubeconfig",
		Config:         cfg,
	}

	yaml, err := generateClusterYAML(params)
	if err != nil {
		t.Fatalf("generateClusterYAML() error = %v", err)
	}

	// Verify key fields are present (string values are quoted in the template)
	checks := []string{
		`cluster_name: "test-cluster"`,
		`k3s_version: "v1.32.12+k3s1"`,
		`instance_type: "cpx21"`,
		"instance_count: 1",
		`instance_type: "cpx31"`,
		"instance_count: 2",
		`location: "ash"`,
		"kubeconfig_path:",
		"traefik:",
		"enabled: false",
	}
	for _, check := range checks {
		if !strings.Contains(yaml, check) {
			t.Errorf("generated YAML missing %q", check)
		}
	}

	// Token should NOT be in the generated YAML (passed via HCLOUD_TOKEN env var)
	if strings.Contains(yaml, "hetzner_token") {
		t.Error("cluster.yaml should not contain hetzner_token - token is passed via env var")
	}

	// Default network config should use quoted 0.0.0.0/0
	if !strings.Contains(yaml, `"0.0.0.0/0"`) {
		t.Error("expected quoted default 0.0.0.0/0 in allowed_networks")
	}
}

func TestGenerateClusterYAML_CustomNetwork(t *testing.T) {
	cfg := Config{
		Location:          "ash",
		KubernetesVersion: "1.32",
		MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
		WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
		Network: &NetworkConfig{
			SSHAllowedCIDRs: []string{"10.0.0.0/8"},
			APIAllowedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
	}

	params := clusterParams{
		ClusterName:    "test",
		K3sVersion:     "v1.32.12+k3s1",
		SSHPublicKey:   "/tmp/key.pub",
		SSHPrivateKey:  "/tmp/key",
		KubeconfigPath: "/tmp/kubeconfig",
		Config:         cfg,
	}

	yaml, err := generateClusterYAML(params)
	if err != nil {
		t.Fatalf("generateClusterYAML() error = %v", err)
	}

	if strings.Contains(yaml, "0.0.0.0/0") {
		t.Error("custom network config should NOT contain 0.0.0.0/0")
	}
	if !strings.Contains(yaml, `"10.0.0.0/8"`) {
		t.Error("expected quoted custom SSH CIDR 10.0.0.0/8")
	}
	if !strings.Contains(yaml, `"192.168.0.0/16"`) {
		t.Error("expected quoted custom API CIDR 192.168.0.0/16")
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
	if !strings.Contains(yaml, `location: "ash"`) {
		t.Error("expected fallback location 'ash'")
	}
	if !strings.Contains(yaml, `location: "fsn1"`) {
		t.Error("expected explicit location 'fsn1'")
	}
}

func TestMinimalEnv(t *testing.T) {
	// Set a known HETZNER_TOKEN and an unrelated secret
	t.Setenv("HETZNER_TOKEN", "test-token-123")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "should-not-leak")
	t.Setenv("CLOUDFLARE_API_TOKEN", "should-not-leak-either")

	env := minimalEnv()

	envMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// HCLOUD_TOKEN should be set from HETZNER_TOKEN
	if envMap["HCLOUD_TOKEN"] != "test-token-123" {
		t.Errorf("HCLOUD_TOKEN = %q, want %q", envMap["HCLOUD_TOKEN"], "test-token-123")
	}

	// PATH should be forwarded
	if _, ok := envMap["PATH"]; !ok {
		t.Error("PATH should be present in minimal env")
	}

	// Unrelated secrets must NOT be present
	if _, ok := envMap["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Error("AWS_SECRET_ACCESS_KEY should not leak into subprocess env")
	}
	if _, ok := envMap["CLOUDFLARE_API_TOKEN"]; ok {
		t.Error("CLOUDFLARE_API_TOKEN should not leak into subprocess env")
	}
}

func TestMinimalEnv_MissingToken(t *testing.T) {
	// Unset HETZNER_TOKEN to verify HCLOUD_TOKEN is set but empty
	t.Setenv("HETZNER_TOKEN", "")

	env := minimalEnv()

	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "HCLOUD_TOKEN=") {
			found = true
			if e != "HCLOUD_TOKEN=" {
				t.Errorf("expected empty HCLOUD_TOKEN, got %q", e)
			}
		}
	}
	if !found {
		t.Error("HCLOUD_TOKEN should always be present in env")
	}
}

func TestWriteClusterYAML(t *testing.T) {
	dir := t.TempDir()
	content := "test: content"

	path, err := writeClusterYAML(dir, content)
	if err != nil {
		t.Fatalf("writeClusterYAML() error = %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // Test file, path from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != content {
		t.Errorf("content = %q, want %q", string(data), content)
	}

	// Verify restrictive permissions
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", info.Mode().Perm())
	}
}
