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
			if settings.GatewayHostAddress != "127.0.0.1" {
				t.Errorf("GatewayHostAddress = %q, want 127.0.0.1", settings.GatewayHostAddress)
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
			name: "kind with workers is valid",
			providerConfig: map[string]any{
				"local": map[string]any{"kind": map[string]any{"workers": 2}},
			},
		},
		{
			name: "negative workers are rejected",
			providerConfig: map[string]any{
				"local": map[string]any{"kind": map[string]any{"workers": -1}},
			},
			wantErr: "kind workers must be 0 or more",
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
		{
			name: "https_port above the port range is rejected",
			providerConfig: map[string]any{
				"local": map[string]any{"https_port": 99999999},
			},
			wantErr: "https_port must be between 1 and 65535",
		},
		{
			name: "negative http_port is rejected",
			providerConfig: map[string]any{
				"local": map[string]any{"http_port": -1},
			},
			wantErr: "http_port must be between 1 and 65535",
		},
		{
			name: "valid custom ports pass",
			providerConfig: map[string]any{
				"local": map[string]any{"http_port": 8080, "https_port": 8443},
			},
		},
		{
			name: "both ports invalid reports http_port first",
			providerConfig: map[string]any{
				"local": map[string]any{"http_port": -1, "https_port": 99999999},
			},
			wantErr: "http_port must be between 1 and 65535",
		},
		{
			name: "equal ports are rejected",
			providerConfig: map[string]any{
				"local": map[string]any{"http_port": 8443, "https_port": 8443},
			},
			wantErr: "http_port and https_port must differ",
		},
		{
			name: "http_port colliding with the default https_port is rejected",
			providerConfig: map[string]any{
				"local": map[string]any{"http_port": 443},
			},
			wantErr: "http_port and https_port must differ",
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

// TestDeployRejectsInvalidWorkers covers Deploy's own guard: the provider's
// Validate is not on the deploy path, so Deploy must reject a bad count
// itself rather than silently creating a single-node cluster. Dry-run keeps
// the test off the container runtime.
func TestDeployRejectsInvalidWorkers(t *testing.T) {
	p := NewProvider()

	tests := []struct {
		name    string
		workers int
		wantErr string
	}{
		{name: "zero workers", workers: 0},
		{name: "positive workers", workers: 2},
		{name: "negative workers", workers: -1, wantErr: "kind workers must be 0 or more"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{Providers: map[string]any{
				"local": map[string]any{"kind": map[string]any{"workers": tt.workers}},
			}}

			err := p.Deploy(context.Background(), "test-project", cfg, cluster.DeployOptions{DryRun: true})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Deploy returned error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Deploy error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestSummaryKindMode(t *testing.T) {
	p := NewProvider()

	tests := []struct {
		name string
		kind map[string]any
		// want maps summary keys to expected values; "" means the key must be absent.
		want map[string]string
	}{
		{
			name: "node image shown",
			kind: map[string]any{"node_image": "kindest/node:v1.32.2"},
			want: map[string]string{"Kind Node Image": "kindest/node:v1.32.2", "Kind Workers": ""},
		},
		{
			// Destroy prints this, so the teardown shows how many nodes go away.
			name: "worker count shown when set",
			kind: map[string]any{"workers": 2},
			want: map[string]string{"Kind Workers": "2", "Kind Node Image": ""},
		},
		{
			name: "single-node default shows neither",
			kind: map[string]any{},
			want: map[string]string{"Kind Workers": "", "Kind Node Image": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ClusterConfig{Providers: map[string]any{
				"local": map[string]any{"kind": tt.kind},
			}}

			summary := p.Summary(cfg)
			if summary["Kind Cluster"] == "" {
				t.Error("Summary missing Kind Cluster entry for managed mode")
			}
			for key, want := range tt.want {
				got, ok := summary[key]
				if want == "" {
					if ok {
						t.Errorf("Summary[%q] = %q, want it absent", key, got)
					}
					continue
				}
				if got != want {
					t.Errorf("Summary[%q] = %q, want %q", key, got, want)
				}
			}
		})
	}
}
