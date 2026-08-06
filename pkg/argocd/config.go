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
	// Keep this at v3.4 or later. The valueFiles overlay seam (#499,
	// ADR-0014) needs glob expansion of helm.valueFiles, which Argo CD
	// gained in 3.4 (argoproj/argo-cd#26768, cherry-picked to release-3.4
	// as #26919). Downgrading below 3.4 would make overlays fail silently
	// rather than error.
	defaultChartVersion = "9.7.1"
	defaultNamespace    = "argocd"

	// Memory limits for the two components whose usage spikes far above idle,
	// in MiB. 1024 MiB is what the API server canonicalises to the 1Gi that
	// kubectl reports. Both were measured on a 14-app EKS cluster: see
	// goMemLimited for why each is what it is.
	controllerMemLimitMiB = 1024
	repoServerMemLimitMiB = 1024

	// goMemLimitPercent is the share of a component's memory limit handed to
	// GOMEMLIMIT as a soft ceiling. Argo CD's HA guide recommends 80-90%.
	goMemLimitPercent = 90
)

// cnpgClusterHealthLua teaches Argo CD how to read the health of a
// postgresql.cnpg.io Cluster. Without it Argo CD has no health check for the
// CRD and reports every Cluster as Healthy the moment it is created, which
// makes the UI and `argocd app get` useless for diagnosing a database that is
// still bootstrapping or stuck.
//
// This check is also load-bearing for sync ordering: the keycloak-db Cluster
// sits at sync-wave -1 inside the keycloak Application so that the wave-0
// StatefulSet is only applied once the database is Healthy and its generated
// keycloak-db-app Secret exists (issue #537). Removing this customization
// would make every Cluster Healthy-on-create again and silently disable that
// gate.
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
				"cm": map[string]any{
					"resource.customizations.health.postgresql.cnpg.io_Cluster": cnpgClusterHealthLua,
				},
			},
			// Upstream argo-cd ships resources: {} for every component, which
			// leaves all ArgoCD pods BestEffort. Defaults below come from the
			// #456 audit (idle usage plus chart-suggested values with headroom).
			// NOTE: Values changes only reach existing installs on the next
			// chart Version bump (see the Version field's doc comment).
			"controller":     goMemLimited("100m", "512Mi", "500m", controllerMemLimitMiB),
			"repoServer":     goMemLimited("25m", "128Mi", "500m", repoServerMemLimitMiB),
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

// goMemLimited sizes a component that needs a hard memory limit plus a
// GOMEMLIMIT soft ceiling derived from it, and it applies to the two Argo CD
// components whose memory spikes far above idle rather than tracking it.
//
// The #456 audit measured both on a single-node kind cluster, where a 512Mi
// limit looked generous. On a 14-app EKS cluster both blew through it and were
// OOMKilled, because what drives each of them is absent from a kind cluster:
//
//   - application-controller: its working set follows the number of Kubernetes
//     objects it caches, not the number of Applications NIC creates. A managed
//     cluster carries far more API objects (cloud controllers, storage CRDs and
//     their CRs) and it watches all of them. Measured peak 600MiB, idling at
//     ~320MiB.
//   - repo-server: renders every app's manifests when its cache is cold, which
//     on a fresh deploy means all of them at once. Measured peak 661MiB against
//     an idle of ~45MiB, so idle measurements say nothing about what it needs.
//
// GOMEMLIMIT is Argo CD's documented mitigation for exactly this: a soft ceiling
// that makes the Go runtime collect before the kubelet's hard limit kills the
// pod. Their HA guide recommends 80-90% of the container limit and warns that
// setting it near the live working set causes GC thrashing. At 1024MiB the
// ceiling lands at 921MiB, above both measured peaks, so GC has room to work
// instead of fighting the limit. Confirmed against the alternative: the
// repo-server also survives a 512Mi limit with GOMEMLIMIT at 460MiB, but only
// by holding its peak 156KiB under the limit, which is no margin at all.
// See https://argo-cd.readthedocs.io/en/latest/operator-manual/high_availability/
//
// Installs that manage many more resources should raise the relevant
// <component>MemLimitMiB; GOMEMLIMIT follows automatically. If the repo-server
// still runs out, Argo CD's other lever is --parallelismlimit, which bounds how
// many manifest generations run at once.
func goMemLimited(cpuReq, memReq, cpuLim string, memLimitMiB int) map[string]any {
	v := helmResources(cpuReq, memReq, cpuLim, fmt.Sprintf("%dMi", memLimitMiB))
	// The Go runtime spells its byte suffixes MiB, not Kubernetes' Mi, and it
	// throws on a malformed value during gcinit rather than falling back to no
	// limit. Getting this wrong crash-loops the container before it serves a
	// single request, so the suffix is asserted in TestGoMemLimitComponents.
	v["env"] = []map[string]any{
		{"name": "GOMEMLIMIT", "value": fmt.Sprintf("%dMiB", memLimitMiB*goMemLimitPercent/100)},
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
