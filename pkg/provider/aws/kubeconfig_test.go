package aws

import (
	"context"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestKubeconfigMarshaling tests YAML marshaling and unmarshaling of kubeconfig structures
func TestKubeconfigMarshaling(t *testing.T) {
	tests := []struct {
		name         string
		kubeconfig   Kubeconfig
		validateFunc func(*testing.T, []byte, *Kubeconfig)
	}{
		{
			name: "full kubeconfig structure",
			kubeconfig: Kubeconfig{
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
			},
			validateFunc: func(t *testing.T, data []byte, kubeconfig *Kubeconfig) {
				if len(data) == 0 {
					t.Error("Marshaled kubeconfig is empty")
				}

				// Verify we can unmarshal it back
				var unmarshaled Kubeconfig
				err := yaml.Unmarshal(data, &unmarshaled)
				if err != nil {
					t.Fatalf("Failed to unmarshal kubeconfig: %v", err)
				}

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
			},
		},
		{
			name: "YAML key format (kebab-case)",
			kubeconfig: Kubeconfig{
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
			},
			validateFunc: func(t *testing.T, data []byte, kubeconfig *Kubeconfig) {
				yamlStr := string(data)
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
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.kubeconfig)
			if err != nil {
				t.Fatalf("Failed to marshal kubeconfig: %v", err)
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, data, &tt.kubeconfig)
			}
		})
	}
}

// TestKubeconfigExec tests KubeconfigExec structure marshaling
func TestKubeconfigExec(t *testing.T) {
	tests := []struct {
		name         string
		exec         KubeconfigExec
		validateFunc func(*testing.T, []byte, *KubeconfigExec)
	}{
		{
			name: "exec with args",
			exec: KubeconfigExec{
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
			},
			validateFunc: func(t *testing.T, data []byte, exec *KubeconfigExec) {
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
			},
		},
		{
			name: "exec with env vars",
			exec: KubeconfigExec{
				APIVersion: "client.authentication.k8s.io/v1beta1",
				Command:    "aws",
				Args:       []string{"eks", "get-token"},
				Env: []KubeconfigEnv{
					{Name: "AWS_PROFILE", Value: "production"},
					{Name: "AWS_REGION", Value: "us-west-2"},
				},
			},
			validateFunc: func(t *testing.T, data []byte, exec *KubeconfigExec) {
				var unmarshaled KubeconfigExec
				err := yaml.Unmarshal(data, &unmarshaled)
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
			},
		},
		{
			name: "exec with empty env (omitempty)",
			exec: KubeconfigExec{
				APIVersion: "client.authentication.k8s.io/v1beta1",
				Command:    "aws",
				Args:       []string{"eks", "get-token"},
				Env:        nil,
			},
			validateFunc: func(t *testing.T, data []byte, exec *KubeconfigExec) {
				yamlStr := string(data)
				// "env:" should not appear in YAML when nil (due to omitempty)
				if containsSubstring([]string{yamlStr}, "env:") {
					t.Error("YAML should not contain 'env:' field when Env is nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.exec)
			if err != nil {
				t.Fatalf("Failed to marshal exec: %v", err)
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, data, &tt.exec)
			}
		})
	}
}

// TestKubeconfigCluster tests KubeconfigCluster structure
func TestKubeconfigCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster KubeconfigCluster
	}{
		{
			name: "cluster with server and CA data",
			cluster: KubeconfigCluster{
				Server:                   "https://ABCD1234.gr7.us-west-2.eks.amazonaws.com",
				CertificateAuthorityData: "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCg==",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(&tt.cluster)
			if err != nil {
				t.Fatalf("Failed to marshal cluster: %v", err)
			}

			var unmarshaled KubeconfigCluster
			err = yaml.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal cluster: %v", err)
			}

			if unmarshaled.Server != tt.cluster.Server {
				t.Errorf("Server = %v, want %v", unmarshaled.Server, tt.cluster.Server)
			}
			if unmarshaled.CertificateAuthorityData != tt.cluster.CertificateAuthorityData {
				t.Errorf("CertificateAuthorityData = %v, want %v", unmarshaled.CertificateAuthorityData, tt.cluster.CertificateAuthorityData)
			}
		})
	}
}

// TestGetKubeconfig tests GetKubeconfig function
func TestGetKubeconfig(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "requires region",
			clusterName: "test-cluster",
			expectError: true,
			errorMsg:    "GetKubeconfig requires region parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider()
			ctx := context.Background()

			_, err := p.GetKubeconfig(ctx, tt.clusterName)

			if tt.expectError {
				if err == nil {
					t.Fatal("GetKubeconfig() should return error when region is not provided")
				}
				if err.Error()[:len(tt.errorMsg)] != tt.errorMsg {
					t.Errorf("Expected error message to start with %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}
