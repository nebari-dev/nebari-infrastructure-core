package provider

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider defines the interface that all cloud providers must implement
type Provider interface {
	// Name returns the provider name (aws, gcp, azure, local)
	Name() string

	// Validate validates the configuration before deployment
	Validate(ctx context.Context, config *config.NebariConfig) error

	// Deploy deploys infrastructure based on the provided configuration using OpenTofu
	Deploy(ctx context.Context, config *config.NebariConfig) error

	// Destroy tears down all infrastructure using OpenTofu
	Destroy(ctx context.Context, config *config.NebariConfig) error

	// GetKubeconfig generates a kubeconfig file for accessing the Kubernetes cluster
	GetKubeconfig(ctx context.Context, config *config.NebariConfig) ([]byte, error)
}
