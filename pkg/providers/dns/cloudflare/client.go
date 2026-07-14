package cloudflare

import "context"

// DNSRecordResult represents a DNS record returned from the Cloudflare API.
// This is our domain type -- it insulates the rest of the code from SDK types.
type DNSRecordResult struct {
	ID      string
	Name    string
	Type    string
	Content string
	TTL     int
}

// CloudflareClient abstracts the Cloudflare API for testability.
// The real implementation wraps the cloudflare-go SDK.
// Tests inject a mock implementation via NewProviderForTesting.
type CloudflareClient interface {
	// ResolveZoneID looks up the zone ID for a given zone name.
	// Returns an error if the zone is not found or the token lacks access.
	ResolveZoneID(ctx context.Context, zoneName string) (string, error)

	// ListDNSRecords returns DNS records matching the given name and type.
	// Both name and recordType can be empty to list all records.
	ListDNSRecords(ctx context.Context, zoneID string, name string, recordType string) ([]DNSRecordResult, error)

	// CreateDNSRecord creates a new DNS record in the given zone.
	CreateDNSRecord(ctx context.Context, zoneID string, name string, recordType string, content string, ttl int) error

	// UpdateDNSRecord updates an existing DNS record by ID.
	UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, name string, recordType string, content string, ttl int) error

	// DeleteDNSRecord deletes a DNS record by ID.
	DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error
}
