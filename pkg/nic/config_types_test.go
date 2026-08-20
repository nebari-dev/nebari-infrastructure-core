package nic

import (
	"context"
	"reflect"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// TestRegisteredProvidersImplementConfigTyped guards the optional ConfigTyped
// capability. Because RegisteredConfigTypes skips providers that don't expose a
// config type, a provider added to the registry without a ConfigType() method
// would be silently omitted from the generated schemas. This test asserts every
// registered provider in every category is covered, so that omission fails
// loudly in CI instead.
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
				"(missing a ConfigType() reflect.Type method); add it so cmd/docgen "+
				"can emit its schema", name)
		}
	}
	for _, name := range c.registry.DNSProviders.List(ctx) {
		if _, ok := types.DNS[name]; !ok {
			t.Errorf("dns provider %q does not implement dns.ConfigTyped "+
				"(missing a ConfigType() reflect.Type method)", name)
		}
	}
	for _, name := range c.registry.RepositoryProviders.List(ctx) {
		if _, ok := types.Repository[name]; !ok {
			t.Errorf("repository provider %q does not implement repository.ConfigTyped "+
				"(missing a ConfigType() reflect.Type method)", name)
		}
	}
}

// TestConfigTypesCoversEveryRegistryCategory fails when a provider category is
// added to registry.Registry but not walked by RegisteredConfigTypes. That
// omission is invisible at runtime - the new category simply produces no schema
// and no docs, and an absent page yields no diff for the drift gate to catch,
// which is exactly how pkg/providers/repository/ shipped undocumented. Counting
// fields keeps the check honest without naming the categories, so it also
// covers the next one.
func TestConfigTypesCoversEveryRegistryCategory(t *testing.T) {
	categories := reflect.TypeOf(registry.Registry{}).NumField()
	walked := reflect.TypeOf(ConfigTypes{}).NumField()

	if walked != categories {
		t.Errorf("ConfigTypes has %d category maps but registry.Registry has %d provider lists; "+
			"add the missing category to ConfigTypes and to the walk in RegisteredConfigTypes, "+
			"along with a ConfigTyped interface in its provider package", walked, categories)
	}
}
