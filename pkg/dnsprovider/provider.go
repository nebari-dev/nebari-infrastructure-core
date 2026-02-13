package dnsprovider

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// DNSProvider defines the interface that all DNS providers must implement.
// Following the cloud provider pattern, providers are stateless — config is
// passed to each method and parsed internally.
type DNSProvider interface {
	// Name returns the DNS provider name (cloudflare, route53, azure-dns, etc.)
	Name() string

	// ProvisionRecords creates or updates DNS records for the deployment.
	// It creates a root domain record and wildcard record pointing to the
	// load balancer endpoint. The provider determines the record type
	// (CNAME for hostnames, A for IPs) from the endpoint value.
	ProvisionRecords(ctx context.Context, cfg *config.NebariConfig, lbEndpoint string) error

	// DestroyRecords removes DNS records that were created during deployment.
	// This is called before infrastructure destruction to clean up stale records.
	// Idempotent — succeeds even if records are already gone.
	DestroyRecords(ctx context.Context, cfg *config.NebariConfig) error
}
