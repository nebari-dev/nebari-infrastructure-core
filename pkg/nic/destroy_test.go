package nic

import (
	"context"
	"errors"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// fakeClusterProvider is a minimal cluster.Provider whose Destroy returns a
// configurable error, for exercising Client.Destroy error handling.
type fakeClusterProvider struct {
	destroyErr error
}

func (f *fakeClusterProvider) Name() string { return "fake" }

func (f *fakeClusterProvider) Validate(context.Context, string, *config.ClusterConfig) error {
	return nil
}

func (f *fakeClusterProvider) Deploy(context.Context, string, *config.ClusterConfig, cluster.DeployOptions) error {
	return nil
}

func (f *fakeClusterProvider) Destroy(context.Context, string, *config.ClusterConfig, cluster.DestroyOptions) error {
	return f.destroyErr
}

func (f *fakeClusterProvider) GetKubeconfig(context.Context, string, *config.ClusterConfig) ([]byte, error) {
	return nil, errors.New("no kubeconfig")
}

func (f *fakeClusterProvider) Summary(*config.ClusterConfig) map[string]string {
	return nil
}

func (f *fakeClusterProvider) InfraSettings(*config.ClusterConfig) cluster.InfraSettings {
	return cluster.InfraSettings{}
}

// TestDestroyForceStillReturnsError pins the fix for #534: --force keeps the
// teardown going past provider errors, but Destroy must still return a
// non-nil error at the end so the CLI exits non-zero instead of reporting a
// partial teardown as clean.
func TestDestroyForceStillReturnsError(t *testing.T) {
	tests := []struct {
		name       string
		destroyErr error
		force      bool
		wantErr    bool
	}{
		{
			name:       "provider error with force returns error",
			destroyErr: errors.New("VPC has dependencies"),
			force:      true,
			wantErr:    true,
		},
		{
			name:       "provider error without force returns error",
			destroyErr: errors.New("VPC has dependencies"),
			force:      false,
			wantErr:    true,
		},
		{
			name:       "clean destroy returns nil",
			destroyErr: nil,
			force:      true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			reg := registry.NewRegistry()
			if err := reg.ClusterProviders.Register(ctx, "fake", &fakeClusterProvider{destroyErr: tt.destroyErr}); err != nil {
				t.Fatalf("register fake cluster provider: %v", err)
			}
			client := &Client{registry: reg}

			cfg := &config.NebariConfig{
				ProjectName: "test-project",
				Cluster: &config.ClusterConfig{
					Providers: map[string]any{"fake": map[string]any{}},
				},
			}

			err := client.Destroy(ctx, cfg, DestroyOptions{Force: tt.force})

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Destroy() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Destroy() expected error, got nil")
			}
			if !errors.Is(err, tt.destroyErr) {
				t.Errorf("Destroy() error = %v, want it to wrap %v", err, tt.destroyErr)
			}
		})
	}
}
