// Package longhorn installs and manages Longhorn distributed block storage on
// a Kubernetes cluster. It is provider-agnostic and intended to be consumed by
// any NIC provider that needs RWO/RWX storage without a managed cloud offering
// (e.g. on-prem, Hetzner via hetzner-k3s, kind/k3d for development).
package longhorn

const (
	// StorageClassName is the Longhorn-managed StorageClass providers should
	// surface to downstream charts when Longhorn is enabled.
	StorageClassName = "longhorn"

	// Namespace is the Kubernetes namespace Longhorn is installed into.
	Namespace = "longhorn-system"

	// ReleaseName is the Helm release name used for Longhorn.
	ReleaseName = "longhorn"

	// ChartVersion pins the upstream Longhorn Helm chart version. Bump
	// together with iscsiDaemonSetYAML when upgrading.
	// v1.11.2 (released 2026-05-05) includes the (*Controller).Snapshot
	// nil-pointer panic fix from longhorn/longhorn#12081.
	ChartVersion = "1.11.2"

	chartRepoName = "longhorn"
	chartRepoURL  = "https://charts.longhorn.io"
	chartName     = "longhorn/longhorn"

	defaultReplicaCount = 2
)

// Config carries the user-tunable Longhorn settings shared across providers.
//
// A nil *Config means "do not install" (see IsEnabled). When the user supplies
// a non-nil Config, Enabled defaults to true so an empty block (`longhorn: {}`)
// is the minimal opt-in. ReplicaCount defaults to 2 — appropriate for small
// clusters; production deploys should raise it.
type Config struct {
	Enabled        *bool             `yaml:"enabled,omitempty"`
	ReplicaCount   int               `yaml:"replica_count,omitempty"`
	DedicatedNodes bool              `yaml:"dedicated_nodes,omitempty"`
	NodeSelector   map[string]string `yaml:"node_selector,omitempty"`
}

// IsEnabled returns whether Longhorn should be installed. A nil Config (i.e.
// the user omitted the longhorn block entirely) is treated as disabled so
// providers can opt-in by setting a non-nil zero-value Config; an explicit
// Enabled=false also disables.
func (c *Config) IsEnabled() bool {
	if c == nil {
		return false
	}
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// Replicas returns the configured replica count, falling back to the default
// when the field is unset or zero.
func (c *Config) Replicas() int {
	if c == nil || c.ReplicaCount == 0 {
		return defaultReplicaCount
	}
	return c.ReplicaCount
}
