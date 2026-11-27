package argocd

import (
	"context"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

const clientTrafficPolicyPath = "manifests/networking/clienttrafficpolicy.yaml"

// TestClientTrafficPolicyBufferLimit pins the gateway request buffer limit
// (#389). Envoy AI Gateway buffers the whole request body to read the model
// name, and Envoy Gateway's 32Ki per-connection default rejects realistic LLM
// client payloads with HTTP 413 before they reach the backend.
func TestClientTrafficPolicyBufferLimit(t *testing.T) {
	tests := []struct {
		name    string
		gateway *config.GatewayConfig
		want    string
	}{
		{name: "no gateway block renders the default", gateway: nil, want: config.DefaultGatewayRequestBufferLimit},
		{name: "unset limit renders the default", gateway: &config.GatewayConfig{}, want: config.DefaultGatewayRequestBufferLimit},
		{name: "override is rendered", gateway: &config.GatewayConfig{RequestBufferLimit: "25Mi"}, want: "25Mi"},
		{name: "plain bytes are rendered as a string", gateway: &config.GatewayConfig{RequestBufferLimit: "1048576"}, want: "1048576"},
	}

	content, err := templates.ReadFile("templates/" + clientTrafficPolicyPath)
	if err != nil {
		t.Fatalf("read client traffic policy template: %v", err)
	}
	// Compare against the rendered Gateway's name, not a literal, so renaming
	// the Gateway without the policy following it fails here: a policy whose
	// target does not resolve is silently inert and the 413s come back.
	gatewayName := extractYAMLName(t, "templates/manifests/networking/gateway.yaml")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{ProjectName: "p", Domain: "test.example.com", Gateway: tt.gateway}
			data := NewTemplateData(context.Background(), cfg, nil, cluster.InfraSettings{})

			processed, err := processTemplate(clientTrafficPolicyPath, content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			var got struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name      string `yaml:"name"`
					Namespace string `yaml:"namespace"`
				} `yaml:"metadata"`
				Spec struct {
					TargetRefs []struct {
						Group string `yaml:"group"`
						Kind  string `yaml:"kind"`
						Name  string `yaml:"name"`
					} `yaml:"targetRefs"`
					Connection struct {
						BufferLimit any `yaml:"bufferLimit"`
					} `yaml:"connection"`
				} `yaml:"spec"`
			}
			if err := yaml.Unmarshal(processed, &got); err != nil {
				t.Fatalf("rendered policy is not valid YAML: %v\n%s", err, processed)
			}

			if got.Kind != "ClientTrafficPolicy" {
				t.Errorf("kind = %q, want ClientTrafficPolicy", got.Kind)
			}
			// A policy must live in its target Gateway's namespace.
			if got.Metadata.Namespace != "envoy-gateway-system" {
				t.Errorf("namespace = %q, want envoy-gateway-system", got.Metadata.Namespace)
			}
			if len(got.Spec.TargetRefs) != 1 {
				t.Fatalf("targetRefs = %+v, want exactly one", got.Spec.TargetRefs)
			}
			ref := got.Spec.TargetRefs[0]
			if ref.Group != "gateway.networking.k8s.io" || ref.Kind != "Gateway" || ref.Name != gatewayName {
				t.Errorf("targetRef = %+v, want gateway.networking.k8s.io/Gateway %s", ref, gatewayName)
			}
			// The CRD accepts an int or a string; render a string so a
			// plain-bytes value is not reinterpreted as a YAML number.
			s, ok := got.Spec.Connection.BufferLimit.(string)
			if !ok {
				t.Fatalf("bufferLimit = %#v (%T), want a string", got.Spec.Connection.BufferLimit, got.Spec.Connection.BufferLimit)
			}
			if s != tt.want {
				t.Errorf("bufferLimit = %q, want %q", s, tt.want)
			}
		})
	}
}
