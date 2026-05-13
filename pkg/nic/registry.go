package nic

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider/cloudflare"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/aws"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/azure"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/existing"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/gcp"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/hetzner"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Providers lists the providers bundled with this build, grouped by
// category. New categories (e.g. certificate, IP) can be added as fields
// without breaking existing callers.
type Providers struct {
	Cluster []string
	DNS     []string
}

// ProviderNames returns the providers bundled with this build. Intended for
// diagnostic output (e.g. a `version` command); operational work should go
// through the Client's methods.
func (c *Client) ProviderNames(ctx context.Context) (*Providers, error) {
	reg, err := defaultRegistry(ctx)
	if err != nil {
		return nil, err
	}
	return &Providers{
		Cluster: reg.ClusterProviders.List(ctx),
		DNS:     reg.DNSProviders.List(ctx),
	}, nil
}

// defaultRegistry builds a Registry with all in-tree cluster and DNS
// providers registered.
func defaultRegistry(ctx context.Context) (*registry.Registry, error) {
	r := registry.NewRegistry()

	if err := r.ClusterProviders.Register(ctx, "aws", aws.NewProvider()); err != nil {
		return nil, fmt.Errorf("register aws cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "gcp", gcp.NewProvider()); err != nil {
		return nil, fmt.Errorf("register gcp cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "azure", azure.NewProvider()); err != nil {
		return nil, fmt.Errorf("register azure cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "local", local.NewProvider()); err != nil {
		return nil, fmt.Errorf("register local cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "hetzner", hetzner.NewProvider()); err != nil {
		return nil, fmt.Errorf("register hetzner cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "existing", existing.NewProvider()); err != nil {
		return nil, fmt.Errorf("register existing cluster provider: %w", err)
	}

	if err := r.DNSProviders.Register(ctx, "cloudflare", cloudflare.NewProvider()); err != nil {
		return nil, fmt.Errorf("register cloudflare dns provider: %w", err)
	}

	return r, nil
}

// validateOptions builds config.ValidateOptions from a registry. Shared by
// operations that need to validate config against the registered providers.
func validateOptions(ctx context.Context, reg *registry.Registry) config.ValidateOptions {
	return config.ValidateOptions{
		ClusterProviders: reg.ClusterProviders.List(ctx),
		DNSProviders:     reg.DNSProviders.List(ctx),
	}
}
