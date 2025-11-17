package provider

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Registry holds registered providers
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register registers a provider with the given name
func (r *Registry) Register(ctx context.Context, name string, provider Provider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.Register")
	defer span.End()

	span.SetAttributes(attribute.String("provider.name", name))

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		err := fmt.Errorf("provider %q is already registered", name)
		span.RecordError(err)
		return err
	}

	r.providers[name] = provider
	return nil
}

// Get retrieves a provider by name
func (r *Registry) Get(ctx context.Context, name string) (Provider, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.Get")
	defer span.End()

	span.SetAttributes(attribute.String("provider.name", name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		err := fmt.Errorf("provider %q is not registered", name)
		span.RecordError(err)
		return nil, err
	}

	return provider, nil
}

// List returns all registered provider names
func (r *Registry) List(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.List")
	defer span.End()

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int("provider.count", len(names)))

	return names
}
