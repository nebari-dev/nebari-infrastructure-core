// Package registry provides a generic, thread-safe registry for named values.
// It is used to register and look up cluster providers, DNS providers, or any
// other named resource without duplicating registration logic.
package registry

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Registry is a thread-safe, generic key-value store for named resources.
type Registry[T any] struct {
	mu        sync.RWMutex
	providers map[string]T
}

// New creates and returns a new empty Registry.
func New[T any]() *Registry[T] {
	return &Registry[T]{
		providers: make(map[string]T),
	}
}

// Register adds a named provider to the registry. It returns an error if
// a provider with the same name is already registered.
func (r *Registry[T]) Register(ctx context.Context, name string, value T) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.Register")
	defer span.End()

	span.SetAttributes(
		attribute.String("registry.type", fmt.Sprintf("%T", value)),
		attribute.String("registry.name", name),
	)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		err := fmt.Errorf("%T %q is already registered", value, name)
		span.RecordError(err)
		return err
	}

	r.providers[name] = value
	return nil
}

// Get retrieves a provider by name. It returns an error if the name is not found.
func (r *Registry[T]) Get(ctx context.Context, name string) (T, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.Get")
	defer span.End()

	span.SetAttributes(attribute.String("registry.name", name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	value, exists := r.providers[name]
	if !exists {
		var zero T
		err := fmt.Errorf("%T %q is not registered", zero, name)
		span.RecordError(err)
		return zero, err
	}

	span.SetAttributes(attribute.String("registry.type", fmt.Sprintf("%T", value)))

	return value, nil
}

// List returns the names of all registered providers.
func (r *Registry[T]) List(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.List")
	defer span.End()

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int("registry.count", len(names)))

	return names
}
