package argocd

import "time"

const (
	defaultChartVersion = "9.4.1"
	defaultNamespace    = "argocd"
)

// Config holds configuration for Argo CD installation
type Config struct {
	// Version is the Argo CD chart version to install.
	// IMPORTANT: The upgrade-skip logic only compares chart versions. If you modify
	// Values (e.g., Helm configuration parameters) without changing Version, those
	// changes will NOT be applied to existing installations. Bump Version to force
	// an upgrade when Values change.
	Version string

	// Namespace is the Kubernetes namespace to install Argo CD into
	Namespace string

	// ReleaseName is the Helm release name
	ReleaseName string

	// Timeout is the maximum time to wait for installation
	Timeout time.Duration

	// Values are custom Helm values to apply
	Values map[string]any
}

// DefaultConfig returns the default Argo CD configuration
func DefaultConfig() Config {
	return Config{
		Version:     defaultChartVersion, // Chart version that installs Argo CD v3.3.0
		Namespace:   defaultNamespace,
		ReleaseName: defaultNamespace,
		Timeout:     5 * time.Minute,
		Values: map[string]any{
			// Run in insecure mode since TLS is terminated at the gateway
			"configs": map[string]any{
				"params": map[string]any{
					"server.insecure": true,
				},
			},
		},
	}
}
