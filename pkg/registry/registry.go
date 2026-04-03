// Package registry provides a unified registry for cluster and DNS providers.
// It consolidates provider registration, lookup, and listing behind a single
// thread-safe Registry struct, used by CLI commands to manage all provider types.
package registry

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// Registry holds all registered providers as ProviderList instances
// It provides type safe methods for registering and retrieving providers, as well as a method
// to get the valid provider names for config validation. The registry is thread-safe, allowing
// concurrent registration and retrieval of providers.
type Registry struct {
	ClusterProviders *ProviderList[provider.Provider]
	DNSProviders     *ProviderList[dnsprovider.DNSProvider]
}

// NewRegistry creates and returns a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		ClusterProviders: newProviderList[provider.Provider]("ClusterProviders"),
		DNSProviders:     newProviderList[dnsprovider.DNSProvider]("DNSProviders"),
	}
}
