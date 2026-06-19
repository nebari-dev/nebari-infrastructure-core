package azure

import (
	"testing"
)

func TestClassifyMCResourceGroups(t *testing.T) {
	ids := []string{
		"/subscriptions/x/resourceGroups/my-cluster-rg",
		"/subscriptions/x/resourceGroups/MC_my-cluster-rg_my-cluster-aks_eastus",
		"/subscriptions/x/resourceGroups/MC_my-cluster-rg_my-cluster-aks_eastus/providers/Microsoft.Network/loadBalancers/foo",
	}
	mc, others := classifyMC(ids)
	if len(mc) != 2 {
		t.Errorf("expected 2 MC items (RG + resource inside), got %d: %v", len(mc), mc)
	}
	if len(others) != 1 {
		t.Errorf("expected 1 non-MC item, got %d: %v", len(others), others)
	}
}
