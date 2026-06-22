package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// managedClustersAPI is the subset of armcontainerservice.ManagedClustersClient
// that the Azure provider uses. Tests inject a fake; production wires the real
// client in NewProvider.
type managedClustersAPI interface {
	ListClusterAdminCredentials(
		ctx context.Context,
		resourceGroupName, resourceName string,
		options *armcontainerservice.ManagedClustersClientListClusterAdminCredentialsOptions,
	) (armcontainerservice.ManagedClustersClientListClusterAdminCredentialsResponse, error)
}

// resourcesAPI is the subset of armresources.Client used by state.go / cleanup.go.
type resourcesAPI interface {
	NewListPager(
		options *armresources.ClientListOptions,
	) *runtime.Pager[armresources.ClientListResponse]
}
