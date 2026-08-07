package nic

import (
	"context"
	"testing"
)

// TestRegisteredProvidersImplementConfigTyped guards the optional ConfigTyped
// capability. Because RegisteredConfigTypes skips providers that don't expose a
// config type, a provider added to the registry without a ConfigType() method
// would be silently omitted from the generated schemas. This test asserts every
// registered cluster and DNS provider is covered, so that omission fails loudly
// in CI instead.
func TestRegisteredProvidersImplementConfigTyped(t *testing.T) {
	ctx := context.Background()
	c, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	types := c.RegisteredConfigTypes(ctx)

	for _, name := range c.registry.ClusterProviders.List(ctx) {
		if _, ok := types.Cluster[name]; !ok {
			t.Errorf("cluster provider %q does not implement cluster.ConfigTyped "+
				"(missing a ConfigType() reflect.Type method); add it so schemagen "+
				"can emit its schema", name)
		}
	}
	for _, name := range c.registry.DNSProviders.List(ctx) {
		if _, ok := types.DNS[name]; !ok {
			t.Errorf("dns provider %q does not implement dns.ConfigTyped "+
				"(missing a ConfigType() reflect.Type method)", name)
		}
	}
}
