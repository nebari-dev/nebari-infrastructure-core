package hetzner

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Config holds Hetzner-specific provider configuration.
// Parsed from the "hetzner_cloud" key in nebari-config.yaml.
type Config struct {
	Location          string               `yaml:"location"`
	KubernetesVersion string               `yaml:"kubernetes_version"`
	NodeGroups        map[string]NodeGroup `yaml:"node_groups"`

	// ScheduleWorkloadsOnMasters controls whether application pods can be
	// scheduled on control-plane nodes. Defaults to true, which enables
	// single-node clusters and makes better use of small Hetzner instances.
	// Set to false for production clusters where you want dedicated masters
	// that only run etcd and the Kubernetes control plane. When false, at
	// least one non-master node group is required.
	ScheduleWorkloadsOnMasters *bool `yaml:"schedule_workloads_on_masters,omitempty"`

	// PersistData controls whether CSI volumes survive cluster destruction.
	// When true, volumes are labeled persist=true during deploy, and destroy
	// skips them. When false (the default), destroy deletes all CSI volumes
	// that are not attached to a running server.
	PersistData bool `yaml:"persist_data,omitempty"`

	SSH     *SSHConfig     `yaml:"ssh,omitempty"`
	Network *NetworkConfig `yaml:"network,omitempty"`
}

// NodeGroup defines a pool of Hetzner Cloud instances. Exactly one node group
// must have Master set to true to serve as the k3s control plane.
type NodeGroup struct {
	InstanceType string `yaml:"instance_type"`
	Count        int    `yaml:"count"`

	// Master marks this node group as the k3s control plane. Exactly one
	// node group must have this set to true. Master nodes run etcd and the
	// Kubernetes API server. Whether they also run application workloads is
	// controlled by Config.ScheduleWorkloadsOnMasters.
	Master bool `yaml:"master,omitempty"`

	// Location overrides the top-level location for this node group.
	// Only valid for worker (non-master) node groups.
	Location string `yaml:"location,omitempty"`

	Autoscaling *Autoscaling `yaml:"autoscaling,omitempty"`
}

// Autoscaling configures automatic node pool scaling.
type Autoscaling struct {
	Enabled      bool `yaml:"enabled"`
	MinInstances int  `yaml:"min_instances"`
	MaxInstances int  `yaml:"max_instances"`
}

// NetworkConfig controls firewall rules for SSH and Kubernetes API access.
// Defaults to 0.0.0.0/0 (open to all) if not specified - restrict these
// in production to your IP ranges.
type NetworkConfig struct {
	SSHAllowedCIDRs []string `yaml:"ssh_allowed_cidrs,omitempty"`
	APIAllowedCIDRs []string `yaml:"api_allowed_cidrs,omitempty"`
}

// SSHConfig allows users to provide their own SSH keys instead of auto-generated ones.
type SSHConfig struct {
	PublicKeyPath  string `yaml:"public_key_path"`
	PrivateKeyPath string `yaml:"private_key_path"`
}

// ScheduleOnMasters returns whether workloads should be scheduled on master nodes.
// Defaults to true if not explicitly set.
func (c *Config) ScheduleOnMasters() bool {
	if c.ScheduleWorkloadsOnMasters == nil {
		return true
	}
	return *c.ScheduleWorkloadsOnMasters
}

// MasterGroup returns the name and NodeGroup marked as master.
func (c *Config) MasterGroup() (string, NodeGroup) {
	for name, ng := range c.NodeGroups {
		if ng.Master {
			return name, ng
		}
	}
	return "", NodeGroup{}
}

// WorkerGroups returns all non-master node groups as a sorted slice of workerEntry.
// The order is deterministic (sorted by name) for template rendering.
func (c *Config) WorkerGroups() []workerEntry {
	var workers []workerEntry
	for name, ng := range c.NodeGroups {
		if !ng.Master {
			workers = append(workers, workerEntry{Name: name, NodeGroup: ng})
		}
	}
	// Sort for deterministic template output
	sortWorkers(workers)
	return workers
}

// workerEntry pairs a node group name with its configuration for template rendering.
type workerEntry struct {
	Name      string
	NodeGroup NodeGroup
}

// SSHAllowedNetworks returns the configured SSH CIDR ranges, defaulting to 0.0.0.0/0.
func (c *Config) SSHAllowedNetworks() []string {
	if c.Network != nil && len(c.Network.SSHAllowedCIDRs) > 0 {
		return c.Network.SSHAllowedCIDRs
	}
	return []string{"0.0.0.0/0"}
}

// APIAllowedNetworks returns the configured API CIDR ranges, defaulting to 0.0.0.0/0.
func (c *Config) APIAllowedNetworks() []string {
	if c.Network != nil && len(c.Network.APIAllowedCIDRs) > 0 {
		return c.Network.APIAllowedCIDRs
	}
	return []string{"0.0.0.0/0"}
}

// IsExplicitK3sVersion returns true if the kubernetes_version already contains
// a k3s revision suffix (e.g., "v1.32.0+k3s1"), meaning no API lookup is needed.
func (c *Config) IsExplicitK3sVersion() bool {
	return strings.Contains(c.KubernetesVersion, "+k3s")
}

// safeIdentifier matches alphanumeric strings with dots, hyphens, and underscores.
// Used to validate values interpolated into the cluster YAML template.
var safeIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// safeK8sVersion matches Kubernetes version strings:
//   - "1.32", "1.32.0" (short forms resolved via GitHub API)
//   - "v1.32.0+k3s1" (explicit k3s release tags)
var safeK8sVersion = regexp.MustCompile(`^v?\d+\.\d+(\.\d+)?(\+k3s\d+)?$`)

// Validate checks that all required fields are present and valid.
func (c *Config) Validate() error {
	if c.Location == "" {
		return fmt.Errorf("hetzner_cloud.location is required")
	}
	if !safeIdentifier.MatchString(c.Location) {
		return fmt.Errorf("hetzner_cloud.location %q contains invalid characters (must match %s)", c.Location, safeIdentifier.String())
	}
	if c.KubernetesVersion == "" {
		return fmt.Errorf("hetzner_cloud.kubernetes_version is required")
	}
	if !safeK8sVersion.MatchString(c.KubernetesVersion) {
		return fmt.Errorf("hetzner_cloud.kubernetes_version %q is invalid (expected MAJOR.MINOR, MAJOR.MINOR.PATCH, or vMAJOR.MINOR.PATCH+k3sN)", c.KubernetesVersion)
	}
	if len(c.NodeGroups) == 0 {
		return fmt.Errorf("hetzner_cloud.node_groups must have at least one group")
	}
	if err := c.validateNodeGroups(); err != nil {
		return err
	}
	if err := c.validateNetworkCIDRs(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateNodeGroups() error {
	masterCount := 0
	var masterName string
	workerCount := 0

	for name, ng := range c.NodeGroups {
		if !safeIdentifier.MatchString(name) {
			return fmt.Errorf("hetzner_cloud.node_groups[%q]: name contains invalid characters (must match %s)", name, safeIdentifier.String())
		}
		if ng.InstanceType == "" {
			return fmt.Errorf("hetzner_cloud.node_groups[%q].instance_type is required", name)
		}
		if !safeIdentifier.MatchString(ng.InstanceType) {
			return fmt.Errorf("hetzner_cloud.node_groups[%q].instance_type %q contains invalid characters", name, ng.InstanceType)
		}
		if ng.Count < 1 && (ng.Autoscaling == nil || !ng.Autoscaling.Enabled) {
			return fmt.Errorf("hetzner_cloud.node_groups[%q].count must be at least 1 (or enable autoscaling)", name)
		}
		if ng.Location != "" && !safeIdentifier.MatchString(ng.Location) {
			return fmt.Errorf("hetzner_cloud.node_groups[%q].location %q contains invalid characters", name, ng.Location)
		}
		if ng.Master {
			masterCount++
			masterName = name
			if ng.Location != "" {
				return fmt.Errorf("hetzner_cloud.node_groups[%q]: master node group uses the top-level location, remove the location override", name)
			}
			if ng.Autoscaling != nil && ng.Autoscaling.Enabled {
				return fmt.Errorf("hetzner_cloud.node_groups[%q]: master node group does not support autoscaling", name)
			}
		} else {
			workerCount++
		}
		if ng.Autoscaling != nil && ng.Autoscaling.Enabled {
			if ng.Autoscaling.MinInstances < 0 {
				return fmt.Errorf("hetzner_cloud.node_groups[%q].autoscaling.min_instances must not be negative", name)
			}
			if ng.Autoscaling.MaxInstances < 1 {
				return fmt.Errorf("hetzner_cloud.node_groups[%q].autoscaling.max_instances must be at least 1", name)
			}
			if ng.Autoscaling.MinInstances > ng.Autoscaling.MaxInstances {
				return fmt.Errorf("hetzner_cloud.node_groups[%q].autoscaling.min_instances (%d) must not exceed max_instances (%d)",
					name, ng.Autoscaling.MinInstances, ng.Autoscaling.MaxInstances)
			}
		}
	}

	if masterCount == 0 {
		return fmt.Errorf("hetzner_cloud.node_groups: exactly one node group must have master: true")
	}
	if masterCount > 1 {
		return fmt.Errorf("hetzner_cloud.node_groups: only one node group can have master: true, found %d", masterCount)
	}

	_, master := c.MasterGroup()
	if master.Count > 1 && master.Count%2 == 0 {
		return fmt.Errorf("hetzner_cloud.node_groups[%q].count should be odd (1, 3, 5) for k3s HA with embedded etcd; got %d", masterName, master.Count)
	}

	if workerCount == 0 && !c.ScheduleOnMasters() {
		return fmt.Errorf("hetzner_cloud.node_groups must include at least one non-master group when schedule_workloads_on_masters is false")
	}

	return nil
}

// validateNetworkCIDRs checks that user-provided CIDR values are syntactically valid.
func (c *Config) validateNetworkCIDRs() error {
	if c.Network == nil {
		return nil
	}
	for i, cidr := range c.Network.SSHAllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("hetzner_cloud.network.ssh_allowed_cidrs[%d]: invalid CIDR %q: %w", i, cidr, err)
		}
	}
	for i, cidr := range c.Network.APIAllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("hetzner_cloud.network.api_allowed_cidrs[%d]: invalid CIDR %q: %w", i, cidr, err)
		}
	}
	return nil
}

// NetworkWarnings returns warnings about network configuration that are not errors
// but should be communicated to the user (e.g., open-to-internet defaults).
func (c *Config) NetworkWarnings() []string {
	var warnings []string
	if c.Network == nil || len(c.Network.SSHAllowedCIDRs) == 0 {
		warnings = append(warnings, "SSH access defaults to 0.0.0.0/0 (open to all) - restrict with hetzner_cloud.network.ssh_allowed_cidrs for production")
	}
	if c.Network == nil || len(c.Network.APIAllowedCIDRs) == 0 {
		warnings = append(warnings, "Kubernetes API access defaults to 0.0.0.0/0 (open to all) - restrict with hetzner_cloud.network.api_allowed_cidrs for production")
	}
	return warnings
}

// sortWorkers sorts worker entries by name for deterministic output.
func sortWorkers(workers []workerEntry) {
	for i := 1; i < len(workers); i++ {
		for j := i; j > 0 && workers[j].Name < workers[j-1].Name; j-- {
			workers[j], workers[j-1] = workers[j-1], workers[j]
		}
	}
}
