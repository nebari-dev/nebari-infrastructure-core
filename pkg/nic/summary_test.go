package nic

import (
	"context"
	"maps"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

func TestClusterSummary(t *testing.T) {
	ctx := context.Background()
	reg := registry.NewRegistry()
	want := map[string]string{"Region": "us-west-2"}
	if err := reg.ClusterProviders.Register(ctx, "fake", &fakeClusterProvider{summary: want}); err != nil {
		t.Fatalf("register fake cluster provider: %v", err)
	}
	client := &Client{registry: reg}

	tests := []struct {
		name    string
		cfg     *config.NebariConfig
		want    map[string]string
		wantErr bool
	}{
		{
			name: "registered provider",
			cfg:  &config.NebariConfig{Cluster: &config.ClusterConfig{Providers: map[string]any{"fake": map[string]any{}}}},
			want: want,
		},
		{
			name:    "unknown provider",
			cfg:     &config.NebariConfig{Cluster: &config.ClusterConfig{Providers: map[string]any{"nope": map[string]any{}}}},
			wantErr: true,
		},
		{
			name:    "no cluster",
			cfg:     &config.NebariConfig{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.ClusterSummary(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ClusterSummary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ClusterSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}
