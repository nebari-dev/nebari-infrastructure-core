// Package nic is the programmatic entrypoint for Nebari Infrastructure
// Core. Construct a Client with NewClient and use its methods (Deploy,
// Destroy, Validate, Kubeconfig) to drive infrastructure operations.
package nic

import (
	"log/slog"
)

// Client is the entrypoint for programmatic use of NIC. Construct one with
// NewClient and reuse it across operations; methods take ctx for
// cancellation and per-call options.
type Client struct {
	logger *slog.Logger
}

// ClientConfig holds optional configuration for NewClient. Zero-valued
// fields fall back to sensible defaults (Logger → slog.Default()). Add new
// fields here to expand the configuration surface without changing the
// NewClient signature.
type ClientConfig struct {
	// Logger receives structured records produced by NIC operations.
	// Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

// NewClient returns a new NIC client. Pass no arguments for default
// configuration, or a single *ClientConfig to customise.
//
//	client := nic.NewClient()
//	client := nic.NewClient(&nic.ClientConfig{Logger: myLogger})
//
// Only the first ClientConfig is read; additional arguments are ignored.
func NewClient(cfg ...*ClientConfig) *Client {
	var c ClientConfig
	if len(cfg) > 0 && cfg[0] != nil {
		c = *cfg[0]
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return &Client{
		logger: c.Logger,
	}
}
