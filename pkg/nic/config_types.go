package nic

import (
	"context"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ConfigTypes is the set of reflect.Type values for each registered provider's
// configuration struct, grouped by category. Returned by
// (*Client).RegisteredConfigTypes for use by schema-generation tooling that
// needs to reflect on provider config types without taking by-name imports on
// concrete provider packages.
type ConfigTypes struct {
	Cluster map[string]reflect.Type
	DNS     map[string]reflect.Type
}

// RegisteredConfigTypes returns the Config Go types associated with each
// registered cluster and DNS provider. The returned maps are keyed by
// provider name (as registered) and contain the reflect.Type of each
// provider's configuration struct.
//
// Intended for build-time schema-generation tooling (e.g. cmd/schemagen)
// that enumerates provider configurations via the registry rather than
// hard-coding the provider list. The registry remains the single source
// of truth for which providers ship in this build.
func (c *Client) RegisteredConfigTypes(ctx context.Context) *ConfigTypes {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.RegisteredConfigTypes")
	defer span.End()

	cluster := make(map[string]reflect.Type)
	for _, name := range c.registry.ClusterProviders.List(ctx) {
		p, err := c.registry.ClusterProviders.Get(ctx, name)
		if err != nil {
			// Unreachable in practice: List and Get share the same backing map.
			continue
		}
		cluster[name] = p.ConfigType()
	}

	dns := make(map[string]reflect.Type)
	for _, name := range c.registry.DNSProviders.List(ctx) {
		p, err := c.registry.DNSProviders.Get(ctx, name)
		if err != nil {
			continue
		}
		dns[name] = p.ConfigType()
	}

	span.SetAttributes(
		attribute.Int("cluster.count", len(cluster)),
		attribute.Int("dns.count", len(dns)),
	)

	return &ConfigTypes{Cluster: cluster, DNS: dns}
}
