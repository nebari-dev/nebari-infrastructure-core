package azure

import (
	"encoding/json"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestToTFVarsSystemPoolResolution(t *testing.T) {
	cfg := Config{
		Region: "eastus",
		NodeGroups: map[string]NodeGroup{
			"sys":  {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: modeSystem},
			"user": {Instance: "Standard_D4_v3", MinNodes: 0, MaxNodes: 5},
		},
	}
	vars := cfg.toTFVars("myproj", nil)

	if got := vars.NodeGroups["sys"].Mode; got != modeSystem {
		t.Errorf("sys.mode = %q, want %s", got, modeSystem)
	}
	if got := vars.NodeGroups["user"].Mode; got != "User" {
		t.Errorf("user.mode defaulted = %q, want User", got)
	}
}

func TestToTFVarsCreateFlags(t *testing.T) {
	t.Run("create RG by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}}}
		vars := cfg.toTFVars("p", nil)
		if !vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should default to true")
		}
	})
	t.Run("BYO RG via explicit name", func(t *testing.T) {
		cfg := Config{
			Region:            "eastus",
			ResourceGroupName: "my-rg",
			NodeGroups:        map[string]NodeGroup{"s": {Mode: modeSystem}},
		}
		vars := cfg.toTFVars("p", nil)
		if vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should be false when ResourceGroupName is set")
		}
		if vars.ExistingResourceGroupName == nil || *vars.ExistingResourceGroupName != "my-rg" {
			t.Errorf("ExistingResourceGroupName not propagated")
		}
	})
	t.Run("create VNet by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}}}
		vars := cfg.toTFVars("p", nil)
		if !vars.CreateVNet {
			t.Error("CreateVNet should default to true")
		}
	})
	t.Run("BYO VNet flips flag", func(t *testing.T) {
		cfg := Config{
			Region:     "eastus",
			NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}},
			Network:    &NetworkConfig{ExistingVNetID: "/subs/.../vn1", ExistingNodeSubnetID: "/subs/.../sub1"},
		}
		vars := cfg.toTFVars("p", nil)
		if vars.CreateVNet {
			t.Error("CreateVNet should be false when ExistingVNetID is set")
		}
	})
}

func TestToTFVarsNICTagsInjected(t *testing.T) {
	cfg := Config{
		Region:     "eastus",
		NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}},
		Tags:       map[string]string{"Env": "dev"},
	}
	vars := cfg.toTFVars("nebari-x", nil)

	if got := vars.Tags["nic.nebari.dev_cluster-name"]; got != "nebari-x" {
		t.Errorf("cluster-name tag = %q, want nebari-x", got)
	}
	if got := vars.Tags["nic.nebari.dev_managed-by"]; got != "nic" {
		t.Errorf("managed-by tag = %q, want nic", got)
	}
	if got := vars.Tags["Env"]; got != "dev" {
		t.Errorf("user tag dropped: %q", got)
	}
}

func TestToTFVarsDataPlane(t *testing.T) {
	t.Run("propagates cilium dataplane", func(t *testing.T) {
		cfg := Config{
			Region:     "eastus",
			NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}},
			Network:    &NetworkConfig{DataPlane: dataPlaneCilium},
		}
		vars := cfg.toTFVars("p", nil)
		if vars.NetworkDataPlane != dataPlaneCilium {
			t.Errorf("NetworkDataPlane = %q, want %q", vars.NetworkDataPlane, dataPlaneCilium)
		}
	})
	t.Run("omits dataplane when unset so the module default wins", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}}}
		b, err := json.Marshal(cfg.toTFVars("p", nil))
		if err != nil {
			t.Fatal(err)
		}
		if s := string(b); contains(s, "network_data_plane") {
			t.Errorf("expected network_data_plane to be omitted from JSON, got: %s", s)
		}
	})
}

func TestToTFVarsOmitsEmptyPointers(t *testing.T) {
	cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: modeSystem}}}
	vars := cfg.toTFVars("p", nil)

	b, err := json.Marshal(vars)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{
		"existing_resource_group_name",
		"existing_vnet_id",
		"existing_node_subnet_id",
		"kubernetes_version",
	} {
		if contains(s, key) {
			t.Errorf("expected %q to be omitted from JSON, got: %s", key, s)
		}
	}
}

func TestToTFVarsBackupContainer(t *testing.T) {
	c := &Config{Region: "eastus", NodeGroups: map[string]NodeGroup{}}

	t.Run("no spec disables container", func(t *testing.T) {
		v := c.toTFVars("proj", nil)
		if v.BackupContainerCreate {
			t.Fatal("expected BackupContainerCreate false")
		}
	})
	t.Run("spec enables container", func(t *testing.T) {
		v := c.toTFVars("proj", &cluster.BackupBucketSpec{Name: "c", StorageAccount: "sa", Create: true})
		if !v.BackupContainerCreate || v.BackupContainerName != "c" || v.BackupStorageAccount != "sa" {
			t.Fatalf("bad tfvars: %+v", v)
		}
	})
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
