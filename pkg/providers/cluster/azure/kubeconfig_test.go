package azure

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

type fakeManagedClusters struct {
	credentials string
}

func (f *fakeManagedClusters) ListClusterAdminCredentials(_ context.Context, _, _ string, _ *armcontainerservice.ManagedClustersClientListClusterAdminCredentialsOptions) (armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse, error) {
	return armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse{
		CredentialResults: armcontainerservice.CredentialResults{
			Kubeconfigs: []*armcontainerservice.CredentialResult{
				{Name: to.Ptr("clusterAdmin"), Value: []byte(f.credentials)},
			},
		},
	}, nil
}

func TestFetchAdminKubeconfig(t *testing.T) {
	api := &fakeManagedClusters{credentials: "apiVersion: v1\nkind: Config\n# fake"}
	got, err := fetchAdminKubeconfig(context.Background(), api, "rg-1", "my-cluster")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == "" {
		t.Error("empty kubeconfig")
	}
	if !contains(string(got), "kind: Config") {
		t.Errorf("unexpected kubeconfig payload: %s", got)
	}
}

func TestResolveResourceGroup(t *testing.T) {
	t.Run("explicit name wins", func(t *testing.T) {
		cfg := &Config{ResourceGroupName: "my-rg"}
		if got := resolveResourceGroup(cfg, "p"); got != "my-rg" {
			t.Errorf("got %q, want my-rg", got)
		}
	})
	t.Run("fallback to project_name-rg", func(t *testing.T) {
		cfg := &Config{}
		if got := resolveResourceGroup(cfg, "myproj"); got != "myproj-rg" {
			t.Errorf("got %q, want myproj-rg", got)
		}
	})
}
