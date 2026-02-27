package hetzner

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid minimal config",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool: MastersPool{
					InstanceType:  "cpx21",
					InstanceCount: 1,
				},
				WorkerNodePools: []WorkerNodePool{
					{Name: "workers", InstanceType: "cpx31", InstanceCount: 2},
				},
			},
		},
		{
			name: "missing location",
			cfg: Config{
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
			wantErr: true,
			errMsg:  "location",
		},
		{
			name: "missing kubernetes_version",
			cfg: Config{
				Location:        "ash",
				MastersPool:     MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools: []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
			wantErr: true,
			errMsg:  "kubernetes_version",
		},
		{
			name: "zero masters",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 0},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
			wantErr: true,
			errMsg:  "instance_count",
		},
		{
			name: "no worker pools",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools:   []WorkerNodePool{},
			},
			wantErr: true,
			errMsg:  "worker_node_pools",
		},
		{
			name: "explicit k3s version passes through",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "v1.32.0+k3s1",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
		},
		{
			name: "worker with autoscaling and zero count is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools: []WorkerNodePool{{
					Name: "w", InstanceType: "cpx31", InstanceCount: 0,
					Autoscaling: &Autoscaling{Enabled: true, MinInstances: 1, MaxInstances: 5},
				}},
			},
		},
		{
			name: "even master count rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 2},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
			wantErr: true,
			errMsg:  "odd",
		},
		{
			name: "3 masters is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 3},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
			},
		},
		{
			name: "autoscaling min exceeds max",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools: []WorkerNodePool{{
					Name: "w", InstanceType: "cpx31", InstanceCount: 0,
					Autoscaling: &Autoscaling{Enabled: true, MinInstances: 10, MaxInstances: 5},
				}},
			},
			wantErr: true,
			errMsg:  "min_instances",
		},
		{
			name: "autoscaling max zero",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools: []WorkerNodePool{{
					Name: "w", InstanceType: "cpx31", InstanceCount: 0,
					Autoscaling: &Autoscaling{Enabled: true, MinInstances: 0, MaxInstances: 0},
				}},
			},
			wantErr: true,
			errMsg:  "max_instances",
		},
		{
			name: "custom network config is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				MastersPool:       MastersPool{InstanceType: "cpx21", InstanceCount: 1},
				WorkerNodePools:   []WorkerNodePool{{Name: "w", InstanceType: "cpx31", InstanceCount: 1}},
				Network: &NetworkConfig{
					SSHAllowedCIDRs: []string{"10.0.0.0/8"},
					APIAllowedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error should contain %q, got: %v", tt.errMsg, err)
				}
			}
		})
	}
}

func TestAllowedNetworksDefaults(t *testing.T) {
	t.Run("defaults to 0.0.0.0/0 when no network config", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.SSHAllowedNetworks(); len(got) != 1 || got[0] != "0.0.0.0/0" {
			t.Errorf("SSHAllowedNetworks() = %v, want [0.0.0.0/0]", got)
		}
		if got := cfg.APIAllowedNetworks(); len(got) != 1 || got[0] != "0.0.0.0/0" {
			t.Errorf("APIAllowedNetworks() = %v, want [0.0.0.0/0]", got)
		}
	})

	t.Run("uses configured CIDRs when present", func(t *testing.T) {
		cfg := &Config{
			Network: &NetworkConfig{
				SSHAllowedCIDRs: []string{"10.0.0.0/8"},
				APIAllowedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
			},
		}
		if got := cfg.SSHAllowedNetworks(); len(got) != 1 || got[0] != "10.0.0.0/8" {
			t.Errorf("SSHAllowedNetworks() = %v, want [10.0.0.0/8]", got)
		}
		if got := cfg.APIAllowedNetworks(); len(got) != 2 {
			t.Errorf("APIAllowedNetworks() = %v, want 2 entries", got)
		}
	})

	t.Run("falls back when network config has empty lists", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{}}
		if got := cfg.SSHAllowedNetworks(); len(got) != 1 || got[0] != "0.0.0.0/0" {
			t.Errorf("SSHAllowedNetworks() = %v, want [0.0.0.0/0]", got)
		}
	})
}

func TestIsExplicitK3sVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"1.32", false},
		{"1.32.0", false},
		{"v1.32.0+k3s1", true},
		{"v1.32.12+k3s3", true},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			cfg := &Config{KubernetesVersion: tt.version}
			if got := cfg.IsExplicitK3sVersion(); got != tt.want {
				t.Errorf("IsExplicitK3sVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
