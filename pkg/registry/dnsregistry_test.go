package registry

import (
	"context"
	"strings"
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

func TestRegisterDNSProvider(t *testing.T) {
	tests := []struct {
		name        string
		providers   []string // names to register in order
		wantErr     bool     // error expected on last registration
		errContains string
	}{
		{
			name:      "register single provider",
			providers: []string{"cloudflare"},
			wantErr:   false,
		},
		{
			name:      "register multiple providers",
			providers: []string{"cloudflare", "route53"},
			wantErr:   false,
		},
		{
			name:        "duplicate registration fails",
			providers:   []string{"cloudflare", "cloudflare"},
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
				err = reg.RegisterDNSProvider(ctx, name, &mockDNSProvider{name: name})
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

func TestGetDNSProvider(t *testing.T) {
	tests := []struct {
		name        string
		register    []string // providers to register first
		lookup      string   // name to look up
		wantErr     bool
		errContains string
	}{
		{
			name:     "existing provider",
			register: []string{"cloudflare"},
			lookup:   "cloudflare",
			wantErr:  false,
		},
		{
			name:        "non-existent provider",
			register:    []string{"cloudflare"},
			lookup:      "route53",
			wantErr:     true,
			errContains: "not registered",
		},
		{
			name:        "empty registry",
			register:    []string{},
			lookup:      "cloudflare",
			wantErr:     true,
			errContains: "not registered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := NewRegistry()

			for _, name := range tt.register {
				if err := reg.RegisterDNSProvider(ctx, name, &mockDNSProvider{name: name}); err != nil {
					t.Fatalf("setup: RegisterDNSProvider(%q) failed: %v", name, err)
				}
			}

			got, err := reg.GetDNSProvider(ctx, tt.lookup)

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

func TestListDNSProviders(t *testing.T) {
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
			register: []string{"cloudflare"},
			want:     1,
		},
		{
			name:     "multiple providers",
			register: []string{"cloudflare", "route53"},
			want:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reg := NewRegistry()

			for _, name := range tt.register {
				if err := reg.RegisterDNSProvider(ctx, name, &mockDNSProvider{name: name}); err != nil {
					t.Fatalf("setup: RegisterDNSProvider(%q) failed: %v", name, err)
				}
			}

			got := reg.ListDNSProviders(ctx)
			if len(got) != tt.want {
				t.Fatalf("ListDNSProviders() returned %d providers, want %d", len(got), tt.want)
			}

			// Verify all registered names are present
			found := make(map[string]bool)
			for _, name := range got {
				found[name] = true
			}
			for _, name := range tt.register {
				if !found[name] {
					t.Errorf("ListDNSProviders() missing %q", name)
				}
			}
		})
	}
}
