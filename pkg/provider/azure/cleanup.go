package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// classifyMC splits resource IDs into those inside an AKS-managed MC_* group
// and everything else. MC_ items are deleted last because their parent
// resource group will cascade them.
func classifyMC(ids []string) (mc, others []string) {
	for _, id := range ids {
		if strings.Contains(id, "/resourceGroups/MC_") {
			mc = append(mc, id)
		} else {
			others = append(others, id)
		}
	}
	return mc, others
}

// cleanupOrphans is invoked by Destroy after tofu destroy completes. It
// enumerates resources tagged for this cluster, identifies anything still
// alive (which means tofu either missed it or it's an AKS-managed sibling),
// and reports them. Auto-delete is deferred to a follow-up; for MVP, we
// report orphans so users can clean up manually without surprise bills.
func cleanupOrphans(ctx context.Context, subscriptionID, projectName string) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("azure auth: %w", err)
	}
	factory, err := armresources.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("armresources client: %w", err)
	}
	return cleanupOrphansWithClient(ctx, factory.NewClient(), projectName)
}

// cleanupOrphansWithClient is the unit-testable inner that takes the
// resourcesAPI seam.
func cleanupOrphansWithClient(ctx context.Context, client resourcesAPI, projectName string) error {
	ids, err := listTaggedResources(ctx, client, projectName)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil // happy path: tofu destroy was thorough.
	}
	mc, others := classifyMC(ids)
	return fmt.Errorf("found %d orphaned resources after destroy (MC: %d, other: %d); run `az resource delete --ids %s` to clean up",
		len(ids), len(mc), len(others), strings.Join(ids, " "))
}
