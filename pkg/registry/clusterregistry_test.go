package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// mockClusterProvider is a mock implementation for testing
type mockClusterProvider struct {
	name string
}

func (m *mockClusterProvider) Name() string                                             { return m.name }
func (m *mockClusterProvider) Validate(_ context.Context, _ *config.NebariConfig) error { return nil }
func (m *mockClusterProvider) Deploy(_ context.Context, _ *config.NebariConfig) error   { return nil }
func (m *mockClusterProvider) Destroy(_ context.Context, _ *config.NebariConfig) error  { return nil }
func (m *mockClusterProvider) GetKubeconfig(_ context.Context, _ *config.NebariConfig) ([]byte, error) {
	return nil, nil
}
func (m *mockClusterProvider) Summary(_ *config.NebariConfig) map[string]string { return nil }
func (m *mockClusterProvider) InfraSettings(_ *config.NebariConfig) provider.InfraSettings {
	return provider.InfraSettings{}
}

func TestRegisterClusterProvider(t *testing.T) {
	tests := []struct {
		name        string
		providers   []string // names to register in order
		wantErr     bool     // error expected on last registration
		errContains string
	}{
		{
			name:      "register single provider",
			providers: []string{"aws"},
			wantErr:   false,
		},
		{
			name:      "register multiple providers",
			providers: []string{"aws", "gcp", "azure"},
			wantErr:   false,
		},
		{
			name:        "duplicate registration fails",
			providers:   []string{"aws", "aws"},
			wantErr:     true,
			errContains: "already registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := NewRegistry()

			var err error
			for _, name := range tt.providers {
				err = reg.RegisterClusterProvider(ctx, name, &mockClusterProvider{name: name})
			}

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetClusterProvider(t *testing.T) {
	tests := []struct {
		name        string
		register    []string // providers to register first
		lookup      string   // name to look up
		wantErr     bool
		errContains string
	}{
		{
			name:     "existing provider",
			register: []string{"aws"},
			lookup:   "aws",
			wantErr:  false,
		},
		{
			name:        "non-existent provider",
			register:    []string{"aws"},
			lookup:      "gcp",
			wantErr:     true,
			errContains: "not registered",
		},
		{
			name:        "empty registry",
			register:    []string{},
			lookup:      "aws",
			wantErr:     true,
			errContains: "not registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := NewRegistry()

			for _, name := range tt.register {
				if err := reg.RegisterClusterProvider(ctx, name, &mockClusterProvider{name: name}); err != nil {
					t.Fatalf("setup: RegisterClusterProvider(%q) failed: %v", name, err)
				}
			}

			got, err := reg.GetClusterProvider(ctx, tt.lookup)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Name() != tt.lookup {
				t.Errorf("got provider %q, want %q", got.Name(), tt.lookup)
			}
		})
	}
}

func TestListClusterProviders(t *testing.T) {
	tests := []struct {
		name     string
		register []string
		want     int
	}{
		{
			name:     "empty registry",
			register: []string{},
			want:     0,
		},
		{
			name:     "single provider",
			register: []string{"aws"},
			want:     1,
		},
		{
			name:     "multiple providers",
			register: []string{"aws", "gcp", "azure"},
			want:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := NewRegistry()

			for _, name := range tt.register {
				if err := reg.RegisterClusterProvider(ctx, name, &mockClusterProvider{name: name}); err != nil {
					t.Fatalf("setup: RegisterClusterProvider(%q) failed: %v", name, err)
				}
			}

			got := reg.ListClusterProviders(ctx)
			if len(got) != tt.want {
				t.Fatalf("ListClusterProviders() returned %d providers, want %d", len(got), tt.want)
			}

			// Verify all registered names are present
			found := make(map[string]bool)
			for _, name := range got {
				found[name] = true
			}
			for _, name := range tt.register {
				if !found[name] {
					t.Errorf("ListClusterProviders() missing %q", name)
				}
			}
		})
	}
}
