package dnsprovider

import (
	"context"
	"reflect"
)

// DNSProvider defines the interface that all DNS providers must implement.
// Providers are stateless - domain and DNS config are passed to each call.
type DNSProvider interface {
	// Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
	Name() string

	// ProvisionRecords creates or updates DNS records for the deployment.
	// It creates a root domain record and wildcard record pointing to the
	// load balancer endpoint. The provider determines the record type
	// (CNAME for hostnames, A for IPs) from the endpoint value.
	ProvisionRecords(ctx context.Context, domain string, dnsConfig map[string]any, lbEndpoint string) error

	// DestroyRecords removes DNS records that were created during deployment.
	// This is called before infrastructure destruction to clean up stale records.
	// Idempotent - succeeds even if records are already gone.
	DestroyRecords(ctx context.Context, domain string, dnsConfig map[string]any) error

	// ConfigType returns the reflect.Type of this DNS provider's configuration
	// struct. Used by schema-generation tooling to enumerate provider
	// configurations via the registry, without taking by-name imports on
	// concrete provider packages. Implementations are one-liners:
	//   func (*Provider) ConfigType() reflect.Type { return reflect.TypeFor[Config]() }
	ConfigType() reflect.Type
}
