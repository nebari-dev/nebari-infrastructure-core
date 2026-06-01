package nic

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Client is the entrypoint for programmatic use of NIC. Construct one with
// NewClient and reuse it across operations.
type Client struct {
	registry *registry.Registry
}

// NewClient returns a new NIC client. The context governs the provider
// registration step (currently used for trace propagation). Returns an
// error if the default provider registry fails to build.
func NewClient(ctx context.Context) (*Client, error) {
	reg, err := defaultRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("build default registry: %w", err)
	}
	return &Client{registry: reg}, nil
}
