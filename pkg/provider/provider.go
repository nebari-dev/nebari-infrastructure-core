package provider

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider defines the interface that all cloud providers must implement
type Provider interface {
	// Deploy deploys infrastructure based on the provided configuration
	Deploy(ctx context.Context, config *config.NebariConfig) error

	// Name returns the provider name (aws, gcp, azure, local)
	Name() string
}
