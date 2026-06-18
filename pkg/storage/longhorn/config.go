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

	// NodeStorageLabel marks a node as a dedicated Longhorn storage node. It is
	// the default Config.NodeSelector and the label providers put on their
	// storage node group.
	NodeStorageLabel = "node.longhorn.io/storage"

	// CreateDefaultDiskLabel is the label Longhorn requires on a node before it
	// auto-provisions a default disk at /var/lib/longhorn (paired with the
	// createDefaultDiskLabeledNodes setting this package enables for dedicated
	// nodes). CONTRACT: when DedicatedNodes is true, every storage node group
	// MUST carry this label, or Longhorn creates no disks and all volumes fault
	// with ReplicaSchedulingFailure (#369). The AWS provider injects it
	// automatically; other providers must add it to their storage pool.
	CreateDefaultDiskLabel = "node.longhorn.io/create-default-disk"

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
	Enabled      *bool `yaml:"enabled,omitempty"`
	ReplicaCount int   `yaml:"replica_count,omitempty"`

	// DedicatedNodes confines Longhorn replica data to a dedicated, tainted
	// storage node group (disks are created only there; see NodeSelector and
	// CreateDefaultDiskLabel).
	//
	// WARNING — toggling this is a MANUAL migration, not a hands-off switch.
	// The setting only governs FUTURE default-disk creation; it never moves or
	// re-syncs replicas that already exist, and neither NIC nor Longhorn migrates
	// them for you:
	//   - false -> true: existing replicas stay on the colocated (general/user)
	//     node disks — Longhorn does not delete those disks on a setting change.
	//     Those nodes then cannot scale down (they still hold replicas) and the
	//     data is NOT auto-moved to the storage nodes. To finish the migration,
	//     manually evict the old disks/nodes in Longhorn (allowScheduling:false
	//     or evictionRequested) so replicas rebuild onto the storage nodes first.
	//   - true -> false while also removing the storage node group: terraform
	//     tears down the nodes holding the only replicas before they are rebuilt
	//     elsewhere -> DATA LOSS (this is the #354 node-removal hazard). Migrate
	//     replicas off the storage nodes (evict, wait for rebuild) BEFORE removing
	//     the group.
	//
	// Before switching modes, take a fresh OFF-CLUSTER backup (the
	// nebari-longhorn-backup-pack provides scheduled S3 backups; trigger an
	// on-demand one right before the switch). In-cluster snapshots do not survive
	// the destructive true->false case since they live on the disks being removed;
	// only the S3 backup does, and you can restore onto the new topology.
	DedicatedNodes bool `yaml:"dedicated_nodes,omitempty"`
	// NodeSelector is the label set that identifies the dedicated storage nodes
	// when DedicatedNodes is true (defaults to {node.longhorn.io/storage: "true"},
	// i.e. NodeStorageLabel). It no longer pins Longhorn's system components by
	// node selector — pinning broke PVC mounts on workload nodes (#366). Instead
	// it tells the provider which node group is the storage pool, so the provider
	// can add CreateDefaultDiskLabel to it; Longhorn then provisions disks only
	// there, which is what confines replicas to storage nodes.
	//
	// Matching is an exact key/value comparison: a storage node group labeled
	// node.longhorn.io/storage="yes" (any value other than the configured one)
	// will not match, so it gets no disk label and its volumes fault. Use the
	// exact values set here.
	//
	// Constraint: the taint toleration is NOT derived from this field. Longhorn's
	// system components tolerate every taint (see tolerateAllTaints in install.go),
	// so the storage nodes' taint - and any taint on a workload pool where a PVC
	// must mount - is covered regardless of what you set here. This selector only
	// controls which nodes are labeled as the storage (replica) pool.
	NodeSelector map[string]string `yaml:"node_selector,omitempty"`

	// ClusterAutoscalerEnabled tells Longhorn whether the cluster runs the
	// Kubernetes Cluster Autoscaler. It is not user-facing so providers set it
	// from their own autoscaler config (e.g. the AWS provider derives it from
	// ClusterAutoscalerEnabled). When non-nil, it renders the Longhorn
	// `kubernetesClusterAutoscalerEnabled` setting, which makes Longhorn mark
	// instance-manager pods safe-to-evict on nodes holding no replicas/engines
	// so the autoscaler can scale those nodes in. nil leaves the setting unset
	// (Longhorn's default of false), so non-autoscaled providers are unaffected.
	ClusterAutoscalerEnabled *bool `yaml:"-"`
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

// WithClusterAutoscalerEnabled returns a copy of the config with
// ClusterAutoscalerEnabled set to enabled, without mutating the receiver.
// A nil receiver yields a fresh Config (the "use defaults" case), so a provider
// can chain this onto an optional, user-owned *Config without aliasing or
// mutating it. A shallow copy is sufficient: only the new pointer field is set,
// and the shared map/pointer fields are read-only downstream.
func (c *Config) WithClusterAutoscalerEnabled(enabled bool) *Config {
	out := &Config{}
	if c != nil {
		cp := *c
		out = &cp
	}
	out.ClusterAutoscalerEnabled = &enabled
	return out
}
