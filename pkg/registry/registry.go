// Package registry provides a unified registry for cluster and DNS providers.
// It consolidates provider registration, lookup, and listing behind a single
// thread-safe Registry struct, used by CLI commands to manage all provider types.
package registry

import (
	"context"
	"sync"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Registry holds all registered providers behind a read-write mutex.
// Use the typed methods (RegisterClusterProvider, GetDNSProvider, etc.)
// to interact with registered providers.
type Registry struct {
	mu               sync.RWMutex
	clusterProviders map[string]provider.Provider
	dnsProviders     map[string]dnsprovider.DNSProvider
}

// NewRegistry creates and returns a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		clusterProviders: make(map[string]provider.Provider),
		dnsProviders:     make(map[string]dnsprovider.DNSProvider),
	}
}

// ValidProviders returns the names of all registered providers, suitable for
// passing to config.Validate to check provider names against the registry.
func (r *Registry) ValidProviders(ctx context.Context) config.ValidateOptions {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.ValidProviders")
	defer span.End()

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

	span.SetAttributes(
		attribute.Int("cluster_provider.count", len(clusterNames)),
		attribute.Int("dns_provider.count", len(dnsNames)),
	)

	return config.ValidateOptions{
		ClusterProviders: clusterNames,
		DNSProviders:     dnsNames,
	}
}
