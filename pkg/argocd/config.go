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

	// controllerMemLimitMiB is the application-controller's memory limit (1024
	// MiB, which the API server canonicalises to the 1Gi kubectl reports), and
	// goMemLimitPercent the share of it handed to GOMEMLIMIT as a soft ceiling.
	// GOMEMLIMIT is derived rather than written out separately so the two
	// cannot drift: raising the limit on its own would just move the OOM
	// threshold without giving the Go runtime a reason to collect sooner.
	// See controllerValues for why these numbers are what they are.
	controllerMemLimitMiB = 1024
	goMemLimitPercent     = 90
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
			"controller":     controllerValues(),
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

// controllerValues sizes the application-controller, which needs more headroom
// than the rest of the chart's components because its working set tracks the
// number of Kubernetes objects it caches rather than the number of Applications
// NIC creates. The #456 audit measured it on a single-node kind cluster, where
// a 512Mi limit was ample; on EKS the same pod idles at 232-287Mi but spikes
// past 512Mi during reconciliation and was OOMKilled repeatedly, because a
// managed cluster carries far more API objects (cloud controllers, Longhorn
// CRDs and CRs) and the controller watches all of them.
//
// GOMEMLIMIT is Argo CD's documented mitigation for exactly this: a soft
// ceiling that makes the Go runtime collect before the kubelet's hard limit
// kills the pod. Argo CD's HA guide recommends 80-90% of the container limit
// and warns that setting it near the live working set causes GC thrashing,
// which 921MiB against a ~290Mi working set stays well clear of. See
// https://argo-cd.readthedocs.io/en/latest/operator-manual/high_availability/
//
// Installs managing many more resources should raise controllerMemLimitMiB;
// GOMEMLIMIT follows automatically.
func controllerValues() map[string]any {
	v := helmResources("100m", "512Mi", "500m", fmt.Sprintf("%dMi", controllerMemLimitMiB))
	// The Go runtime spells its byte suffixes MiB, not Kubernetes' Mi, and it
	// throws on a malformed value during gcinit rather than falling back to no
	// limit. Getting this wrong crash-loops the controller before it serves a
	// single request, so the suffix is asserted in TestControllerGoMemLimit.
	v["env"] = []map[string]any{
		{"name": "GOMEMLIMIT", "value": fmt.Sprintf("%dMiB", controllerMemLimitMiB*goMemLimitPercent/100)},
	}
	return v
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
