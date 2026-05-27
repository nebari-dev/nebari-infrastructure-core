package aws

import (
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// Compile-time interface compliance check
var _ provider.Provider = (*Provider)(nil)

// TestProviderName tests the Name method
func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "aws" {
		t.Errorf("expected provider name to be 'aws', got %s", provider.Name())
	}
}

// TestNewProvider tests provider creation
func TestNewProvider(t *testing.T) {
	provider := NewProvider()
	if provider == nil {
		t.Fatal("expected provider to be non-nil")
	}
}

func TestInfraSettings(t *testing.T) {
	p := NewProvider()
	cfg := &config.ClusterConfig{
		Providers: map[string]any{"aws": map[string]any{}},
	}

	settings := p.InfraSettings(cfg)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"StorageClass", settings.StorageClass, "longhorn"},
		{"NeedsMetalLB", settings.NeedsMetalLB, false},
		{"KeycloakBasePath is empty", settings.KeycloakBasePath, ""},
		{"LB type annotation", settings.LoadBalancerAnnotations["service.beta.kubernetes.io/aws-load-balancer-type"], "external"},
		{"LB target-type annotation", settings.LoadBalancerAnnotations["service.beta.kubernetes.io/aws-load-balancer-nlb-target-type"], "ip"},
		{"LB scheme annotation defaults to internet-facing", settings.LoadBalancerAnnotations["service.beta.kubernetes.io/aws-load-balancer-scheme"], "internet-facing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestInfraSettings_LoadBalancerScheme(t *testing.T) {
	const schemeKey = "service.beta.kubernetes.io/aws-load-balancer-scheme"

	tests := []struct {
		name       string
		providers  map[string]any
		wantScheme string
	}{
		{
			name:       "default is internet-facing when unset",
			providers:  map[string]any{"aws": map[string]any{}},
			wantScheme: "internet-facing",
		},
		{
			name:       "explicit internet-facing",
			providers:  map[string]any{"aws": map[string]any{"load_balancer_scheme": "internet-facing"}},
			wantScheme: "internet-facing",
		},
		{
			name:       "explicit internal for private VPC deployments",
			providers:  map[string]any{"aws": map[string]any{"load_balancer_scheme": "internal"}},
			wantScheme: "internal",
		},
	}

	p := NewProvider()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := p.InfraSettings(&config.ClusterConfig{Providers: tt.providers})
			if got := settings.LoadBalancerAnnotations[schemeKey]; got != tt.wantScheme {
				t.Errorf("scheme annotation: got %q, want %q", got, tt.wantScheme)
			}
		})
	}
}
