package aws

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// Compile-time interface compliance check
var _ cluster.Provider = (*Provider)(nil)

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

// TestValidate_LoadBalancerScheme covers the rejection path only. Valid
// schemes flow through Validate to the AWS credential check, which would
// require live AWS credentials (or a long IMDS timeout) in this test
// environment. The default and valid-value paths are exercised by
// TestInfraSettings and TestInfraSettings_LoadBalancerScheme.
func TestValidate_LoadBalancerScheme(t *testing.T) {
	tests := []struct {
		name   string
		scheme string
	}{
		{name: "typo is rejected", scheme: "internet_facing"},
		{name: "arbitrary string is rejected", scheme: "public"},
		{name: "mixed case is rejected", scheme: "Internal"},
	}

	p := NewProvider()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{Providers: map[string]any{"aws": map[string]any{
				"region":               "us-west-2",
				"kubernetes_version":   "1.34",
				"load_balancer_scheme": tt.scheme,
				"node_groups": map[string]any{
					"general": map[string]any{"instance": "m5.large"},
				},
			}}}

			err := p.Validate(context.Background(), "test-project", cfg)
			if err == nil {
				t.Fatalf("expected error for scheme %q, got nil", tt.scheme)
			}
			if !strings.Contains(err.Error(), "load_balancer_scheme") {
				t.Fatalf("expected error to mention load_balancer_scheme, got: %v", err)
			}
		})
	}
}

// TestValidateTaints verifies node group taint effects are validated against
// the EKS API enum (NO_SCHEDULE/NO_EXECUTE/PREFER_NO_SCHEDULE), not the
// Kubernetes-style spelling, and that a missing key is rejected. It exercises
// the helper directly so the valid cases don't fall through Validate to the
// AWS credential check (which would block on an IMDS timeout).
func TestValidateTaints(t *testing.T) {
	tests := []struct {
		name      string
		taints    []Taint
		errSubstr string // "" means no error expected
	}{
		{name: "EKS NO_SCHEDULE is accepted", taints: []Taint{{Key: "k", Value: "v", Effect: "NO_SCHEDULE"}}},
		{name: "EKS NO_EXECUTE is accepted", taints: []Taint{{Key: "k", Effect: "NO_EXECUTE"}}},
		{name: "EKS PREFER_NO_SCHEDULE is accepted", taints: []Taint{{Key: "k", Effect: "PREFER_NO_SCHEDULE"}}},
		{name: "no taints is fine", taints: nil},
		{name: "k8s-style NoSchedule is rejected", taints: []Taint{{Key: "k", Effect: "NoSchedule"}}, errSubstr: "invalid effect"},
		{name: "arbitrary effect is rejected", taints: []Taint{{Key: "k", Effect: "Garbage"}}, errSubstr: "invalid effect"},
		{name: "missing key is rejected", taints: []Taint{{Effect: "NO_SCHEDULE"}}, errSubstr: "missing key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTaints("storage", tt.taints)
			if tt.errSubstr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
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
