package dns

import "context"

// ValidateOptions configures provider validation behavior.
type ValidateOptions struct {
	// CheckCreds additionally verifies credentials and zone reachability
	// against the provider API. Leave false for offline validation (no API
	// calls, no credentials required), e.g. `nic validate` and the
	// pre-provisioning check in `nic deploy`.
	CheckCreds bool
}

// Provider defines the interface that all DNS providers must implement.
// Providers are stateless - domain and DNS config are passed to each call.
type Provider interface {
	// Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
	Name() string

	// Validate checks that cfg is consistent with the deployment domain.
	// The offline checks always run: the provider's zone configuration must
	// be present, well-typed, and must contain domain, so the records
	// created at deploy time (apex + wildcard) actually live in that zone.
	// When opts.CheckCreds is true the provider additionally verifies
	// credentials and zone reachability against its API.
	Validate(ctx context.Context, domain string, cfg map[string]any, opts ValidateOptions) error

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
