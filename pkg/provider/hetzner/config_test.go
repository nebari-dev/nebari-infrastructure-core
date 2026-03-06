package hetzner

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

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
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 2},
				},
			},
		},
		{
			name: "missing location",
			cfg: Config{
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "location",
		},
		{
			name: "missing kubernetes_version",
			cfg: Config{
				Location: "ash",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "kubernetes_version",
		},
		{
			name: "zero master count",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 0, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "count",
		},
		{
			name: "no worker pools without schedule on masters",
			cfg: Config{
				Location:                   "ash",
				KubernetesVersion:          "1.32",
				ScheduleWorkloadsOnMasters: boolPtr(false),
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
				},
			},
			wantErr: true,
			errMsg:  "non-master group",
		},
		{
			name: "no worker pools with schedule on masters is valid",
			cfg: Config{
				Location:                   "ash",
				KubernetesVersion:          "1.32",
				ScheduleWorkloadsOnMasters: boolPtr(true),
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
				},
			},
		},
		{
			name: "single node cluster defaults to schedule on masters",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
				},
			},
		},
		{
			name: "explicit k3s version passes through",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "v1.32.0+k3s1",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
		},
		{
			name: "worker with autoscaling and zero count is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 0,
						Autoscaling: &Autoscaling{Enabled: true, MinInstances: 1, MaxInstances: 5}},
				},
			},
		},
		{
			name: "even master count rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 2, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "odd",
		},
		{
			name: "3 masters is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 3, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
		},
		{
			name: "no master group",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "master: true",
		},
		{
			name: "multiple master groups rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master1": {InstanceType: "cpx21", Count: 1, Master: true},
					"master2": {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "only one",
		},
		{
			name: "autoscaling min exceeds max",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 0,
						Autoscaling: &Autoscaling{Enabled: true, MinInstances: 10, MaxInstances: 5}},
				},
			},
			wantErr: true,
			errMsg:  "min_instances",
		},
		{
			name: "autoscaling max zero",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 0,
						Autoscaling: &Autoscaling{Enabled: true, MinInstances: 0, MaxInstances: 0}},
				},
			},
			wantErr: true,
			errMsg:  "max_instances",
		},
		{
			name: "master autoscaling rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master": {InstanceType: "cpx21", Count: 1, Master: true,
						Autoscaling: &Autoscaling{Enabled: true, MinInstances: 1, MaxInstances: 3}},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "autoscaling",
		},
		{
			name: "master location override rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true, Location: "fsn1"},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "location",
		},
		{
			name: "custom network config is valid",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
				Network: &NetworkConfig{
					SSHAllowedCIDRs: []string{"10.0.0.0/8"},
					APIAllowedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
				},
			},
		},
		{
			name: "location with invalid characters rejected",
			cfg: Config{
				Location:          "ash\"; malicious",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name: "node group name with invalid characters rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":               {InstanceType: "cpx21", Count: 1, Master: true},
					"workers\"\nmalicious": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name: "instance type with spaces rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx 21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "invalid characters",
		},
		{
			name: "kubernetes_version with injection rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32\"\nmalicious: true",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name: "kubernetes_version with arbitrary text rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "latest",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
			},
			wantErr: true,
			errMsg:  "invalid",
		},
		{
			name: "invalid SSH CIDR rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
				Network: &NetworkConfig{
					SSHAllowedCIDRs: []string{"not-a-cidr"},
				},
			},
			wantErr: true,
			errMsg:  "invalid CIDR",
		},
		{
			name: "invalid API CIDR rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":  {InstanceType: "cpx21", Count: 1, Master: true},
					"workers": {InstanceType: "cpx31", Count: 1},
				},
				Network: &NetworkConfig{
					SSHAllowedCIDRs: []string{"10.0.0.0/8"},
					APIAllowedCIDRs: []string{"192.168.0.0/16", "bad"},
				},
			},
			wantErr: true,
			errMsg:  "invalid CIDR",
		},
		{
			name: "identifiers with dots and hyphens are valid",
			cfg: Config{
				Location:          "eu-central",
				KubernetesVersion: "1.32",
				NodeGroups: map[string]NodeGroup{
					"master":         {InstanceType: "cx22.metal", Count: 1, Master: true},
					"gpu-workers_v2": {InstanceType: "cpx31", Count: 1},
				},
			},
		},
		{
			name: "empty node_groups rejected",
			cfg: Config{
				Location:          "ash",
				KubernetesVersion: "1.32",
				NodeGroups:        map[string]NodeGroup{},
			},
			wantErr: true,
			errMsg:  "at least one",
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

func TestNetworkWarnings(t *testing.T) {
	tests := []struct {
		name         string
		cfg          Config
		wantWarnings int
	}{
		{
			name:         "no network config produces 2 warnings",
			cfg:          Config{},
			wantWarnings: 2,
		},
		{
			name:         "empty network config produces 2 warnings",
			cfg:          Config{Network: &NetworkConfig{}},
			wantWarnings: 2,
		},
		{
			name: "configured CIDRs produce no warnings",
			cfg: Config{Network: &NetworkConfig{
				SSHAllowedCIDRs: []string{"10.0.0.0/8"},
				APIAllowedCIDRs: []string{"10.0.0.0/8"},
			}},
			wantWarnings: 0,
		},
		{
			name: "only SSH configured produces 1 warning",
			cfg: Config{Network: &NetworkConfig{
				SSHAllowedCIDRs: []string{"10.0.0.0/8"},
			}},
			wantWarnings: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.cfg.NetworkWarnings()
			if len(warnings) != tt.wantWarnings {
				t.Errorf("NetworkWarnings() returned %d warnings, want %d: %v", len(warnings), tt.wantWarnings, warnings)
			}
		})
	}
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

func TestScheduleOnMasters(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "nil defaults to true",
			cfg:  Config{},
			want: true,
		},
		{
			name: "explicit true",
			cfg:  Config{ScheduleWorkloadsOnMasters: boolPtr(true)},
			want: true,
		},
		{
			name: "explicit false",
			cfg:  Config{ScheduleWorkloadsOnMasters: boolPtr(false)},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ScheduleOnMasters(); got != tt.want {
				t.Errorf("ScheduleOnMasters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMasterGroup(t *testing.T) {
	cfg := &Config{
		NodeGroups: map[string]NodeGroup{
			"ctrl":    {InstanceType: "cpx21", Count: 1, Master: true},
			"workers": {InstanceType: "cpx31", Count: 2},
		},
	}
	name, mg := cfg.MasterGroup()
	if name != "ctrl" {
		t.Errorf("MasterGroup() name = %q, want %q", name, "ctrl")
	}
	if mg.InstanceType != "cpx21" {
		t.Errorf("MasterGroup() instance_type = %q, want %q", mg.InstanceType, "cpx21")
	}
}

func TestWorkerGroups(t *testing.T) {
	cfg := &Config{
		NodeGroups: map[string]NodeGroup{
			"master": {InstanceType: "cpx21", Count: 1, Master: true},
			"b":      {InstanceType: "cpx31", Count: 1},
			"a":      {InstanceType: "cpx41", Count: 2},
		},
	}
	workers := cfg.WorkerGroups()
	if len(workers) != 2 {
		t.Fatalf("WorkerGroups() returned %d entries, want 2", len(workers))
	}
	if workers[0].Name != "a" || workers[1].Name != "b" {
		t.Errorf("WorkerGroups() not sorted: got [%s, %s], want [a, b]", workers[0].Name, workers[1].Name)
	}
}
