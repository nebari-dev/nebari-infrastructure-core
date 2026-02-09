package cloudflare

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// mockClient implements CloudflareClient for testing.
// Each method delegates to a function field if non-nil, otherwise returns a sensible default.
type mockClient struct {
	resolveZoneIDFn   func(ctx context.Context, zoneName string) (string, error)
	listDNSRecordsFn  func(ctx context.Context, zoneID string, name string, recordType string) ([]DNSRecordResult, error)
	createDNSRecordFn func(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error
	updateDNSRecordFn func(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error
	deleteDNSRecordFn func(ctx context.Context, zoneID string, recordID string) error
}

func (m *mockClient) ResolveZoneID(ctx context.Context, zoneName string) (string, error) {
	if m.resolveZoneIDFn != nil {
		return m.resolveZoneIDFn(ctx, zoneName)
	}
	return "zone-123", nil
}

func (m *mockClient) ListDNSRecords(ctx context.Context, zoneID string, name string, recordType string) ([]DNSRecordResult, error) {
	if m.listDNSRecordsFn != nil {
		return m.listDNSRecordsFn(ctx, zoneID, name, recordType)
	}
	return nil, nil
}

func (m *mockClient) CreateDNSRecord(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error {
	if m.createDNSRecordFn != nil {
		return m.createDNSRecordFn(ctx, zoneID, name, recordType, content, ttl)
	}
	return nil
}

func (m *mockClient) UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error {
	if m.updateDNSRecordFn != nil {
		return m.updateDNSRecordFn(ctx, zoneID, recordID, name, recordType, content, ttl)
	}
	return nil
}

func (m *mockClient) DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error {
	if m.deleteDNSRecordFn != nil {
		return m.deleteDNSRecordFn(ctx, zoneID, recordID)
	}
	return nil
}

func TestProviderName(t *testing.T) {
	provider := NewProvider()
	if provider.Name() != "cloudflare" {
		t.Fatalf("Name() = %q, want %q", provider.Name(), "cloudflare")
	}
}

func TestProvisionRecords(t *testing.T) {
	baseCfg := &config.NebariConfig{
		ProjectName: "test-project",
		Provider:    "aws",
		Domain:      "nebari.example.com",
		DNSProvider: "cloudflare",
		DNS: map[string]any{
			"zone_name": "example.com",
		},
	}

	tests := []struct {
		name           string
		cfg            *config.NebariConfig
		lbEndpoint     string
		envToken       string
		mock           *mockClient
		wantErr        bool
		wantErrContain string
		wantCreates    []string // "name:type:content" format
		wantUpdates    []string // "name:type:content" format
	}{
		{
			name:       "creates A records for IP endpoint",
			cfg:        baseCfg,
			lbEndpoint: "203.0.113.42",
			envToken:   "test-token",
			mock:       &mockClient{
				// No existing records -- ListDNSRecords returns empty
			},
			wantCreates: []string{
				"nebari.example.com:A:203.0.113.42",
				"*.nebari.example.com:A:203.0.113.42",
			},
		},
		{
			name:       "creates CNAME records for hostname endpoint",
			cfg:        baseCfg,
			lbEndpoint: "ab123.elb.us-west-2.amazonaws.com",
			envToken:   "test-token",
			mock:       &mockClient{
				// No existing records -- ListDNSRecords returns empty
			},
			wantCreates: []string{
				"nebari.example.com:CNAME:ab123.elb.us-west-2.amazonaws.com",
				"*.nebari.example.com:CNAME:ab123.elb.us-west-2.amazonaws.com",
			},
		},
		{
			name:       "updates existing record when content differs",
			cfg:        baseCfg,
			lbEndpoint: "203.0.113.99",
			envToken:   "test-token",
			mock: &mockClient{
				listDNSRecordsFn: func(_ context.Context, _ string, name string, _ string) ([]DNSRecordResult, error) {
					switch name {
					case "nebari.example.com":
						return []DNSRecordResult{{
							ID: "rec-1", Name: "nebari.example.com", Type: "A",
							Content: "203.0.113.1", TTL: 300,
						}}, nil
					case "*.nebari.example.com":
						return []DNSRecordResult{{
							ID: "rec-2", Name: "*.nebari.example.com", Type: "A",
							Content: "203.0.113.1", TTL: 300,
						}}, nil
					default:
						return nil, nil
					}
				},
			},
			wantUpdates: []string{
				"nebari.example.com:A:203.0.113.99",
				"*.nebari.example.com:A:203.0.113.99",
			},
		},
		{
			name:       "no-op when records already match",
			cfg:        baseCfg,
			lbEndpoint: "203.0.113.42",
			envToken:   "test-token",
			mock: &mockClient{
				listDNSRecordsFn: func(_ context.Context, _ string, name string, _ string) ([]DNSRecordResult, error) {
					switch name {
					case "nebari.example.com":
						return []DNSRecordResult{{
							ID: "rec-1", Name: "nebari.example.com", Type: "A",
							Content: "203.0.113.42", TTL: 300,
						}}, nil
					case "*.nebari.example.com":
						return []DNSRecordResult{{
							ID: "rec-2", Name: "*.nebari.example.com", Type: "A",
							Content: "203.0.113.42", TTL: 300,
						}}, nil
					default:
						return nil, nil
					}
				},
			},
			wantCreates: nil,
			wantUpdates: nil,
		},
		{
			name: "error when DNS config missing",
			cfg: &config.NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				Domain:      "nebari.example.com",
				DNSProvider: "cloudflare",
				DNS:         nil,
			},
			lbEndpoint:     "203.0.113.42",
			envToken:       "test-token",
			mock:           &mockClient{},
			wantErr:        true,
			wantErrContain: "dns configuration is missing",
		},
		{
			name:           "error when API token missing",
			cfg:            baseCfg,
			lbEndpoint:     "203.0.113.42",
			envToken:       "", // no token
			mock:           &mockClient{},
			wantErr:        true,
			wantErrContain: "CLOUDFLARE_API_TOKEN",
		},
		{
			name: "error when domain empty",
			cfg: &config.NebariConfig{
				ProjectName: "test-project",
				Provider:    "aws",
				Domain:      "",
				DNSProvider: "cloudflare",
				DNS: map[string]any{
					"zone_name": "example.com",
				},
			},
			lbEndpoint:     "203.0.113.42",
			envToken:       "test-token",
			mock:           &mockClient{},
			wantErr:        true,
			wantErrContain: "domain",
		},
		{
			name:       "error when zone resolution fails",
			cfg:        baseCfg,
			lbEndpoint: "203.0.113.42",
			envToken:   "test-token",
			mock: &mockClient{
				resolveZoneIDFn: func(_ context.Context, _ string) (string, error) {
					return "", fmt.Errorf("zone not found")
				},
			},
			wantErr:        true,
			wantErrContain: "not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set or clear the API token env var
			if tc.envToken != "" {
				t.Setenv("CLOUDFLARE_API_TOKEN", tc.envToken)
			}
			// When envToken is empty, we need to ensure the env var is unset.
			// t.Setenv sets it; not calling t.Setenv leaves it at its current
			// value. To force it unset, set to empty and rely on validation.
			// Actually, t.Setenv("CLOUDFLARE_API_TOKEN", "") would set it to "".
			// We need to unset. Use os.Unsetenv inside test cleanup, but t.Setenv
			// already saves/restores, so we just don't set it.
			// The test binary shouldn't have CLOUDFLARE_API_TOKEN set by default.

			// Track creates and updates
			var creates []string
			var updates []string

			tc.mock.createDNSRecordFn = wrapCreate(tc.mock.createDNSRecordFn, &creates)
			tc.mock.updateDNSRecordFn = wrapUpdate(tc.mock.updateDNSRecordFn, &updates)

			provider := NewProviderForTesting(tc.mock)
			err := provider.ProvisionRecords(context.Background(), tc.cfg, tc.lbEndpoint)

			// Check error expectations
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrContain)
				}
				if !strings.Contains(err.Error(), tc.wantErrContain) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErrContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check creates
			if len(tc.wantCreates) != len(creates) {
				t.Errorf("expected %d creates, got %d: %v", len(tc.wantCreates), len(creates), creates)
			} else {
				for i, want := range tc.wantCreates {
					if creates[i] != want {
						t.Errorf("create[%d] = %q, want %q", i, creates[i], want)
					}
				}
			}

			// Check updates
			if len(tc.wantUpdates) != len(updates) {
				t.Errorf("expected %d updates, got %d: %v", len(tc.wantUpdates), len(updates), updates)
			} else {
				for i, want := range tc.wantUpdates {
					if updates[i] != want {
						t.Errorf("update[%d] = %q, want %q", i, updates[i], want)
					}
				}
			}
		})
	}
}

// wrapCreate wraps an optional createDNSRecordFn to also track calls.
func wrapCreate(original func(context.Context, string, string, string, string, int) error, tracker *[]string) func(context.Context, string, string, string, string, int) error {
	return func(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error {
		*tracker = append(*tracker, fmt.Sprintf("%s:%s:%s", name, recordType, content))
		if original != nil {
			return original(ctx, zoneID, name, recordType, content, ttl)
		}
		return nil
	}
}

// wrapUpdate wraps an optional updateDNSRecordFn to also track calls.
func wrapUpdate(original func(context.Context, string, string, string, string, string, int) error, tracker *[]string) func(context.Context, string, string, string, string, string, int) error {
	return func(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error {
		*tracker = append(*tracker, fmt.Sprintf("%s:%s:%s", name, recordType, content))
		if original != nil {
			return original(ctx, zoneID, recordID, name, recordType, content, ttl)
		}
		return nil
	}
}
