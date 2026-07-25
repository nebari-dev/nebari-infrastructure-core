package nic

import (
	"context"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns"
)

// ConfigTypes is the set of reflect.Type values for each registered provider's
// configuration struct, grouped by category. Returned by
// (*Client).RegisteredConfigTypes for schema-generation tooling that reflects
// on provider config types via the registry rather than a hard-coded list.
type ConfigTypes struct {
	Cluster map[string]reflect.Type
	DNS     map[string]reflect.Type
}

// RegisteredConfigTypes returns the config Go type of each registered cluster
// and DNS provider, keyed by provider name. It walks the registry - the single
// source of truth for which providers ship in this build - and reads each
// provider's config type through the optional cluster.ConfigTyped /
// dns.ConfigTyped capability. A provider that does not implement that
// capability is simply omitted (its config type is unknown), never a hard error.
//
// Intended for build-time tooling (cmd/schemagen) and, later, config
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

	span.SetAttributes(
		attribute.Int("cluster.count", len(clusterTypes)),
		attribute.Int("dns.count", len(dnsTypes)),
	)

	return &ConfigTypes{Cluster: clusterTypes, DNS: dnsTypes}
}
