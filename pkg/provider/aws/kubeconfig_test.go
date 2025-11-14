package aws

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKubeconfigStructure(t *testing.T) {
	// Test that we can create a valid kubeconfig structure
	kubeconfig := Kubeconfig{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []KubeconfigNamedCluster{
			{
				Name: "test-cluster",
				Cluster: KubeconfigCluster{
					Server:                   "https://test.eks.amazonaws.com",
					CertificateAuthorityData: "LS0tLS1CRUdJTi==",
				},
			},
		},
		Contexts: []KubeconfigNamedContext{
			{
				Name: "test-cluster",
				Context: KubeconfigContext{
					Cluster: "test-cluster",
					User:    "test-cluster",
				},
			},
		},
		CurrentContext: "test-cluster",
		Users: []KubeconfigNamedUser{
			{
				Name: "test-cluster",
				User: KubeconfigUser{
					Exec: KubeconfigExec{
						APIVersion: "client.authentication.k8s.io/v1beta1",
						Command:    "aws",
						Args: []string{
							"eks",
							"get-token",
							"--cluster-name",
							"test-cluster",
							"--region",
							"us-west-2",
						},
					},
				},
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&kubeconfig)
	if err != nil {
		t.Fatalf("Failed to marshal kubeconfig: %v", err)
	}

	if len(data) == 0 {
		t.Error("Marshaled kubeconfig is empty")
	}

	// Verify we can unmarshal it back
	var unmarshaled Kubeconfig
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal kubeconfig: %v", err)
	}

	// Verify key fields
	if unmarshaled.APIVersion != "v1" {
		t.Errorf("APIVersion = %v, want v1", unmarshaled.APIVersion)
	}

	if unmarshaled.Kind != "Config" {
		t.Errorf("Kind = %v, want Config", unmarshaled.Kind)
	}

	if len(unmarshaled.Clusters) != 1 {
		t.Errorf("Clusters length = %v, want 1", len(unmarshaled.Clusters))
	}

	if len(unmarshaled.Contexts) != 1 {
		t.Errorf("Contexts length = %v, want 1", len(unmarshaled.Contexts))
	}

	if len(unmarshaled.Users) != 1 {
		t.Errorf("Users length = %v, want 1", len(unmarshaled.Users))
	}

	if unmarshaled.CurrentContext != "test-cluster" {
		t.Errorf("CurrentContext = %v, want test-cluster", unmarshaled.CurrentContext)
	}
}

func TestKubeconfigExecFormat(t *testing.T) {
	exec := KubeconfigExec{
		APIVersion: "client.authentication.k8s.io/v1beta1",
		Command:    "aws",
		Args: []string{
			"eks",
			"get-token",
			"--cluster-name",
			"my-cluster",
			"--region",
			"us-east-1",
		},
	}

	data, err := yaml.Marshal(&exec)
	if err != nil {
		t.Fatalf("Failed to marshal exec: %v", err)
	}

	// Verify the YAML contains expected fields
	yamlStr := string(data)
	expectedStrings := []string{
		"apiVersion:",
		"client.authentication.k8s.io/v1beta1",
		"command: aws",
		"args:",
		"eks",
		"get-token",
		"--cluster-name",
		"my-cluster",
		"--region",
		"us-east-1",
	}

	for _, expected := range expectedStrings {
		if !containsSubstring([]string{yamlStr}, expected) {
			t.Errorf("YAML should contain %q", expected)
		}
	}
}

func TestKubeconfigWithEnvVars(t *testing.T) {
	exec := KubeconfigExec{
		APIVersion: "client.authentication.k8s.io/v1beta1",
		Command:    "aws",
		Args:       []string{"eks", "get-token"},
		Env: []KubeconfigEnv{
			{Name: "AWS_PROFILE", Value: "production"},
			{Name: "AWS_REGION", Value: "us-west-2"},
		},
	}

	data, err := yaml.Marshal(&exec)
	if err != nil {
		t.Fatalf("Failed to marshal exec with env: %v", err)
	}

	var unmarshaled KubeconfigExec
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal exec: %v", err)
	}

	if len(unmarshaled.Env) != 2 {
		t.Errorf("Env length = %v, want 2", len(unmarshaled.Env))
	}

	if unmarshaled.Env[0].Name != "AWS_PROFILE" {
		t.Errorf("Env[0].Name = %v, want AWS_PROFILE", unmarshaled.Env[0].Name)
	}

	if unmarshaled.Env[0].Value != "production" {
		t.Errorf("Env[0].Value = %v, want production", unmarshaled.Env[0].Value)
	}
}

func TestKubeconfigEmptyEnv(t *testing.T) {
	// Test that empty env array is omitted from YAML
	exec := KubeconfigExec{
		APIVersion: "client.authentication.k8s.io/v1beta1",
		Command:    "aws",
		Args:       []string{"eks", "get-token"},
		Env:        nil,
	}

	data, err := yaml.Marshal(&exec)
	if err != nil {
		t.Fatalf("Failed to marshal exec: %v", err)
	}

	yamlStr := string(data)

	// "env:" should not appear in YAML when nil (due to omitempty)
	if containsSubstring([]string{yamlStr}, "env:") {
		t.Error("YAML should not contain 'env:' field when Env is nil")
	}
}

func TestKubeconfigClusterFields(t *testing.T) {
	cluster := KubeconfigCluster{
		Server:                   "https://ABCD1234.gr7.us-west-2.eks.amazonaws.com",
		CertificateAuthorityData: "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCg==",
	}

	data, err := yaml.Marshal(&cluster)
	if err != nil {
		t.Fatalf("Failed to marshal cluster: %v", err)
	}

	var unmarshaled KubeconfigCluster
	err = yaml.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal cluster: %v", err)
	}

	if unmarshaled.Server != cluster.Server {
		t.Errorf("Server = %v, want %v", unmarshaled.Server, cluster.Server)
	}

	if unmarshaled.CertificateAuthorityData != cluster.CertificateAuthorityData {
		t.Errorf("CertificateAuthorityData = %v, want %v", unmarshaled.CertificateAuthorityData, cluster.CertificateAuthorityData)
	}
}

func TestKubeconfigYAMLKeys(t *testing.T) {
	// Test that YAML keys use the correct format (kebab-case for Kubernetes)
	kubeconfig := Kubeconfig{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []KubeconfigNamedCluster{
			{
				Name: "test",
				Cluster: KubeconfigCluster{
					Server:                   "https://test.eks.aws",
					CertificateAuthorityData: "test-ca-data",
				},
			},
		},
		Contexts: []KubeconfigNamedContext{
			{
				Name: "test",
				Context: KubeconfigContext{
					Cluster: "test",
					User:    "test",
				},
			},
		},
		CurrentContext: "test",
		Users: []KubeconfigNamedUser{
			{
				Name: "test",
				User: KubeconfigUser{
					Exec: KubeconfigExec{
						APIVersion: "client.authentication.k8s.io/v1beta1",
						Command:    "aws",
						Args:       []string{"test"},
					},
				},
			},
		},
	}

	data, err := yaml.Marshal(&kubeconfig)
	if err != nil {
		t.Fatalf("Failed to marshal kubeconfig: %v", err)
	}

	yamlStr := string(data)

	// Verify correct YAML key format
	expectedKeys := []string{
		"apiVersion:",
		"kind:",
		"clusters:",
		"contexts:",
		"current-context:",
		"users:",
		"server:",
		"certificate-authority-data:",
	}

	for _, key := range expectedKeys {
		if !containsSubstring([]string{yamlStr}, key) {
			t.Errorf("YAML should contain key %q", key)
		}
	}
}
