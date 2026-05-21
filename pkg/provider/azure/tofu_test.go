package azure

import (
	"encoding/json"
	"testing"
)

func TestToTFVarsSystemPoolResolution(t *testing.T) {
	cfg := Config{
		Region: "eastus",
		NodeGroups: map[string]NodeGroup{
			"sys":  {Instance: "Standard_D2_v3", MinNodes: 1, MaxNodes: 1, Mode: "System"},
			"user": {Instance: "Standard_D4_v3", MinNodes: 0, MaxNodes: 5},
		},
	}
	vars := cfg.toTFVars("myproj")

	if got := vars.NodeGroups["sys"].Mode; got != "System" {
		t.Errorf("sys.mode = %q, want System", got)
	}
	if got := vars.NodeGroups["user"].Mode; got != "User" {
		t.Errorf("user.mode defaulted = %q, want User", got)
	}
}

func TestToTFVarsCreateFlags(t *testing.T) {
	t.Run("create RG by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
		vars := cfg.toTFVars("p")
		if !vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should default to true")
		}
	})
	t.Run("BYO RG via explicit name", func(t *testing.T) {
		cfg := Config{
			Region:            "eastus",
			ResourceGroupName: "my-rg",
			NodeGroups:        map[string]NodeGroup{"s": {Mode: "System"}},
		}
		vars := cfg.toTFVars("p")
		if vars.CreateResourceGroup {
			t.Error("CreateResourceGroup should be false when ResourceGroupName is set")
		}
		if vars.ExistingResourceGroupName == nil || *vars.ExistingResourceGroupName != "my-rg" {
			t.Errorf("ExistingResourceGroupName not propagated")
		}
	})
	t.Run("create VNet by default", func(t *testing.T) {
		cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
		vars := cfg.toTFVars("p")
		if !vars.CreateVNet {
			t.Error("CreateVNet should default to true")
		}
	})
	t.Run("BYO VNet flips flag", func(t *testing.T) {
		cfg := Config{
			Region:     "eastus",
			NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}},
			Network:    &NetworkConfig{ExistingVNetID: "/subs/.../vn1", ExistingNodeSubnetID: "/subs/.../sub1"},
		}
		vars := cfg.toTFVars("p")
		if vars.CreateVNet {
			t.Error("CreateVNet should be false when ExistingVNetID is set")
		}
	})
}

func TestToTFVarsNICTagsInjected(t *testing.T) {
	cfg := Config{
		Region:     "eastus",
		NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}},
		Tags:       map[string]string{"Env": "dev"},
	}
	vars := cfg.toTFVars("nebari-x")

	if got := vars.Tags["nic.nebari.dev/cluster-name"]; got != "nebari-x" {
		t.Errorf("cluster-name tag = %q, want nebari-x", got)
	}
	if got := vars.Tags["nic.nebari.dev/managed-by"]; got != "nic" {
		t.Errorf("managed-by tag = %q, want nic", got)
	}
	if got := vars.Tags["Env"]; got != "dev" {
		t.Errorf("user tag dropped: %q", got)
	}
}

func TestToTFVarsOmitsEmptyPointers(t *testing.T) {
	cfg := Config{Region: "eastus", NodeGroups: map[string]NodeGroup{"s": {Mode: "System"}}}
	vars := cfg.toTFVars("p")

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

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
