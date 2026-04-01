package registry

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/dnsprovider"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Register registers a DNS provider with the given name
func (r *Registry) RegisterDNSProvider(ctx context.Context, name string, provider dnsprovider.DNSProvider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.RegisterDNSProvider")
	defer span.End()

	span.SetAttributes(attribute.String("dns_provider.name", name))

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.dnsProviders[name]; exists {
		err := fmt.Errorf("DNS provider %q is already registered", name)
		span.RecordError(err)
		return err
	}

	r.dnsProviders[name] = provider
	return nil
}

// Get retrieves a DNS provider by name
func (r *Registry) GetDNSProvider(ctx context.Context, name string) (dnsprovider.DNSProvider, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.GetDNSProvider")
	defer span.End()

	span.SetAttributes(attribute.String("dns_provider.name", name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.dnsProviders[name]
	if !exists {
		err := fmt.Errorf("DNS provider %q is not registered", name)
		span.RecordError(err)
		return nil, err
	}

	return provider, nil
}

// List returns all registered DNS provider names
func (r *Registry) ListDNSProviders(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.ListDNSProviders")
	defer span.End()

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.dnsProviders))
	for name := range r.dnsProviders {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int("dns_provider.count", len(names)))

	return names
}
