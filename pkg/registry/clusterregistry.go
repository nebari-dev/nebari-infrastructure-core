package registry

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// Register registers a cluster provider with the given name
func (r *Registry) RegisterClusterProvider(ctx context.Context, name string, provider provider.Provider) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.RegisterClusterProvider")
	defer span.End()

	span.SetAttributes(attribute.String("provider.name", name))

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clusterProviders[name]; exists {
		err := fmt.Errorf("provider %q is already registered", name)
		span.RecordError(err)
		return err
	}

	r.clusterProviders[name] = provider
	return nil
}

// Get retrieves a provider by name
func (r *Registry) GetClusterProvider(ctx context.Context, name string) (provider.Provider, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.GetClusterProvider")
	defer span.End()

	span.SetAttributes(attribute.String("provider.name", name))

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.clusterProviders[name]
	if !exists {
		err := fmt.Errorf("provider %q is not registered", name)
		span.RecordError(err)
		return nil, err
	}

	return provider, nil
}

// List returns all registered provider names
func (r *Registry) ListClusterProviders(ctx context.Context) []string {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "registry.ListClusterProviders")
	defer span.End()

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.clusterProviders))
	for name := range r.clusterProviders {
		names = append(names, name)
	}

	span.SetAttributes(attribute.Int("provider.count", len(names)))

	return names
}
