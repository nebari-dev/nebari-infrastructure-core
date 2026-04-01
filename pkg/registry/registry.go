package registry

import (
	"sync"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

type Registry struct {
	mu               sync.RWMutex
	ClusterProviders map[string]provider.Provider
	DNSProviders     map[string]dnsprovider.DNSProvider
	// TODO implement provider pattern for Git and Certs
}

func NewRegistry() *Registry {
	return &Registry{
		ClusterProviders: make(map[string]provider.Provider),
		DNSProviders:     make(map[string]dnsprovider.DNSProvider),
	}
}

func (r *Registry) ValidProviders() config.ValidateOptions {
	clusterNames := make([]string, 0, len(r.ClusterProviders))
	for name := range r.ClusterProviders {
		clusterNames = append(clusterNames, name)
	}
	dnsNames := make([]string, 0, len(r.DNSProviders))
	for name := range r.DNSProviders {
		dnsNames = append(dnsNames, name)
	}
	return config.ValidateOptions{
		ClusterProviders: clusterNames,
		DNSProviders:     dnsNames,
	}
}
