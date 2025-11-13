package dnsprovider

import (
	"context"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// mockDNSProvider is a mock implementation for testing
type mockDNSProvider struct {
	name string
}

func (m *mockDNSProvider) Name() string {
	return m.name
}

func (m *mockDNSProvider) Initialize(ctx context.Context, config *config.NebariConfig) error {
	return nil
}

func (m *mockDNSProvider) GetRecord(ctx context.Context, name string, recordType string) (*DNSRecord, error) {
	return nil, nil
}

func (m *mockDNSProvider) AddRecord(ctx context.Context, record DNSRecord) error {
	return nil
}

func (m *mockDNSProvider) UpdateRecord(ctx context.Context, record DNSRecord) error {
	return nil
}

func (m *mockDNSProvider) DeleteRecord(ctx context.Context, name string, recordType string) error {
	return nil
}

func (m *mockDNSProvider) EnsureRecord(ctx context.Context, record DNSRecord) error {
	return nil
}

func (m *mockDNSProvider) GetCertManagerConfig(ctx context.Context) (map[string]string, error) {
	return nil, nil
}

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if registry.providers == nil {
		t.Fatal("Registry providers map is nil")
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
	registry.Register(ctx, "test1", &mockDNSProvider{name: "test1"})
	registry.Register(ctx, "test2", &mockDNSProvider{name: "test2"})

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
