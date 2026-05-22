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

// NewClient returns a new NIC client. Returns an error if the default
// provider registry fails to build.
func NewClient() (*Client, error) {
	reg, err := defaultRegistry(context.Background())
	if err != nil {
		return nil, fmt.Errorf("build default registry: %w", err)
	}
	return &Client{registry: reg}, nil
}
