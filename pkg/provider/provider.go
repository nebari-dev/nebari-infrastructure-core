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

	// Deploy deploys infrastructure based on the provided configuration
	// This is a high-level method that orchestrates resource creation
	Deploy(ctx context.Context, config *config.NebariConfig) error

	// Reconcile compares desired config against actual state and reconciles differences
	// This is the core stateless reconciliation method
	Reconcile(ctx context.Context, config *config.NebariConfig) error

	// Destroy tears down all infrastructure in reverse order
	Destroy(ctx context.Context, config *config.NebariConfig) error

	// GetKubeconfig generates a kubeconfig file for accessing the Kubernetes cluster
	GetKubeconfig(ctx context.Context, config *config.NebariConfig) ([]byte, error)
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
