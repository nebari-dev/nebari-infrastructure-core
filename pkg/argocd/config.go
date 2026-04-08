package argocd

import (
	"fmt"
	"time"
)

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

// ConfigWithOIDC returns an Argo CD configuration with Keycloak OIDC SSO enabled.
// It builds on DefaultConfig and adds OIDC provider config, RBAC policies mapping
// Keycloak groups to ArgoCD roles, and the client secret.
//
// The OIDC config references the client secret via $oidc.keycloak.clientSecret,
// which ArgoCD resolves from the argocd-secret Kubernetes Secret. The secret value
// is injected via configs.secret.extra in the Helm values.
func ConfigWithOIDC(domain, keycloakBasePath, clientSecret string) Config {
	cfg := DefaultConfig()

	issuerURL := fmt.Sprintf("https://keycloak.%s%s/realms/nebari", domain, keycloakBasePath)
	argocdURL := fmt.Sprintf("https://argocd.%s", domain)

	oidcConfig := fmt.Sprintf(`name: Keycloak
issuer: %s
clientID: argocd
clientSecret: $oidc.keycloak.clientSecret
requestedScopes:
  - openid
  - profile
  - email
  - groups`, issuerURL)

	rbacPolicy := `g, argocd-admins, role:admin
g, argocd-viewers, role:readonly`

	configs := cfg.Values["configs"].(map[string]any)
	configs["cm"] = map[string]any{
		"url":         argocdURL,
		"oidc.config": oidcConfig,
	}
	configs["rbac"] = map[string]any{
		"policy.default": "",
		"scopes":         "[groups]",
		"policy.csv":     rbacPolicy,
	}
	configs["secret"] = map[string]any{
		"extra": map[string]any{
			"oidc.keycloak.clientSecret": clientSecret,
		},
	}

	cfg.Values["configs"] = configs
	return cfg
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
