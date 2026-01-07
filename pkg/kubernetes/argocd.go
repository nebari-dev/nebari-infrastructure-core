package kubernetes

import "time"

// argoCDConfig holds configuration for Argo CD installation
type argoCDConfig struct {
	// Version is the Argo CD chart version to install
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

// defaultArgoCDConfig returns the default Argo CD configuration
func defaultArgoCDConfig() argoCDConfig {
	return argoCDConfig{
		Version:     "7.7.9", // Chart version that installs Argo CD v2.11.0
		Namespace:   "argocd",
		ReleaseName: "argocd",
		Timeout:     5 * time.Minute,
		Values:      map[string]any{
			// Minimal default configuration
			// Users can customize Argo CD after installation
		},
	}
}
