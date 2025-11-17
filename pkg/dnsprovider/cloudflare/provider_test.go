package cloudflare

import (
	"context"
	"os"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
)

func TestNewProvider(t *testing.T) {
	provider := NewProvider()
	if provider == nil {
		t.Fatal("NewProvider() returned nil")
	}
}

func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "cloudflare" {
		t.Fatalf("Name() = %q, want %q", provider.Name(), "cloudflare")
	}
}

func TestInitialize(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	tests := []struct {
		name        string
		cfg         *config.NebariConfig
		envToken    string
		wantErr     bool
		errContains string
	}{
		{
			name: "success with env token",
			cfg: &config.NebariConfig{
				ProjectName: "test",
				DNSProvider: "cloudflare",
				DNS: map[string]any{
					"zone_name": "example.com",
					"email":     "admin@example.com",
				},
			},
			envToken: "test-token",
			wantErr:  false,
		},
		{
			name: "missing DNS config",
			cfg: &config.NebariConfig{
				ProjectName: "test",
				DNSProvider: "cloudflare",
			},
			envToken:    "test-token",
			wantErr:     true,
			errContains: "dns configuration is missing",
		},
		{
			name: "missing API token",
			cfg: &config.NebariConfig{
				ProjectName: "test",
				DNSProvider: "cloudflare",
				DNS: map[string]any{
					"zone_name": "example.com",
				},
			},
			envToken:    "",
			wantErr:     true,
			errContains: "CLOUDFLARE_API_TOKEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envToken != "" {
				if err := os.Setenv("CLOUDFLARE_API_TOKEN", tt.envToken); err != nil {
					t.Fatalf("Failed to set env var: %v", err)
				}
				defer func() {
					if err := os.Unsetenv("CLOUDFLARE_API_TOKEN"); err != nil {
						t.Logf("Failed to unset env var: %v", err)
					}
				}()
			} else {
				if err := os.Unsetenv("CLOUDFLARE_API_TOKEN"); err != nil {
					t.Logf("Failed to unset env var: %v", err)
				}
			}

			err := provider.Initialize(ctx, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Initialize() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Fatalf("Initialize() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestGetRecord(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	// Test without initialization
	_, err := provider.GetRecord(ctx, "test", "A")
	if err == nil {
		t.Fatal("GetRecord() should fail without initialization")
	}

	// Initialize provider
	if err := os.Setenv("CLOUDFLARE_API_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("CLOUDFLARE_API_TOKEN"); err != nil {
			t.Logf("Failed to unset env var: %v", err)
		}
	}()

	cfg := &config.NebariConfig{
		ProjectName: "test",
		DNSProvider: "cloudflare",
		DNS: map[string]any{
			"zone_name": "example.com",
		},
	}
	err = provider.Initialize(ctx, cfg)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Test GetRecord (stub returns nil, nil)
	record, err := provider.GetRecord(ctx, "test", "A")
	if err != nil {
		t.Fatalf("GetRecord() failed: %v", err)
	}
	if record != nil {
		t.Fatal("GetRecord() stub should return nil record")
	}
}

func TestEnsureRecord(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	// Initialize provider
	if err := os.Setenv("CLOUDFLARE_API_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("CLOUDFLARE_API_TOKEN"); err != nil {
			t.Logf("Failed to unset env var: %v", err)
		}
	}()

	cfg := &config.NebariConfig{
		ProjectName: "test",
		DNSProvider: "cloudflare",
		DNS: map[string]any{
			"zone_name": "example.com",
		},
	}
	err := provider.Initialize(ctx, cfg)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Test EnsureRecord
	record := dnsprovider.DNSRecord{
		Name:    "test",
		Type:    "A",
		Content: "1.2.3.4",
		TTL:     300,
	}
	err = provider.EnsureRecord(ctx, record)
	if err != nil {
		t.Fatalf("EnsureRecord() failed: %v", err)
	}
}

func TestGetCertManagerConfig(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	// Initialize provider
	if err := os.Setenv("CLOUDFLARE_API_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("CLOUDFLARE_API_TOKEN"); err != nil {
			t.Logf("Failed to unset env var: %v", err)
		}
	}()

	cfg := &config.NebariConfig{
		ProjectName: "test",
		DNSProvider: "cloudflare",
		DNS: map[string]any{
			"zone_name": "example.com",
			"email":     "admin@example.com",
		},
	}
	err := provider.Initialize(ctx, cfg)
	if err != nil {
		t.Fatalf("Initialize() failed: %v", err)
	}

	// Test GetCertManagerConfig
	certConfig, err := provider.GetCertManagerConfig(ctx)
	if err != nil {
		t.Fatalf("GetCertManagerConfig() failed: %v", err)
	}
	if certConfig == nil {
		t.Fatal("GetCertManagerConfig() returned nil")
	}
	if certConfig["email"] != "admin@example.com" {
		t.Fatalf("GetCertManagerConfig() email = %q, want %q", certConfig["email"], "admin@example.com")
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
