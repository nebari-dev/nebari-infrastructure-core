package argocd

import (
	"fmt"
	"time"
)

const (
	// defaultChartVersion is the Argo CD Helm chart version NIC installs.
	// Chart 9.7.1 installs Argo CD v3.4.4. The previous pin, chart 9.4.1,
	// shipped Argo CD v3.3.0, which is affected by GHSA-3v3m-wc6v-x4x3
	// (critical), GHSA-h98r-wv3h-fr38 (high) and GHSA-rg3g-4rw9-gqrp
	// (medium); v3.4.4 is past the highest patch floor of those three (3.4.2).
	//
	// Keep this at v3.4 or later. Planned work (#499) needs glob expansion of
	// helm.valueFiles, which Argo CD gained in 3.4 (argoproj/argo-cd#26768,
	// cherry-picked to release-3.4 as #26919). Downgrading below 3.4 would
	// make that mechanism fail silently rather than error.
	defaultChartVersion = "9.7.1"
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
		Version:     defaultChartVersion, // Chart version that installs Argo CD v3.4.4
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
			// Upstream argo-cd ships resources: {} for every component, which
			// leaves all ArgoCD pods BestEffort. Defaults below come from the
			// #456 audit (idle usage plus chart-suggested values with headroom).
			// NOTE: Values changes only reach existing installs on the next
			// chart Version bump (see the Version field's doc comment).
			"controller":     helmResources("100m", "256Mi", "500m", "512Mi"),
			"repoServer":     helmResources("25m", "128Mi", "500m", "512Mi"),
			"server":         helmResources("25m", "64Mi", "200m", "128Mi"),
			"applicationSet": helmResources("25m", "64Mi", "200m", "128Mi"),
			"redis":          helmResources("25m", "64Mi", "200m", "128Mi"),
			"notifications":  helmResources("25m", "64Mi", "200m", "128Mi"),
			// NIC wires ArgoCD OIDC directly to Keycloak; the dex pod the
			// chart deploys by default is never referenced (#457).
			"dex": map[string]any{"enabled": false},
		},
	}
}

// helmResources builds a chart component's resources block. cpuLim may be
// empty to omit the CPU limit for burst-friendly components.
func helmResources(cpuReq, memReq, cpuLim, memLim string) map[string]any {
	limits := map[string]any{"memory": memLim}
	if cpuLim != "" {
		limits["cpu"] = cpuLim
	}
	return map[string]any{
		"resources": map[string]any{
			"requests": map[string]any{"cpu": cpuReq, "memory": memReq},
			"limits":   limits,
		},
	}
}
