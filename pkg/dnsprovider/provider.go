package dnsprovider

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// DNSRecord represents a DNS record
type DNSRecord struct {
	Name    string // Record name (e.g., "www", "@" for root, "*" for wildcard)
	Type    string // Record type (A, AAAA, CNAME, TXT, etc.)
	Content string // Record content/value (IP address, domain, etc.)
	TTL     int    // Time to live in seconds
	// Provider-specific fields can be added by individual providers
}

// DNSProvider defines the interface that all DNS providers must implement
type DNSProvider interface {
	// Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
	Name() string

	// Initialize sets up the DNS provider with credentials from config
	// This should validate credentials and zone access
	Initialize(ctx context.Context, config *config.NebariConfig) error

	// GetRecord retrieves a specific DNS record by name and type
	// Returns nil if record doesn't exist (not an error)
	GetRecord(ctx context.Context, name string, recordType string) (*DNSRecord, error)

	// AddRecord creates a new DNS record
	// Returns error if record already exists
	AddRecord(ctx context.Context, record DNSRecord) error

	// UpdateRecord updates an existing DNS record
	// Returns error if record doesn't exist
	UpdateRecord(ctx context.Context, record DNSRecord) error

	// DeleteRecord deletes a DNS record by name and type
	// Returns error if record doesn't exist
	DeleteRecord(ctx context.Context, name string, recordType string) error

	// EnsureRecord ensures a record exists with the given properties
	// Creates if missing, updates if different, no-op if matches
	// This is the recommended method for most use cases
	EnsureRecord(ctx context.Context, record DNSRecord) error

	// GetCertManagerConfig returns the configuration needed for cert-manager
	// to perform DNS-01 challenges (e.g., API tokens, zone info)
	// Returns provider-specific configuration as a map
	GetCertManagerConfig(ctx context.Context) (map[string]string, error)
}
