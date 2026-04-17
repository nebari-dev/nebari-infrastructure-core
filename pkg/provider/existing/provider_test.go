package existing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// Compile-time interface compliance check
var _ provider.Provider = (*Provider)(nil)

func TestProviderName(t *testing.T) {
	p := NewProvider()
	if p.Name() != "existing" {
		t.Errorf("expected provider name to be 'existing', got %s", p.Name())
	}
}

func TestNewProvider(t *testing.T) {
	p := NewProvider()
	if p == nil {
		t.Fatal("expected provider to be non-nil")
	}
}

// writeTestKubeconfig writes a minimal kubeconfig file with the given contexts.
// The first context is set as current-context. Returns the file path.
func writeTestKubeconfig(t *testing.T, contexts ...string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")

	content := "apiVersion: v1\nkind: Config\nclusters:\n"
	for _, ctx := range contexts {
		content += "- cluster:\n    server: https://localhost:6443\n  name: " + ctx + "-cluster\n"
	}
	content += "contexts:\n"
	for _, ctx := range contexts {
		content += "- context:\n    cluster: " + ctx + "-cluster\n    user: " + ctx + "-user\n  name: " + ctx + "\n"
	}
	content += "users:\n"
	for _, ctx := range contexts {
		content += "- name: " + ctx + "-user\n  user:\n    token: fake-token\n"
	}

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test kubeconfig: %v", err)
	}

	return path
}

func clusterConfig(providerCfg map[string]any) *config.ClusterConfig {
	return &config.ClusterConfig{
		Providers: map[string]any{"existing": providerCfg},
	}
}

func TestValidate(t *testing.T) {
	kubePath := writeTestKubeconfig(t, "my-context", "other-context")

	tests := []struct {
		name      string
		config    map[string]any
		wantError string
	}{
		{
			name:   "valid config",
			config: map[string]any{"kubeconfig": kubePath, "context": "my-context"},
		},
		{
			name:   "valid config with different context",
			config: map[string]any{"kubeconfig": kubePath, "context": "other-context"},
		},
		{
			name:      "missing context",
			config:    map[string]any{"kubeconfig": kubePath},
			wantError: "context is required",
		},
		{
			name:      "context not in kubeconfig",
			config:    map[string]any{"kubeconfig": kubePath, "context": "nonexistent"},
			wantError: "not found in kubeconfig",
		},
		{
			name:      "kubeconfig file not found",
			config:    map[string]any{"kubeconfig": "/no/such/file", "context": "x"},
			wantError: "failed to load kubeconfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider()
			err := p.Validate(context.Background(), "test", clusterConfig(tt.config))

			if tt.wantError == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("expected error containing %q, got: %v", tt.wantError, err)
				}
			}
		})
	}
}

func TestDeploy(t *testing.T) {
	p := NewProvider()
	cc := clusterConfig(map[string]any{"context": "my-context"})

	err := p.Deploy(context.Background(), "test", cc, provider.DeployOptions{})
	if err != nil {
		t.Errorf("expected Deploy to be no-op, got error: %v", err)
	}
}

func TestDestroy(t *testing.T) {
	p := NewProvider()
	cc := clusterConfig(map[string]any{"context": "my-context"})

	err := p.Destroy(context.Background(), "test", cc, provider.DestroyOptions{})
	if err != nil {
		t.Errorf("expected Destroy to be no-op, got error: %v", err)
	}
}

func TestGetKubeconfig(t *testing.T) {
	kubePath := writeTestKubeconfig(t, "my-context", "other-context")

	p := NewProvider()
	cc := clusterConfig(map[string]any{
		"kubeconfig": kubePath,
		"context":    "other-context",
	})

	kubeconfigBytes, err := p.GetKubeconfig(context.Background(), "test", cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(kubeconfigBytes) == 0 {
		t.Fatal("expected non-empty kubeconfig bytes")
	}

	content := string(kubeconfigBytes)
	if !strings.Contains(content, "other-context") {
		t.Error("kubeconfig should contain other-context")
	}
	if strings.Contains(content, "my-context") {
		t.Error("kubeconfig should not contain my-context")
	}
}

func TestInfraSettings(t *testing.T) {
	tests := []struct {
		name                string
		config              map[string]any
		wantStorageClass    string
		wantMetalLB         bool
		wantLBAnnotationLen int
		wantLBAnnotations   map[string]string
	}{
		{
			name:             "default storage class",
			config:           map[string]any{"context": "my-context"},
			wantStorageClass: "standard",
			wantMetalLB:      false,
		},
		{
			name:             "custom storage class",
			config:           map[string]any{"context": "my-context", "storage_class": "longhorn"},
			wantStorageClass: "longhorn",
			wantMetalLB:      false,
		},
		{
			name: "load balancer annotations",
			config: map[string]any{
				"context":       "my-context",
				"storage_class": "hcloud-volumes",
				"load_balancer_annotations": map[string]any{
					"load-balancer.hetzner.cloud/location": "ash",
				},
			},
			wantStorageClass:    "hcloud-volumes",
			wantMetalLB:         false,
			wantLBAnnotationLen: 1,
			wantLBAnnotations:   map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider()
			settings := p.InfraSettings(clusterConfig(tt.config))

			if settings.StorageClass != tt.wantStorageClass {
				t.Errorf("StorageClass = %q, want %q", settings.StorageClass, tt.wantStorageClass)
			}
			if settings.NeedsMetalLB != tt.wantMetalLB {
				t.Errorf("NeedsMetalLB = %v, want %v", settings.NeedsMetalLB, tt.wantMetalLB)
			}
			if len(settings.LoadBalancerAnnotations) != tt.wantLBAnnotationLen {
				t.Errorf("LoadBalancerAnnotations count = %d, want %d", len(settings.LoadBalancerAnnotations), tt.wantLBAnnotationLen)
			}
			for k, v := range tt.wantLBAnnotations {
				if settings.LoadBalancerAnnotations[k] != v {
					t.Errorf("LB annotation %q = %q, want %q", k, settings.LoadBalancerAnnotations[k], v)
				}
			}
		})
	}
}

func TestSummary(t *testing.T) {
	p := NewProvider()
	cc := clusterConfig(map[string]any{
		"kubeconfig":    "/custom/kubeconfig",
		"context":       "my-context",
		"storage_class": "longhorn",
	})

	summary := p.Summary(cc)

	if summary["Provider"] != "Existing Cluster" {
		t.Errorf("Provider = %q, want %q", summary["Provider"], "Existing Cluster")
	}
	if summary["Context"] != "my-context" {
		t.Errorf("Context = %q, want %q", summary["Context"], "my-context")
	}
	if summary["Kubeconfig"] != "/custom/kubeconfig" {
		t.Errorf("Kubeconfig = %q, want %q", summary["Kubeconfig"], "/custom/kubeconfig")
	}
	if summary["Storage Class"] != "longhorn" {
		t.Errorf("Storage Class = %q, want %q", summary["Storage Class"], "longhorn")
	}
}
