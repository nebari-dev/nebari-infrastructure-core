package cloudflare

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider implements the Cloudflare DNS provider.
// Stateless — config is parsed on each call, matching the cloud provider pattern.
type Provider struct {
	client CloudflareClient // nil = use real SDK client; set via NewProviderForTesting
}

// NewProvider creates a new Cloudflare DNS provider.
func NewProvider() *Provider {
	return &Provider{}
}

// NewProviderForTesting creates a provider with an injected mock client.
func NewProviderForTesting(client CloudflareClient) *Provider {
	return &Provider{client: client}
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "cloudflare"
}

// ProvisionRecords creates or updates DNS records for the deployment.
func (p *Provider) ProvisionRecords(ctx context.Context, cfg *config.NebariConfig, lbEndpoint string) error {
	return nil // Stub — implemented in Task 4
}

// DestroyRecords removes DNS records created during deployment.
func (p *Provider) DestroyRecords(ctx context.Context, cfg *config.NebariConfig) error {
	return nil // Stub — implemented in Task 6
}
