package local

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// Compile-time interface compliance check
var _ cluster.Provider = (*Provider)(nil)

func TestInfraSettings(t *testing.T) {
	p := NewProvider()

	tests := []struct {
		name           string
		providerConfig map[string]any
		wantSC         string
		wantHTTPSPort  int
	}{
		{
			name:           "no local config block returns defaults",
			providerConfig: nil,
			wantSC:         "standard",
			wantHTTPSPort:  0,
		},
		{
			name:           "empty local config returns defaults",
			providerConfig: map[string]any{"local": map[string]any{}},
			wantSC:         "standard",
			wantHTTPSPort:  0,
		},
		{
			name: "https_port override",
			providerConfig: map[string]any{
				"local": map[string]any{"https_port": 8443},
			},
			wantSC:        "standard",
			wantHTTPSPort: 8443,
		},
		{
			name: "unmarshal error returns defaults",
			providerConfig: map[string]any{
				"local": "not-a-map",
			},
			wantSC:        "standard",
			wantHTTPSPort: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{
				Providers: tt.providerConfig,
			}

			settings := p.InfraSettings(cfg)

			if settings.StorageClass != tt.wantSC {
				t.Errorf("StorageClass = %q, want %q", settings.StorageClass, tt.wantSC)
			}
			if !settings.GatewayHostPorts {
				t.Error("GatewayHostPorts = false, want true")
			}
			if settings.HTTPSPort != tt.wantHTTPSPort {
				t.Errorf("HTTPSPort = %d, want %d", settings.HTTPSPort, tt.wantHTTPSPort)
			}
			// Fields not set by local provider should always be zero values
			if len(settings.LoadBalancerAnnotations) != 0 {
				t.Errorf("LoadBalancerAnnotations = %v, want empty", settings.LoadBalancerAnnotations)
			}
			if settings.KeycloakBasePath != "" {
				t.Errorf("KeycloakBasePath = %q, want empty", settings.KeycloakBasePath)
			}
			if !settings.SupportsLocalGitOps {
				t.Error("SupportsLocalGitOps = false, want true")
			}
		})
	}
}

func TestValidateKindMode(t *testing.T) {
	p := NewProvider()
	ctx := context.Background()

	tests := []struct {
		name           string
		providerConfig map[string]any
		wantErr        string
	}{
		{
			name:           "no config block is valid (kind with defaults)",
			providerConfig: nil,
		},
		{
			name: "empty kind block is valid",
			providerConfig: map[string]any{
				"local": map[string]any{"kind": map[string]any{}},
			},
		},
		{
			name: "kind with node_image and mounts is valid",
			providerConfig: map[string]any{
				"local": map[string]any{
					"kind": map[string]any{
						"node_image": "kindest/node:v1.32.2",
						"extra_mounts": []any{
							map[string]any{
								"host_path":      "/tmp/data",
								"container_path": "/data",
								"read_only":      true,
							},
						},
					},
				},
			},
		},
		{
			name: "relative mount paths are rejected",
			providerConfig: map[string]any{
				"local": map[string]any{
					"kind": map[string]any{
						"extra_mounts": []any{
							map[string]any{
								"host_path":      "data",
								"container_path": "/data",
							},
						},
					},
				},
			},
			wantErr: "must be absolute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{Providers: tt.providerConfig}

			err := p.Validate(ctx, "test-project", cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate returned nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSummaryKindMode(t *testing.T) {
	p := NewProvider()

	cfg := &config.ClusterConfig{
		Providers: map[string]any{
			"local": map[string]any{
				"kind": map[string]any{"node_image": "kindest/node:v1.32.2"},
			},
		},
	}

	summary := p.Summary(cfg)
	if summary["Kind Cluster"] == "" {
		t.Error("Summary missing Kind Cluster entry for managed mode")
	}
	if summary["Kind Node Image"] != "kindest/node:v1.32.2" {
		t.Errorf("Kind Node Image = %q, want kindest/node:v1.32.2", summary["Kind Node Image"])
	}
}
