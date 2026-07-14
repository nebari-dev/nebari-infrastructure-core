package azure

import (
	"strings"
	"testing"
)

func ptrBool(b bool) *bool { return &b }

func TestConfigValidate(t *testing.T) {
	validNodeGroup := NodeGroup{Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 3, Mode: modeSystem}

	cases := []struct {
		name      string
		cfg       Config
		wantErr   bool
		wantInErr string
	}{
		{
			name: "minimal valid",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
			},
			wantErr: false,
		},
		{
			name:      "missing region",
			cfg:       Config{NodeGroups: map[string]NodeGroup{"system": validNodeGroup}},
			wantErr:   true,
			wantInErr: "region",
		},
		{
			name:      "no node groups",
			cfg:       Config{Region: "eastus"},
			wantErr:   true,
			wantInErr: "node_groups",
		},
		{
			name: "two system pools",
			cfg: Config{
				Region: "eastus",
				NodeGroups: map[string]NodeGroup{
					"a": {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: modeSystem},
					"b": {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: modeSystem},
				},
			},
			wantErr:   true,
			wantInErr: modeSystem,
		},
		{
			name: "BYO vnet missing subnet",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{ExistingVNetID: "/subscriptions/.../vn1"},
			},
			wantErr:   true,
			wantInErr: "existing_node_subnet_id",
		},
		{
			name: "BYO subnet missing vnet",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{ExistingNodeSubnetID: "/subscriptions/.../sub1"},
			},
			wantErr:   true,
			wantInErr: "existing_vnet_id",
		},
		{
			name: "bad CIDR",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{VNetCIDRBlock: "not-a-cidr"},
			},
			wantErr:   true,
			wantInErr: "vnet_cidr_block",
		},
		{
			name: "create_resource_group false without existing name",
			cfg: Config{
				Region:              "eastus",
				NodeGroups:          map[string]NodeGroup{"system": validNodeGroup},
				CreateResourceGroup: ptrBool(false),
			},
			wantErr:   true,
			wantInErr: "resource_group_name",
		},
		{
			name: "bad kubernetes version",
			cfg: Config{
				Region:            "eastus",
				NodeGroups:        map[string]NodeGroup{"system": validNodeGroup},
				KubernetesVersion: "latest",
			},
			wantErr:   true,
			wantInErr: "kubernetes_version",
		},
		{
			name: "valid kubernetes version",
			cfg: Config{
				Region:            "eastus",
				NodeGroups:        map[string]NodeGroup{"system": validNodeGroup},
				KubernetesVersion: "1.34",
			},
			wantErr: false,
		},
		{
			name: "valid cilium dataplane",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{DataPlane: dataPlaneCilium},
			},
			wantErr: false,
		},
		{
			name: "invalid dataplane",
			cfg: Config{
				Region:     "eastus",
				NodeGroups: map[string]NodeGroup{"system": validNodeGroup},
				Network:    &NetworkConfig{DataPlane: "calico"},
			},
			wantErr:   true,
			wantInErr: "dataplane",
		},
		{
			name: "invalid node_provisioning_mode",
			cfg: Config{
				Region:               "eastus",
				NodeGroups:           map[string]NodeGroup{"system": validNodeGroup},
				NodeProvisioningMode: "auto",
			},
			wantErr:   true,
			wantInErr: "node_provisioning_mode",
		},
		{
			name: "NAP Auto without cilium",
			cfg: Config{
				Region:               "eastus",
				NodeGroups:           map[string]NodeGroup{"system": validNodeGroup},
				NodeProvisioningMode: napModeAuto,
			},
			wantErr:   true,
			wantInErr: "cilium",
		},
		{
			name: "NAP Auto with cilium",
			cfg: Config{
				Region:               "eastus",
				NodeGroups:           map[string]NodeGroup{"system": validNodeGroup},
				NodeProvisioningMode: napModeAuto,
				Network:              &NetworkConfig{DataPlane: dataPlaneCilium},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantInErr)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if tc.wantErr && tc.wantInErr != "" && !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("error %q does not contain expected substring %q", err.Error(), tc.wantInErr)
			}
		})
	}
}
