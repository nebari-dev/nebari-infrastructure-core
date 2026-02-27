package dnsprovider

import (
	"context"
	"testing"
)

// mockDNSProvider is a mock implementation for testing
type mockDNSProvider struct {
	name string
}

func (m *mockDNSProvider) Name() string {
	return m.name
}

func (m *mockDNSProvider) ProvisionRecords(ctx context.Context, domain string, dnsConfig map[string]any, lbEndpoint string) error {
	return nil
}

func (m *mockDNSProvider) DestroyRecords(ctx context.Context, domain string, dnsConfig map[string]any) error {
	return nil
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil || registry.providers == nil {
		t.Fatal("NewRegistry() returned nil or has nil providers map")
	}
}

func TestRegisterProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	provider := &mockDNSProvider{name: "test"}
	err := registry.Register(ctx, "test", provider)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test duplicate registration
	err = registry.Register(ctx, "test", provider)
	if err == nil {
		t.Fatal("Register() should fail for duplicate provider")
	}
}

func TestGetProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	provider := &mockDNSProvider{name: "test"}
	err := registry.Register(ctx, "test", provider)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test successful get
	got, err := registry.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got != provider {
		t.Fatal("Get() returned wrong provider")
	}

	// Test non-existent provider
	_, err = registry.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Get() should fail for non-existent provider")
	}
}

func TestListProviders(t *testing.T) {
	ctx := context.Background()
	registry := NewRegistry()

	// Test empty registry
	providers := registry.List(ctx)
	if len(providers) != 0 {
		t.Fatalf("List() returned %d providers, expected 0", len(providers))
	}

	// Register providers
	if err := registry.Register(ctx, "test1", &mockDNSProvider{name: "test1"}); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}
	if err := registry.Register(ctx, "test2", &mockDNSProvider{name: "test2"}); err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Test list
	providers = registry.List(ctx)
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
