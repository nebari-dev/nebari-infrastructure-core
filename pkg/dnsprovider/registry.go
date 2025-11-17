package dnsprovider

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Registry holds registered DNS providers
type Registry struct {
	mu        sync.RWMutex
	providers map[string]DNSProvider
}

// NewRegistry creates a new DNS provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]DNSProvider),
	}
}

// Register registers a DNS provider with the given name
func (r *Registry) Register(ctx context.Context, name string, provider DNSProvider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "dnsregistry.Register")
	defer span.End()

	span.SetAttributes(attribute.String("dns_provider.name", name))

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		err := fmt.Errorf("DNS provider %q is already registered", name)
		span.RecordError(err)
		return err
	}

	r.providers[name] = provider
	return nil
}

// Get retrieves a DNS provider by name
func (r *Registry) Get(ctx context.Context, name string) (DNSProvider, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "dnsregistry.Get")
	defer span.End()

	span.SetAttributes(attribute.String("dns_provider.name", name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		err := fmt.Errorf("DNS provider %q is not registered", name)
		span.RecordError(err)
		return nil, err
	}

	return provider, nil
}

// List returns all registered DNS provider names
func (r *Registry) List(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "dnsregistry.List")
	defer span.End()

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int("dns_provider.count", len(names)))

	return names
}
