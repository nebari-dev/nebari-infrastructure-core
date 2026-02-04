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

	// Reconcile compares desired config against actual state and reconciles differences.
	// Deprecated: This method is redundant with OpenTofu - Deploy already performs
	// reconciliation via tofu apply. See https://github.com/nebari-dev/nebari-infrastructure-core/issues/44
	Reconcile(ctx context.Context, config *config.NebariConfig) error

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

// InfrastructureState represents the discovered state of infrastructure
// This is an in-memory struct populated by querying cloud APIs
// It is NEVER persisted to disk (stateless architecture)
type InfrastructureState struct {
	ClusterName string
	Provider    string
	Region      string

	// Network state
	Network *NetworkState

	// Cluster state
	Cluster *ClusterState

	// Node pools state
	NodePools []NodePoolState

	// Storage state
	Storage *StorageState
}

// NetworkState represents the network infrastructure state
type NetworkState struct {
	// Provider-specific network ID (AWS VPC ID, GCP network name, etc.)
	ID string

	// CIDR block for the network
	CIDR string

	// Subnet IDs or names
	SubnetIDs []string

	// Additional provider-specific fields
	Metadata map[string]string
}

// ClusterState represents the Kubernetes cluster state
type ClusterState struct {
	// Cluster name
	Name string

	// Provider-specific cluster identifier (EKS ARN, GKE self-link, etc.)
	ID string

	// Kubernetes API endpoint
	Endpoint string

	// Kubernetes version
	Version string

	// Cluster status (ACTIVE, CREATING, DELETING, etc.)
	Status string

	// Certificate authority data for kubeconfig
	CAData string

	// Additional provider-specific fields
	Metadata map[string]string
}

// NodePoolState represents a node pool/group state
type NodePoolState struct {
	// Node pool name
	Name string

	// Provider-specific identifier
	ID string

	// Instance/machine type
	InstanceType string

	// Autoscaling configuration
	MinSize     int
	MaxSize     int
	DesiredSize int

	// Current status
	Status string

	// Kubernetes labels
	Labels map[string]string

	// Kubernetes taints
	Taints []Taint

	// GPU enabled
	GPU bool

	// Spot instances enabled
	Spot bool

	// Additional provider-specific fields
	Metadata map[string]string
}

// Taint represents a Kubernetes taint
type Taint struct {
	Key    string
	Value  string
	Effect string // NoSchedule, PreferNoSchedule, NoExecute
}

// StorageState represents shared storage state (EFS, Filestore, Azure Files, etc.)
type StorageState struct {
	// Provider-specific storage ID
	ID string

	// Storage type (efs, filestore, azurefiles, etc.)
	Type string

	// Mount targets or endpoints
	MountTargets []string

	// Status
	Status string

	// Additional provider-specific fields
	Metadata map[string]string
}
