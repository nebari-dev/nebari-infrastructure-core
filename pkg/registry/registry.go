package registry

import (
	"sync"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

// Registry holds registered DNS providers
type Registry struct {
	mu               sync.RWMutex
	clusterProviders map[string]provider.Provider
	dnsProviders     map[string]dnsprovider.DNSProvider
	// TODO implement provider pattern for Git and Certs
	// CertProviders map[string]certprovider.CertProvider
	// GitProviders map[string]gitprovider.GitProvider
}

// NewRegistry creates a new unified provider registry
func NewRegistry() *Registry {
	return &Registry{
		clusterProviders: make(map[string]provider.Provider),
		dnsProviders:     make(map[string]dnsprovider.DNSProvider),
		// CertProviders: make(map[string]certprovider.Provider),
		// GitProviders:     make(map[string]gitprovider.Provider),
	}
}

func (r *Registry) ValidProviders() config.ValidProviders {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clusterNames := make([]string, 0, len(r.clusterProviders))
	for name := range r.clusterProviders {
		clusterNames = append(clusterNames, name)
	}
	dnsNames := make([]string, 0, len(r.dnsProviders))
	for name := range r.dnsProviders {
		dnsNames = append(dnsNames, name)
	}
	return config.ValidProviders{
		ClusterProviders: clusterNames,
		DNSProviders:     dnsNames,
	}
}
