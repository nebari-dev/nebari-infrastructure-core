package local

// Config represents configuration for local Kubernetes deployments (K3s, kind, minikube).
// Used for development and testing, or for "bring your own cluster" scenarios.
type Config struct {
	// KubeContext specifies which kubectl context to use (from ~/.kube/config)
	KubeContext string `yaml:"kube_context,omitempty"`
	// NodeSelectors maps workload types to node label selectors for scheduling
	NodeSelectors map[string]map[string]string `yaml:"node_selectors,omitempty"`
	// AdditionalFields captures any extra local-specific configuration
	AdditionalFields map[string]any `yaml:",inline"`
}
