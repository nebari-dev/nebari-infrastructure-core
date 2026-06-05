package aws

import (
	"context"
	"strings"
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

// TestValidate_TaintEffect verifies node group taint effects are validated
// against the EKS API enum (NO_SCHEDULE/NO_EXECUTE/PREFER_NO_SCHEDULE), not the
// Kubernetes-style spelling. A valid effect flows past the taint check to the
// AWS credential check (so its error, if any, must not mention the effect);
// an invalid effect is rejected at the taint check.
func TestValidate_TaintEffect(t *testing.T) {
	tests := []struct {
		name          string
		effect        string
		wantEffectErr bool
	}{
		{name: "EKS NO_SCHEDULE is accepted", effect: "NO_SCHEDULE", wantEffectErr: false},
		{name: "EKS NO_EXECUTE is accepted", effect: "NO_EXECUTE", wantEffectErr: false},
		{name: "EKS PREFER_NO_SCHEDULE is accepted", effect: "PREFER_NO_SCHEDULE", wantEffectErr: false},
		{name: "k8s-style NoSchedule is rejected", effect: "NoSchedule", wantEffectErr: true},
		{name: "arbitrary string is rejected", effect: "Garbage", wantEffectErr: true},
	}

	p := NewProvider()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{Providers: map[string]any{"aws": map[string]any{
				"region":             "us-west-2",
				"kubernetes_version": "1.34",
				"node_groups": map[string]any{
					"storage": map[string]any{
						"instance": "m7g.large",
						"taints": []any{
							map[string]any{"key": "node.longhorn.io/storage", "value": "true", "effect": tt.effect},
						},
					},
				},
			}}}

			err := p.Validate(context.Background(), "test-project", cfg)
			mentionsEffect := err != nil && strings.Contains(err.Error(), "invalid effect")
			if mentionsEffect != tt.wantEffectErr {
				t.Fatalf("effect %q: invalid-effect error = %v (err=%v), want %v", tt.effect, mentionsEffect, err, tt.wantEffectErr)
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
