package hetzner

import (
	"fmt"
	"strings"
)

// Config holds Hetzner-specific provider configuration.
// Parsed from the "hetzner_cloud" key in nebari-config.yaml.
type Config struct {
	Location          string           `yaml:"location"`
	KubernetesVersion string           `yaml:"kubernetes_version"`
	MastersPool       MastersPool      `yaml:"masters_pool"`
	WorkerNodePools   []WorkerNodePool `yaml:"worker_node_pools"`
	SSH               *SSHConfig       `yaml:"ssh,omitempty"`
	Network           *NetworkConfig   `yaml:"network,omitempty"`
}

// NetworkConfig controls firewall rules for SSH and Kubernetes API access.
// Defaults to 0.0.0.0/0 (open to all) if not specified - restrict these
// in production to your IP ranges.
type NetworkConfig struct {
	SSHAllowedCIDRs []string `yaml:"ssh_allowed_cidrs,omitempty"`
	APIAllowedCIDRs []string `yaml:"api_allowed_cidrs,omitempty"`
}

// MastersPool defines the control plane node pool.
type MastersPool struct {
	InstanceType  string `yaml:"instance_type"`
	InstanceCount int    `yaml:"instance_count"`
}

// WorkerNodePool defines a worker node pool with optional autoscaling.
type WorkerNodePool struct {
	Name          string       `yaml:"name"`
	InstanceType  string       `yaml:"instance_type"`
	InstanceCount int          `yaml:"instance_count"`
	Location      string       `yaml:"location,omitempty"`
	Autoscaling   *Autoscaling `yaml:"autoscaling,omitempty"`
}

// Autoscaling configures automatic node pool scaling.
type Autoscaling struct {
	Enabled      bool `yaml:"enabled"`
	MinInstances int  `yaml:"min_instances"`
	MaxInstances int  `yaml:"max_instances"`
}

// SSHConfig allows users to provide their own SSH keys instead of auto-generated ones.
type SSHConfig struct {
	PublicKeyPath  string `yaml:"public_key_path"`
	PrivateKeyPath string `yaml:"private_key_path"`
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

// Validate checks that all required fields are present and valid.
func (c *Config) Validate() error {
	if c.Location == "" {
		return fmt.Errorf("hetzner_cloud.location is required")
	}
	if c.KubernetesVersion == "" {
		return fmt.Errorf("hetzner_cloud.kubernetes_version is required")
	}
	if c.MastersPool.InstanceType == "" {
		return fmt.Errorf("hetzner_cloud.masters_pool.instance_type is required")
	}
	if c.MastersPool.InstanceCount < 1 {
		return fmt.Errorf("hetzner_cloud.masters_pool.instance_count must be at least 1")
	}
	if c.MastersPool.InstanceCount > 1 && c.MastersPool.InstanceCount%2 == 0 {
		return fmt.Errorf("hetzner_cloud.masters_pool.instance_count should be odd (1, 3, 5) for k3s HA with embedded etcd; got %d", c.MastersPool.InstanceCount)
	}
	if len(c.WorkerNodePools) == 0 {
		return fmt.Errorf("hetzner_cloud.worker_node_pools must have at least one pool")
	}
	for i, pool := range c.WorkerNodePools {
		if pool.Name == "" {
			return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].name is required", i)
		}
		if pool.InstanceType == "" {
			return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].instance_type is required", i)
		}
		if pool.InstanceCount < 1 && (pool.Autoscaling == nil || !pool.Autoscaling.Enabled) {
			return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].instance_count must be at least 1 (or enable autoscaling)", i)
		}
		if pool.Autoscaling != nil && pool.Autoscaling.Enabled {
			if pool.Autoscaling.MinInstances < 0 {
				return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].autoscaling.min_instances must not be negative", i)
			}
			if pool.Autoscaling.MaxInstances < 1 {
				return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].autoscaling.max_instances must be at least 1", i)
			}
			if pool.Autoscaling.MinInstances > pool.Autoscaling.MaxInstances {
				return fmt.Errorf("hetzner_cloud.worker_node_pools[%d].autoscaling.min_instances (%d) must not exceed max_instances (%d)",
					i, pool.Autoscaling.MinInstances, pool.Autoscaling.MaxInstances)
			}
		}
	}
	return nil
}
