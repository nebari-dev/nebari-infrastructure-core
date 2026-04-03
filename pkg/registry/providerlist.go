package registry

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// This file defines the ProviderList type, which is a generic type
// for holding providers. It is used in the Registry struct to hold
// the registered providers in a thread-safe way.

// ProviderList is a generic type for holding providers. Currently
// we have DNS and Cluster Providers, but in the future will have
// Cert and Git providers too.
// TODO: update comment when cert and git providers are implemented
type ProviderList[T any] struct {
	mu                  sync.RWMutex
	name                string
	registeredProviders map[string]T
}

func newProviderList[T any](name string) *ProviderList[T] {
	return &ProviderList[T]{
		name:                name,
		registeredProviders: make(map[string]T),
	}
}

// Register registers a provider with the given name.
func (p *ProviderList[T]) Register(ctx context.Context, name string, value T) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, fmt.Sprintf("registry.%s.Register", p.name))
	defer span.End()

	span.SetAttributes(attribute.String(fmt.Sprintf("%s.name", p.name), name))

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.registeredProviders[name]; exists {
		err := fmt.Errorf("%s provider %q is already registered", p.name, name)
		span.RecordError(err)
		return err
	}

	p.registeredProviders[name] = value
	return nil
}

// Get retrieves a provider by name.
func (p *ProviderList[T]) Get(ctx context.Context, name string) (T, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, fmt.Sprintf("registry.%s.Get", p.name))
	defer span.End()

	span.SetAttributes(attribute.String(fmt.Sprintf("%s.name", p.name), name))

	p.mu.RLock()
	defer p.mu.RUnlock()

	provider, exists := p.registeredProviders[name]
	if !exists {
		err := fmt.Errorf("%s %q is not registered", p.name, name)
		span.RecordError(err)
		return provider, err
	}

	return provider, nil
}

// List returns the names of all registered providers as a slice of strings.
func (p *ProviderList[T]) List(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, fmt.Sprintf("registry.%s.List", p.name))
	defer span.End()

	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, 0, len(p.registeredProviders))
	for name := range p.registeredProviders {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int(fmt.Sprintf("%s.count", p.name), len(names)))

	return names
}
