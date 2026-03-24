package dnsprovider

import "context"

// ValidateOptions controls which checks Validate performs.
type ValidateOptions struct {
	// CheckCreds, when true, also verifies credentials against the live provider
	// API (e.g. resolves the zone ID). Requires network access.
	CheckCreds bool
}

// DNSProvider defines the interface that all DNS providers must implement.
// Providers are stateless - domain and DNS config are passed to each call.
type DNSProvider interface {
	// Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
	Name() string

	// Validate checks the DNS configuration. It always verifies required config
	// fields (e.g. zone_name) and that the domain falls within the zone.
	// When opts.CheckCreds is true it additionally verifies that credentials are
	// present and can reach the live provider API.
	Validate(ctx context.Context, domain string, dnsConfig map[string]any, opts ValidateOptions) error

	// ProvisionRecords creates or updates DNS records for the deployment.
	// It creates a root domain record and wildcard record pointing to the
	// load balancer endpoint. The provider determines the record type
	// (CNAME for hostnames, A for IPs) from the endpoint value.
	ProvisionRecords(ctx context.Context, domain string, dnsConfig map[string]any, lbEndpoint string) error

	// DestroyRecords removes DNS records that were created during deployment.
	// This is called before infrastructure destruction to clean up stale records.
	// Idempotent - succeeds even if records are already gone.
	DestroyRecords(ctx context.Context, domain string, dnsConfig map[string]any) error
}
