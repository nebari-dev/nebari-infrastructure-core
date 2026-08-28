// Package registry provides a unified registry for cluster, DNS, and repository
// providers. It consolidates provider registration, lookup, and listing behind a
// single thread-safe Registry struct, used by CLI commands to manage all
// provider types.
package registry

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/dns"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// Registry holds all registered providers as ProviderList instances
// It provides type safe methods for registering and retrieving providers, as well as a method
// to get the valid provider names for config validation. The registry is thread-safe, allowing
// concurrent registration and retrieval of providers.
type Registry struct {
	ClusterProviders    *ProviderList[cluster.Provider]
	DNSProviders        *ProviderList[dns.Provider]
	RepositoryProviders *ProviderList[repository.Provider]
}

// NewRegistry creates and returns a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		ClusterProviders:    newProviderList[cluster.Provider]("ClusterProviders"),
		DNSProviders:        newProviderList[dns.Provider]("DNSProviders"),
		RepositoryProviders: newProviderList[repository.Provider]("RepositoryProviders"),
	}
}
