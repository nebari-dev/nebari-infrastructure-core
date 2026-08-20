package nic

import (
	"context"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// ConfigTypes is the set of reflect.Type values for each registered provider's
// configuration struct, grouped by category. Returned by
// (*Client).RegisteredConfigTypes for schema-generation tooling that reflects
// on provider config types via the registry rather than a hard-coded list.
type ConfigTypes struct {
	Cluster    map[string]reflect.Type
	DNS        map[string]reflect.Type
	Repository map[string]reflect.Type
}

// RegisteredConfigTypes returns the config Go type of each registered provider,
// keyed by provider name within its category. It walks the registry - the single
// source of truth for which providers ship in this build - and reads each
// provider's config type through the optional ConfigTyped capability its
// category defines.
//
// Every category in registry.Registry must be walked here. A category that is
// missed generates no schema and no documentation, and produces no diff for the
// drift gate to fail on, so it goes unnoticed - which is how the repository
// category shipped undocumented. A provider that does not implement that
// capability is simply omitted (its config type is unknown), never a hard error.
//
// Intended for build-time tooling (cmd/docgen) and, later, config
// scaffolding (nic init), so neither needs to import concrete provider packages
// or maintain a parallel type list.
func (c *Client) RegisteredConfigTypes(ctx context.Context) *ConfigTypes {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.RegisteredConfigTypes")
	defer span.End()

	clusterTypes := make(map[string]reflect.Type)
	for _, name := range c.registry.ClusterProviders.List(ctx) {
		p, err := c.registry.ClusterProviders.Get(ctx, name)
		if err != nil {
			// Unreachable in practice: List and Get share the same backing map.
			continue
		}
		if ct, ok := p.(cluster.ConfigTyped); ok {
			clusterTypes[name] = ct.ConfigType()
		}
	}

	dnsTypes := make(map[string]reflect.Type)
	for _, name := range c.registry.DNSProviders.List(ctx) {
		p, err := c.registry.DNSProviders.Get(ctx, name)
		if err != nil {
			continue
		}
		if ct, ok := p.(dns.ConfigTyped); ok {
			dnsTypes[name] = ct.ConfigType()
		}
	}

	repositoryTypes := make(map[string]reflect.Type)
	for _, name := range c.registry.RepositoryProviders.List(ctx) {
		p, err := c.registry.RepositoryProviders.Get(ctx, name)
		if err != nil {
			continue
		}
		if ct, ok := p.(repository.ConfigTyped); ok {
			repositoryTypes[name] = ct.ConfigType()
		}
	}

	span.SetAttributes(
		attribute.Int("cluster.count", len(clusterTypes)),
		attribute.Int("dns.count", len(dnsTypes)),
		attribute.Int("repository.count", len(repositoryTypes)),
	)

	return &ConfigTypes{Cluster: clusterTypes, DNS: dnsTypes, Repository: repositoryTypes}
}
