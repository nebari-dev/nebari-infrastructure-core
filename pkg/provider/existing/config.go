package existing

import (
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"
)

// Config represents configuration for connecting to a pre-existing Kubernetes cluster.
type Config struct {
	// Kubeconfig is the path to the kubeconfig file.
	// Defaults to KUBECONFIG env or ~/.kube/config when empty.
	Kubeconfig string `yaml:"kubeconfig,omitempty"`

	// Context is the name of the context entry in the kubeconfig file.
	// Required — must be explicitly set to avoid accidentally deploying
	// to the wrong cluster.
	Context string `yaml:"context"`

	// StorageClass is the default Kubernetes StorageClass for persistent volumes.
	// Defaults to "standard" when empty.
	StorageClass string `yaml:"storage_class,omitempty"`

	// LoadBalancerAnnotations are added to the Gateway's LoadBalancer Service.
	// Use this to pass cloud-specific annotations the Cloud Controller Manager may require for
	// provisioning LoadBalancers (e.g., "load-balancer.hetzner.cloud/location: ash").
	LoadBalancerAnnotations map[string]string `yaml:"load_balancer_annotations,omitempty"`
}

const defaultStorageClass = "standard"

// GetStorageClass returns the configured storage class or the default.
func (c *Config) GetStorageClass() string {
	if c.StorageClass != "" {
		return c.StorageClass
	}
	return defaultStorageClass
}

// GetKubeconfigPath returns the configured kubeconfig path or the default
// (KUBECONFIG env → ~/.kube/config).
func (c *Config) GetKubeconfigPath() (string, error) {
	if c.Kubeconfig != "" {
		return c.Kubeconfig, nil
	}
	return kubeconfig.GetPath()
}
