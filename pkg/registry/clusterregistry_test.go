package registry

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// mockClusterProvider is a mock implementation for testing
type mockClusterProvider struct {
	name      string
	configKey string
}

func (m *mockClusterProvider) Name() string {
	return m.name
}

func (m *mockClusterProvider) ConfigKey() string {
	return m.configKey
}

func (m *mockClusterProvider) Deploy(ctx context.Context, config *config.NebariConfig) error {
	return nil
}

func (m *mockClusterProvider) Destroy(ctx context.Context, config *config.NebariConfig) error {
	return nil
}

func (m *mockClusterProvider) Validate(ctx context.Context, config *config.NebariConfig) error {
	return nil
}

func (m *mockClusterProvider) Summary(config *config.NebariConfig) map[string]string {
	return nil
}

func (m *mockClusterProvider) GetKubeconfig(ctx context.Context, config *config.NebariConfig) ([]byte, error) {
	return nil, nil
}

func (m *mockClusterProvider) InfraSettings(config *config.NebariConfig) provider.InfraSettings {
	return provider.InfraSettings{}
}

func TestRegisterClusterProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	provider := &mockClusterProvider{name: "test"}
	err := registry.RegisterClusterProvider(ctx, "test", provider)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test duplicate registration
	err = registry.RegisterClusterProvider(ctx, "test", provider)
	if err == nil {
		t.Fatal("Register() should fail for duplicate provider")
	}
}

func TestGetClusterProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	provider := &mockClusterProvider{name: "test"}
	err := registry.RegisterClusterProvider(ctx, "test", provider)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test successful get
	got, err := registry.GetClusterProvider(ctx, "test")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got != provider {
		t.Fatal("Get() returned wrong provider")
	}

	// Test non-existent provider
	_, err = registry.GetClusterProvider(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Get() should fail for non-existent provider")
	}
}

func TestListClusterProviders(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	// Test empty registry
	providers := registry.ListClusterProviders(ctx)
	if len(providers) != 0 {
		t.Fatalf("List() returned %d providers, expected 0", len(providers))
	}

	// Register providers
	if err := registry.RegisterClusterProvider(ctx, "test1", &mockClusterProvider{name: "test1"}); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}
	if err := registry.RegisterClusterProvider(ctx, "test2", &mockClusterProvider{name: "test2"}); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test list
	providers = registry.ListClusterProviders(ctx)
	if len(providers) != 2 {
		t.Fatalf("List() returned %d providers, expected 2", len(providers))
	}

	// Verify provider names are present
	found := make(map[string]bool)
	for _, name := range providers {
		found[name] = true
	}
	if !found["test1"] || !found["test2"] {
		t.Fatal("List() missing expected provider names")
	}
}
