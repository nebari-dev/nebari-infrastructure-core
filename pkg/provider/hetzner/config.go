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
	}
	return nil
}
