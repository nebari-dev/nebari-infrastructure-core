package azure

import (
	"context"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v6"
)

// fetchAdminKubeconfig pulls the admin kubeconfig via the AKS data plane API.
// Inner function: takes a managedClustersAPI interface so tests can fake it.
func fetchAdminKubeconfig(ctx context.Context, api managedClustersAPI, resourceGroup, clusterName string) ([]byte, error) {
	resp, err := api.ListClusterAdminCredentials(ctx, resourceGroup, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("list admin credentials: %w", err)
	}
	for _, kc := range resp.Kubeconfigs {
		if kc != nil && len(kc.Value) > 0 {
			return kc.Value, nil
		}
	}
	return nil, fmt.Errorf("no kubeconfig returned for cluster %q", clusterName)
}

// newManagedClustersClient wires the real SDK client. Production path.
func newManagedClustersClient(subscriptionID string) (managedClustersAPI, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure auth: %w", err)
	}
	factory, err := armcontainerservice.NewClientFactory(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("armcontainerservice factory: %w", err)
	}
	return factory.NewManagedClustersClient(), nil
}

// resolveResourceGroup returns either the user-supplied RG or the convention
// "<project_name>-rg" used by the Terraform module when create_resource_group=true.
func resolveResourceGroup(cfg *Config, projectName string) string {
	if cfg.ResourceGroupName != "" {
		return cfg.ResourceGroupName
	}
	return projectName + "-rg"
}

// resolveClusterName mirrors the Terraform module's "${var.project_name}-aks".
func resolveClusterName(projectName string) string {
	return projectName + "-aks"
}

// fetchKubeconfigForCluster is the high-level wrapper called by Provider.GetKubeconfig.
func fetchKubeconfigForCluster(ctx context.Context, cfg *Config, projectName string) ([]byte, error) {
	subID := os.Getenv(subscriptionIDEnv)
	if subID == "" {
		return nil, fmt.Errorf("%s environment variable is required", subscriptionIDEnv)
	}
	api, err := newManagedClustersClient(subID)
	if err != nil {
		return nil, err
	}
	return fetchAdminKubeconfig(ctx, api, resolveResourceGroup(cfg, projectName), resolveClusterName(projectName))
}
