package nic

import (
	"context"
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/fingerprint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/registry"
)

// Client is the entrypoint for programmatic use of NIC. Construct one with
// NewClient and reuse it across operations.
type Client struct {
	registry *registry.Registry

	// build is the NIC binary's identity, set via WithBuild by the cmd layer.
	// Nil when the caller did not supply one; see WithBuild.
	build *fingerprint.Build
}

// NewClient returns a new NIC client. The context governs the provider
// registration step (currently used for trace propagation). Returns an
// error if the default provider registry fails to build. Options are applied
// in order after the registry is built.
func NewClient(ctx context.Context, opts ...Option) (*Client, error) {
	reg, err := defaultRegistry(ctx)
	if err != nil {
		return nil, fmt.Errorf("build default registry: %w", err)
	}
	c := &Client{registry: reg}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Option configures a Client. Options exist so the cmd layer can hand
// build-time facts (which only package main holds) to the orchestration layer
// without pkg/nic reaching upwards for them.
type Option func(*Client)

// WithBuild records the NIC build identity that Deploy stamps onto the cluster
// as deployment metadata. Callers pass the -ldflags-injected version, commit
// and build date from package main. When this option is not supplied the
// metadata write is skipped rather than recording placeholder values, so a
// programmatic embedder that never sets it does not plant a misleading
// "unknown" provenance record on the cluster.
func WithBuild(version, commit, date string) Option {
	return func(c *Client) {
		c.build = &fingerprint.Build{Version: version, Commit: commit, Date: date}
	}
}
