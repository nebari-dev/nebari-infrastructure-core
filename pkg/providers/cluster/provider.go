package cluster

import (
	"context"
	"time"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
)

// BackupBucketSpec describes an object-storage bucket/container the provider's
// Terraform module should provision for Longhorn backups. A nil *BackupBucketSpec
// in DeployOptions means "do not provision" (external or pre-existing bucket).
type BackupBucketSpec struct {
	// Name is the S3 bucket name (AWS) or storage container name (Azure).
	Name string
	// StorageAccount is the Azure storage account name (Azure only; empty for AWS).
	StorageAccount string
	// Create asks the module to provision the bucket/container. When false the
	// spec still carries the bucket Name for other wiring (e.g. scoping the Pod
	// Identity IAM policy to a pre-existing bucket), but no bucket is created.
	Create bool
	// PodIdentity asks the module to provision a keyless IAM-role association
	// (EKS Pod Identity) for Longhorn's service account, scoped to Name. AWS-only;
	// set when the S3 target uses keyless auth (no static credentials).
	PodIdentity bool
	// ForceDestroy allows `tofu destroy` to remove a non-empty bucket. Derived
	// from the inverse of the target's retain_on_destroy (default false => retain).
	// Only meaningful when Create is true.
	ForceDestroy bool
}

// DeployOptions holds runtime flags for infrastructure deployment.
type DeployOptions struct {
	DryRun  bool
	Timeout time.Duration

	// TrustBundle is the resolved top-level CA bundle (base64-encoded PEM),
	// passed so providers can apply it without seeing the full NebariConfig.
	TrustBundle string

	// BackupBucket, when non-nil, asks the provider to provision a Longhorn
	// backup bucket/container in its Terraform module.
	BackupBucket *BackupBucketSpec
}

// DestroyOptions holds runtime flags for infrastructure destruction.
type DestroyOptions struct {
	DryRun  bool
	Force   bool
	Timeout time.Duration

	// TrustBundle mirrors DeployOptions.TrustBundle; destroy must compute the
	// same tofu variables as deploy for the plan to match.
	TrustBundle string

	// BackupBucket, when non-nil, describes a NIC-provisioned Longhorn backup
	// bucket/container. When its ForceDestroy is false (retain_on_destroy on),
	// providers remove it from Terraform state before destroy so it (and its
	// backups) survive teardown.
	BackupBucket *BackupBucketSpec
}

// InfraSettings describes provider-specific Kubernetes infrastructure settings.
// The ArgoCD writer and deploy command use these to configure templates
// without importing provider packages or switching on provider names.
type InfraSettings struct {
	// StorageClass is the default Kubernetes StorageClass for persistent volumes.
	// Examples: "gp2" (AWS), "hcloud-volumes" (Hetzner), "standard" (local)
	StorageClass string

	// NeedsMetalLB indicates whether this provider requires MetalLB for load balancing.
	// Cloud providers with native LBs return false; local provider returns true.
	NeedsMetalLB bool

	// LoadBalancerAnnotations are added to the Gateway's provisioned LoadBalancer Service.
	// Used by providers whose cloud controller manager requires annotations
	// (e.g., {"load-balancer.hetzner.cloud/location": "ash"}).
	LoadBalancerAnnotations map[string]string

	// MetalLBAddressPool is the IP range for MetalLB's IPAddressPool.
	// Only used when NeedsMetalLB is true (e.g., "192.168.1.100-192.168.1.110").
	MetalLBAddressPool string

	// KeycloakBasePath is appended to the Keycloak service URL for the operator.
	// Most providers leave this empty. Providers using the Keycloak legacy chart
	// (keycloakx) need "/auth" because that chart serves under the /auth context path,
	// while the modern Bitnami chart serves at the root.
	KeycloakBasePath string

	// HTTPSPort is the port for the Gateway's HTTPS listener and HTTP-to-HTTPS redirects.
	// Leave as 0 for the standard port 443. NewTemplateData normalizes 0 to 443.
	// Providers running behind a non-standard HTTPS port (e.g., development on 8443)
	// can override this.
	HTTPSPort int

	// EFSStorageClass is the name of the EFS-backed StorageClass, if available.
	// Empty when EFS is not enabled. Software packs can use this for ReadWriteMany
	// volumes (e.g., shared model storage for LLM serving).
	EFSStorageClass string

	// SupportsLocalGitOps indicates whether this provider can use local file:// git repos.
	// True for providers where cluster nodes can access host filesystem paths (local, kind, k3s).
	// Cloud providers (AWS, GCP, Azure) return false - their nodes can't see the dev machine's FS.
	SupportsLocalGitOps bool

	// LonghornEnabled indicates whether the Longhorn distributed block storage
	// (and therefore the Longhorn UI) is deployed by this provider for the given
	// cluster config. Used by the foundational deploy flow to decide whether to
	// expose longhorn.<domain> through the gateway and provision an OIDC client.
	LonghornEnabled bool

	// CrossplaneCapabilities is the set of Crossplane provider capabilities the
	// provider has been explicitly authorized to install, keyed by capability id
	// (e.g. "aws-s3", "aws-iam", "aws-eks"). Provider-agnostic: each cloud
	// provider populates the ids required by the capabilities the admin opted
	// into, including internal dependencies. Shared orchestration gates provider
	// manifests on membership in this set instead of branching on the cluster
	// provider name. A nil map means none enabled.
	CrossplaneCapabilities map[string]bool
}

// CrossplaneEnabled reports whether any Crossplane capability has been
// authorized. Crossplane is provider/profile-conditional foundational software
// (ADR-0012 §3): with no capability enabled the entire install is omitted, so
// the foundational manifests (core chart, providers Application,
// provider-keycloak) are only written when this returns true.
func (s InfraSettings) CrossplaneEnabled() bool {
	for _, enabled := range s.CrossplaneCapabilities {
		if enabled {
			return true
		}
	}
	return false
}

// Provider defines the interface that all cloud providers must implement.
//
// This interface establishes the abstraction boundary between CLI commands and
// provider implementations. CLI commands depend only on this interface, never on
// concrete provider types, enabling new providers to be added without modifying
// CLI code (Open/Closed Principle).
//
// Methods receive only the data they need (projectName, ClusterConfig, options)
// rather than the full NebariConfig, so providers never see DNS, certificate,
// or git repository settings.
type Provider interface {
	// Name returns the short provider identifier used in CLI output, logging,
	// and OpenTelemetry span attributes (e.g., "aws", "gcp", "azure", "local").
	Name() string

	// Validate checks that the configuration is valid before any infrastructure
	// operations. This includes verifying required fields, validating formats,
	// and checking cloud-specific constraints (e.g., valid regions, instance types).
	Validate(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) error

	// Deploy creates or updates infrastructure to match the desired configuration.
	// Implementations are responsible for idempotency: running Deploy multiple times
	// with the same config should converge to the same infrastructure state. The
	// backing tool is provider-specific (e.g., OpenTofu for AWS, hetzner-k3s for
	// Hetzner). Use DeployOptions.DryRun to preview changes without applying them.
	Deploy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts DeployOptions) error

	// Destroy tears down all infrastructure resources in the correct order,
	// respecting dependencies (e.g., node groups before cluster, cluster before VPC).
	// The backing tool is provider-specific.
	Destroy(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig, opts DestroyOptions) error

	// GetKubeconfig generates a kubeconfig file for authenticating with the
	// Kubernetes cluster. The returned bytes can be written to a file or used
	// directly with Kubernetes client libraries.
	GetKubeconfig(ctx context.Context, projectName string, clusterConfig *config.ClusterConfig) ([]byte, error)

	// Summary returns key-value pairs describing provider-specific configuration
	// for display purposes. This allows CLI commands to show details like region
	// or project in confirmation prompts without importing provider packages.
	// Used in destructive operations to help users confirm they're targeting
	// the correct infrastructure (e.g., distinguishing clusters with the same
	// name in different regions).
	Summary(clusterConfig *config.ClusterConfig) map[string]string

	// InfraSettings returns provider-specific Kubernetes infrastructure settings.
	// CLI commands and the ArgoCD writer use these to configure templates
	// without importing provider packages or switching on provider names.
	InfraSettings(clusterConfig *config.ClusterConfig) InfraSettings
}
