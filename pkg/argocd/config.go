package argocd

import (
	"fmt"
	"time"
)

const (
	defaultChartVersion = "9.4.1"
	defaultNamespace    = "argocd"
)

// cnpgClusterHealthLua teaches Argo CD how to read the health of a
// postgresql.cnpg.io Cluster. Without it Argo CD has no health check for the
// CRD and reports every Cluster as Healthy the moment it is created, which
// makes the UI and `argocd app get` useless for diagnosing a database that is
// still bootstrapping or stuck.
//
// Keyed on .status.phase, whose values are string constants in CNPG's
// api/v1/cluster_types.go (PhaseHealthy, PhaseUnrecoverable, ...). Anything
// not explicitly terminal is Progressing, so a phase added by a future
// operator release degrades to "still working" rather than a false Healthy.
const cnpgClusterHealthLua = `local hs = {}
if obj.status == nil or obj.status.phase == nil or obj.status.phase == "" then
  hs.status = "Progressing"
  hs.message = "Waiting for the CloudNativePG operator to report a phase"
  return hs
end
hs.message = obj.status.phase
if obj.status.phase == "Cluster in healthy state" then
  hs.status = "Healthy"
elseif obj.status.phase == "Cluster is unrecoverable and needs manual intervention"
    or obj.status.phase == "Invalid cluster definition"
    or obj.status.phase == "Unable to create required cluster objects"
    or obj.status.phase == "Cluster has incomplete or invalid image catalog"
    or obj.status.phase == "Waiting for user action" then
  hs.status = "Degraded"
else
  hs.status = "Progressing"
end
return hs
`

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

	// Group names are matched both with and without a leading slash because the
	// Keycloak group-membership mapper's full.path setting differs by deployment
	// phase: the realm-setup job creates it with full.path=false ("argocd-admins"),
	// but the data-science-pack rbac-bootstrap job reconciles it to full.path=true
	// ("/argocd-admins") on every sync, which JupyterHub requires for shared-dir
	// mounts. Matching both keeps ArgoCD access working regardless of which ran last.
	rbacPolicy := `g, argocd-admins, role:admin
g, /argocd-admins, role:admin
g, argocd-viewers, role:readonly
g, /argocd-viewers, role:readonly`

	configs := cfg.Values["configs"].(map[string]any)
	// Merge into the cm map from DefaultConfig rather than replacing it, so the
	// resource health customizations set there survive the OIDC path.
	cm := configs["cm"].(map[string]any)
	cm["url"] = argocdURL
	cm["oidc.config"] = oidcConfig
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
				"cm": map[string]any{
					"resource.customizations.health.postgresql.cnpg.io_Cluster": cnpgClusterHealthLua,
				},
			},
		},
	}
}
