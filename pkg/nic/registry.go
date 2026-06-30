package nic

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	clusteraws "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/aws"
	clusterazure "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/azure"
	clusterexisting "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/existing"
	clustergcp "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/gcp"
	clusterhetzner "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/hetzner"
	clusterlocal "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/local"
	dnscloudflare "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns/cloudflare"
	repoexisting "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repo/existing"
	repolocal "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repo/local"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Providers lists the providers bundled with this build, grouped by
// category. New categories (e.g. certificate, IP) can be added as fields
// without breaking existing callers.
type Providers struct {
	Cluster []string
	DNS     []string
	Repo    []string
}

// ProviderNames returns the providers bundled with this build. Intended for
// diagnostic output (e.g. a `version` command); operational work should go
// through the Client's methods.
func (c *Client) ProviderNames(ctx context.Context) *Providers {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.ProviderNames")
	defer span.End()

	return &Providers{
		Cluster: c.registry.ClusterProviders.List(ctx),
		DNS:     c.registry.DNSProviders.List(ctx),
		Repo:    c.registry.RepoProviders.List(ctx),
	}
}

// defaultRegistry builds a Registry with all in-tree cluster, DNS, and repo
// providers registered.
//
// Provider import paths are aliased with a category prefix (cluster*, dns*,
// repo*): the cluster and repo categories both expose "existing" and "local"
// providers, whose packages would otherwise collide, so every provider import
// is prefixed for consistency.
func defaultRegistry(ctx context.Context) (*registry.Registry, error) {
	r := registry.NewRegistry()

	if err := r.ClusterProviders.Register(ctx, "aws", clusteraws.NewProvider()); err != nil {
		return nil, fmt.Errorf("register aws cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "gcp", clustergcp.NewProvider()); err != nil {
		return nil, fmt.Errorf("register gcp cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "azure", clusterazure.NewProvider()); err != nil {
		return nil, fmt.Errorf("register azure cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "local", clusterlocal.NewProvider()); err != nil {
		return nil, fmt.Errorf("register local cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "hetzner", clusterhetzner.NewProvider()); err != nil {
		return nil, fmt.Errorf("register hetzner cluster provider: %w", err)
	}
	if err := r.ClusterProviders.Register(ctx, "existing", clusterexisting.NewProvider()); err != nil {
		return nil, fmt.Errorf("register existing cluster provider: %w", err)
	}

	if err := r.DNSProviders.Register(ctx, "cloudflare", dnscloudflare.NewProvider()); err != nil {
		return nil, fmt.Errorf("register cloudflare dns provider: %w", err)
	}

	if err := r.RepoProviders.Register(ctx, repoexisting.ProviderName, repoexisting.NewProvider()); err != nil {
		return nil, fmt.Errorf("register existing repo provider: %w", err)
	}
	if err := r.RepoProviders.Register(ctx, repolocal.ProviderName, repolocal.NewProvider()); err != nil {
		return nil, fmt.Errorf("register local repo provider: %w", err)
	}

	return r, nil
}

// validateOptions builds config.ValidateOptions from a registry. Shared by
// operations that need to validate config against the registered providers.
func validateOptions(ctx context.Context, reg *registry.Registry) config.ValidateOptions {
	return config.ValidateOptions{
		ClusterProviders: reg.ClusterProviders.List(ctx),
		DNSProviders:     reg.DNSProviders.List(ctx),
		RepoProviders:    reg.RepoProviders.List(ctx),
	}
}
