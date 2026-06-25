package openshift

import "github.com/nebari-dev/nebari-infrastructure-core/pkg/kubeconfig"

// Mode values for the OpenShift provider.
const (
	// ModeProvision provisions a fresh ROSA HCP cluster via OpenTofu.
	ModeProvision = "provision"
	// ModeExisting connects to an already-running OpenShift cluster via kubeconfig.
	ModeExisting = "existing"

	// defaultStorageClass is the native ROSA CSI StorageClass. Used when the
	// config leaves storage_class unset. Longhorn (privileged, SCC-heavy) stays
	// opt-in and is discouraged on OpenShift.
	defaultStorageClass = "gp3-csi"
)

// Compute describes the worker node pool for provision mode.
type Compute struct {
	InstanceType string `yaml:"instance_type,omitempty"`
	Replicas     int    `yaml:"replicas,omitempty"`
}

// LonghornConfig opts the provider into installing Longhorn. Off by default;
// Longhorn requires privileged SecurityContextConstraints on OpenShift.
type LonghornConfig struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

// SCCConfig controls the SecurityContextConstraints bootstrap. Nebari's upstream
// foundational charts pin a fixed UID and a seccomp profile, a combination only
// the privileged SCC permits on OpenShift, so SCC bootstrap is enabled by default.
type SCCConfig struct {
	// Manage enables/disables applying SCC bindings. Pointer so an unset value
	// defaults to true (managed). Set to false to manage SCCs out-of-band.
	Manage *bool `yaml:"manage,omitempty"`
	// Name is the SCC granted to the foundational namespaces. Defaults to
	// "privileged" (the only stock SCC allowing fixed-UID + seccomp pods like
	// argocd-redis). Override with a custom least-privilege SCC if desired.
	Name string `yaml:"name,omitempty"`
}

// Config is the cluster.openshift provider configuration. It is dual-mode: a
// `mode` discriminator selects between provisioning a ROSA HCP cluster
// (provision) and targeting an existing OpenShift cluster (existing). Fields not
// relevant to the active mode are ignored.
type Config struct {
	// ModeField selects provision (default) or existing. Exposed via Mode().
	ModeField string `yaml:"mode,omitempty"`

	// --- provision mode ---
	Region            string   `yaml:"region,omitempty"`
	OpenShiftVersion  string   `yaml:"openshift_version,omitempty"`
	AvailabilityZones []string `yaml:"availability_zones,omitempty"`
	Compute           Compute  `yaml:"compute,omitempty"`
	MachineCIDR       string   `yaml:"machine_cidr,omitempty"`
	StateBucket       string   `yaml:"state_bucket,omitempty"`

	// --- existing mode ---
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
	Context    string `yaml:"context,omitempty"`

	// --- shared ---
	StorageClass string         `yaml:"storage_class,omitempty"`
	SCC          SCCConfig      `yaml:"scc,omitempty"`
	Longhorn     LonghornConfig `yaml:"longhorn,omitempty"`
}

// Mode returns the configured mode, defaulting to provision when unset.
func (c *Config) Mode() string {
	if c.ModeField == "" {
		return ModeProvision
	}
	return c.ModeField
}

// StorageClassOrDefault returns the configured storage class, or the native CSI
// default (gp3-csi) when unset.
func (c *Config) StorageClassOrDefault() string {
	if c.StorageClass != "" {
		return c.StorageClass
	}
	return defaultStorageClass
}

// LonghornEnabled reports whether Longhorn install was opted into.
func (c *Config) LonghornEnabled() bool {
	return c.Longhorn.Enabled
}

// SCCManageEnabled reports whether NIC should apply SCC bindings. Defaults to
// true when unset.
func (c *Config) SCCManageEnabled() bool {
	if c.SCC.Manage == nil {
		return true
	}
	return *c.SCC.Manage
}

// SCCName returns the SCC to grant foundational namespaces, defaulting to
// "privileged".
func (c *Config) SCCName() string {
	if c.SCC.Name != "" {
		return c.SCC.Name
	}
	return defaultSCCName
}

// GetKubeconfigPath returns the configured kubeconfig path (existing mode), or
// the default resolution (KUBECONFIG env → ~/.kube/config) when unset.
func (c *Config) GetKubeconfigPath() (string, error) {
	if c.Kubeconfig != "" {
		return c.Kubeconfig, nil
	}
	return kubeconfig.GetPath()
}
