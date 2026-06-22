package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// buildTagFilter returns the Azure Resource Graph `$filter` expression that
// matches every resource NIC tagged for this cluster.
func buildTagFilter(projectName string) string {
	return fmt.Sprintf(
		"tagName eq '%s' and tagValue eq '%s'",
		tagClusterName, projectName,
	)
}

// listTaggedResources enumerates resources matching the NIC cluster tags.
// Returns IDs suitable for cleanup.
func listTaggedResources(ctx context.Context, client resourcesAPI, projectName string) ([]string, error) {
	pager := client.NewListPager(&armresources.ClientListOptions{
		Filter: to.Ptr(buildTagFilter(projectName)),
	})

	var ids []string
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tagged resources: %w", err)
		}
		for _, r := range page.Value {
			if r.ID != nil {
				ids = append(ids, *r.ID)
			}
		}
	}
	return ids, nil
}
