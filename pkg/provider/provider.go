package provider

import (
	"context"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// Provider defines the interface that all cloud providers must implement.
//
// This interface establishes the abstraction boundary between CLI commands and
// provider implementations. CLI commands depend only on this interface, never on
// concrete provider types, enabling new providers to be added without modifying
// CLI code (Open/Closed Principle).
type Provider interface {
	// Name returns the short provider identifier used in CLI output, logging,
	// and OpenTelemetry span attributes (e.g., "aws", "gcp", "azure", "local").
	Name() string

	// ConfigKey returns the YAML key used for this provider's configuration block.
	// This allows providers to extract their own config from NebariConfig.ProviderConfig
	// without the config package needing to know about provider-specific types.
	// Examples: "amazon_web_services", "google_cloud_platform", "azure", "local"
	ConfigKey() string

	// Validate checks that the configuration is valid before any infrastructure
	// operations. This includes verifying required fields, validating formats,
	// and checking cloud-specific constraints (e.g., valid regions, instance types).
	Validate(ctx context.Context, config *config.NebariConfig) error

	// Deploy creates or updates infrastructure to match the desired configuration.
	// Backed by OpenTofu, this operation is idempotent - running Deploy multiple
	// times with the same config results in the same infrastructure state.
	// Use --dry-run flag to preview changes without applying them (runs tofu plan).
	Deploy(ctx context.Context, config *config.NebariConfig) error

	// Destroy tears down all infrastructure resources in the correct order,
	// respecting dependencies (e.g., node groups before cluster, cluster before VPC).
	// Backed by OpenTofu's tofu destroy command.
	Destroy(ctx context.Context, config *config.NebariConfig) error

	// GetKubeconfig generates a kubeconfig file for authenticating with the
	// Kubernetes cluster. The returned bytes can be written to a file or used
	// directly with Kubernetes client libraries.
	GetKubeconfig(ctx context.Context, config *config.NebariConfig) ([]byte, error)

	// Summary returns key-value pairs describing provider-specific configuration
	// for display purposes. This allows CLI commands to show details like region
	// or project in confirmation prompts without importing provider packages.
	// Used in destructive operations to help users confirm they're targeting
	// the correct infrastructure (e.g., distinguishing clusters with the same
	// name in different regions).
	Summary(config *config.NebariConfig) map[string]string
}

// CredentialValidator is an optional interface for providers that support
// thorough credential validation beyond basic authentication.
// Providers implement this to enable the --validate-creds flag.
// Providers that don't implement this (e.g., local) will show a message
// indicating the flag is not supported.
type CredentialValidator interface {
	// ValidateCredentials performs thorough credential validation including
	// permission checks using provider-specific APIs (e.g., IAM Policy Simulator).
	// Returns nil if all required permissions are present.
	ValidateCredentials(ctx context.Context, config *config.NebariConfig) error
}
