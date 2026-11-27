package config

import (
	"context"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestGatewayConfigParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "quoted quantity",
			in:   `gateway: { request_buffer_limit: "10Mi" }`,
			want: "10Mi",
		},
		{
			name: "unquoted quantity",
			in:   `gateway: { request_buffer_limit: 256Ki }`,
			want: "256Ki",
		},
		{
			// A bare integer is bytes in Envoy Gateway's quantity syntax; it
			// must survive decoding into the string field unchanged.
			name: "unquoted integer bytes",
			in:   `gateway: { request_buffer_limit: 1048576 }`,
			want: "1048576",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c NebariConfig
			if err := yaml.Unmarshal([]byte(tt.in), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if c.Gateway == nil {
				t.Fatal("gateway block not parsed")
			}
			if c.Gateway.RequestBufferLimit != tt.want {
				t.Errorf("RequestBufferLimit = %q, want %q", c.Gateway.RequestBufferLimit, tt.want)
			}
		})
	}
}

func TestGatewayConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		gateway     *GatewayConfig
		errContains string
	}{
		{name: "nil block", gateway: nil},
		{name: "empty limit uses default", gateway: &GatewayConfig{}},
		{name: "binary suffix", gateway: &GatewayConfig{RequestBufferLimit: "50Mi"}},
		{name: "decimal suffix", gateway: &GatewayConfig{RequestBufferLimit: "100M"}},
		{name: "lowercase kilo", gateway: &GatewayConfig{RequestBufferLimit: "512k"}},
		{name: "plain bytes", gateway: &GatewayConfig{RequestBufferLimit: "1048576"}},
		{name: "uint32 max", gateway: &GatewayConfig{RequestBufferLimit: "4294967295"}},
		{name: "default value", gateway: &GatewayConfig{RequestBufferLimit: DefaultGatewayRequestBufferLimit}},
		{name: "minimum 32Ki", gateway: &GatewayConfig{RequestBufferLimit: "32Ki"}},
		{name: "minimum in plain bytes", gateway: &GatewayConfig{RequestBufferLimit: "32768"}},
		{
			// The typo the floor exists for: 50 meant as 50Mi.
			name:        "bare number missing its unit",
			gateway:     &GatewayConfig{RequestBufferLimit: "50"},
			errContains: "minimum of 32768 bytes",
		},
		{
			name:        "one byte below minimum",
			gateway:     &GatewayConfig{RequestBufferLimit: "32767"},
			errContains: "minimum of 32768 bytes",
		},
		{
			name:        "decimal kilo below minimum",
			gateway:     &GatewayConfig{RequestBufferLimit: "32k"},
			errContains: "minimum of 32768 bytes",
		},
		{
			name:        "zero",
			gateway:     &GatewayConfig{RequestBufferLimit: "0"},
			errContains: "request_buffer_limit",
		},
		{
			name:        "decimal point",
			gateway:     &GatewayConfig{RequestBufferLimit: "1.5Mi"},
			errContains: "request_buffer_limit",
		},
		{
			name:        "unknown suffix",
			gateway:     &GatewayConfig{RequestBufferLimit: "50MB"},
			errContains: "request_buffer_limit",
		},
		{
			name:        "negative",
			gateway:     &GatewayConfig{RequestBufferLimit: "-1Mi"},
			errContains: "request_buffer_limit",
		},
		{
			name:        "whitespace",
			gateway:     &GatewayConfig{RequestBufferLimit: " 50Mi"},
			errContains: "request_buffer_limit",
		},
		{
			name:        "above uint32 max",
			gateway:     &GatewayConfig{RequestBufferLimit: "4Gi"},
			errContains: "4294967295",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gateway.Validate(context.Background())
			if tt.errContains == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestGatewayConfigRequestBufferLimitOrDefault(t *testing.T) {
	tests := []struct {
		name    string
		gateway *GatewayConfig
		want    string
	}{
		{name: "nil block", gateway: nil, want: DefaultGatewayRequestBufferLimit},
		{name: "unset limit", gateway: &GatewayConfig{}, want: DefaultGatewayRequestBufferLimit},
		{name: "override", gateway: &GatewayConfig{RequestBufferLimit: "25Mi"}, want: "25Mi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gateway.RequestBufferLimitOrDefault(context.Background()); got != tt.want {
				t.Errorf("RequestBufferLimitOrDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}
