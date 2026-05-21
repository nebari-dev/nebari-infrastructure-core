package azure

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

type fakeVersionsAPI struct {
	versions []string
}

func (f *fakeVersionsAPI) ListKubernetesVersions(_ context.Context, _ string, _ *armcontainerservice.ManagedClustersClientListKubernetesVersionsOptions) (armcontainerservice.ManagedClustersClientListKubernetesVersionsResponse, error) {
	patches := make([]*armcontainerservice.KubernetesVersion, 0, len(f.versions))
	for _, v := range f.versions {
		patches = append(patches, &armcontainerservice.KubernetesVersion{Version: to.Ptr(v)})
	}
	return armcontainerservice.ManagedClustersClientListKubernetesVersionsResponse{
		KubernetesVersionListResult: armcontainerservice.KubernetesVersionListResult{Values: patches},
	}, nil
}

func TestListSupportedVersions(t *testing.T) {
	api := &fakeVersionsAPI{versions: []string{"1.32", "1.33", "1.34"}}
	got, err := listSupportedVersions(context.Background(), api, "eastus")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
	if got[0] != "1.32" {
		t.Errorf("first = %q, want 1.32", got[0])
	}
}
