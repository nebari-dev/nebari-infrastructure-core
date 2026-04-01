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
	ClusterProviders map[string]provider.Provider
	DNSProviders     map[string]dnsprovider.DNSProvider
	// TODO implement provider pattern for Git and Certs
	// CertProviders map[string]certprovider.CertProvider
	// GitProviders map[string]gitprovider.GitProvider
}

// NewRegistry creates a new DNS provider registry
func NewRegistry() *Registry {
	return &Registry{
		ClusterProviders: make(map[string]provider.Provider),
		DNSProviders:     make(map[string]dnsprovider.DNSProvider),
		// CertProviders: make(map[string]certprovider.Provider),
		// GitProviders:     make(map[string]gitprovider.Provider),
	}
}

func (r *Registry) ValidProviders() config.ValidProviders {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clusterNames := make([]string, 0, len(r.ClusterProviders))
	for name := range r.ClusterProviders {
		clusterNames = append(clusterNames, name)
	}
	dnsNames := make([]string, 0, len(r.DNSProviders))
	for name := range r.DNSProviders {
		dnsNames = append(dnsNames, name)
	}
	return config.ValidProviders{
		ClusterProviders: clusterNames,
		DNSProviders:     dnsNames,
	}
}
