package azure

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// State backend constants. The resource group and container names are fixed
// across all NIC-managed clusters in a subscription; the storage account name
// is derived from the subscription ID to keep it globally unique without
// exposing the raw subscription ID.
const (
	stateResourceGroupName  = "nic-tfstate-rg"
	stateContainerName      = "tfstate"
	stateStorageAccountStub = "nictfstate"
	// Azure storage account name rules: 3-24 chars, lowercase alphanumeric.
	stateStorageAccountHashLen = 14
)

// stateBackendConfig captures the four values that must be passed to
// `tofu init` as -backend-config flags so the azurerm backend can locate
// the remote state blob for this cluster.
type stateBackendConfig struct {
	RGName    string
	SAName    string
	Container string
	Key       string
}

// stateStorageAccountName derives a deterministic, globally-unique storage
// account name from the Azure subscription ID. Storage account names must be
// 3-24 lowercase alphanumeric characters; we use a fixed "nictfstate" prefix
// plus the first 14 hex chars of SHA-256(subscriptionID), for a total of
// 24 characters.
func stateStorageAccountName(subscriptionID string) string {
	hash := sha256.Sum256([]byte(subscriptionID))
	return fmt.Sprintf("%s%x", stateStorageAccountStub, hash[:stateStorageAccountHashLen/2])
}

// newStateBackendConfig builds the deterministic backend config struct (no
// cloud calls). Use ensureStateBackend to actually create the resources.
func newStateBackendConfig(subscriptionID, projectName string) stateBackendConfig {
	return stateBackendConfig{
		RGName:    stateResourceGroupName,
		SAName:    stateStorageAccountName(subscriptionID),
		Container: stateContainerName,
		Key:       fmt.Sprintf("%s.tfstate", projectName),
	}
}

// ensureStateBackend idempotently creates the resource group, storage account,
// and blob container that back the azurerm Terraform state for this cluster.
// Returns the resolved backend config to pass to `tf.Init` via
// tfexec.BackendConfig flags.
//
// All Azure SDK calls go through the default credential chain (azidentity).
// The caller is responsible for ensuring AZURE_SUBSCRIPTION_ID (and friends)
// are set in the environment.
func ensureStateBackend(ctx context.Context, subscriptionID, location, projectName string) (stateBackendConfig, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.EnsureStateBackend")
	defer span.End()

	cfg := newStateBackendConfig(subscriptionID, projectName)
	span.SetAttributes(
		attribute.String("state.resource_group", cfg.RGName),
		attribute.String("state.storage_account", cfg.SAName),
		attribute.String("state.container", cfg.Container),
		attribute.String("state.key", cfg.Key),
	)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		span.RecordError(err)
		return stateBackendConfig{}, fmt.Errorf("azure credentials: %w", err)
	}

	if err := ensureStateResourceGroup(ctx, subscriptionID, location, cfg.RGName, cred); err != nil {
		span.RecordError(err)
		return stateBackendConfig{}, err
	}
	if err := ensureStateStorageAccount(ctx, subscriptionID, location, cfg.RGName, cfg.SAName, cred); err != nil {
		span.RecordError(err)
		return stateBackendConfig{}, err
	}
	if err := ensureStateBlobVersioning(ctx, subscriptionID, cfg.RGName, cfg.SAName, cred); err != nil {
		span.RecordError(err)
		return stateBackendConfig{}, err
	}
	if err := ensureStateContainer(ctx, subscriptionID, cfg.RGName, cfg.SAName, cfg.Container, cred); err != nil {
		span.RecordError(err)
		return stateBackendConfig{}, err
	}

	return cfg, nil
}

// stateBackendExists reports whether the Terraform state storage account for
// this subscription already exists. It's used by dry-run to decide between the
// real azurerm backend (existing cluster) and a throwaway local backend
// (never-deployed cluster), so a dry run never bootstraps cloud state.
func stateBackendExists(ctx context.Context, subscriptionID string) (bool, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "azure.StateBackendExists")
	defer span.End()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("azure credentials: %w", err)
	}

	client, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("create storage accounts client: %w", err)
	}

	saName := stateStorageAccountName(subscriptionID)
	_, err = client.GetProperties(ctx, stateResourceGroupName, saName, nil)
	if err == nil {
		return true, nil
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == 404 {
		return false, nil
	}
	span.RecordError(err)
	return false, fmt.Errorf("get storage account %q: %w", saName, err)
}

func ensureStateResourceGroup(ctx context.Context, subscriptionID, location, rgName string, cred azcore.TokenCredential) error {
	client, err := armresources.NewResourceGroupsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create resource groups client: %w", err)
	}

	exists, err := client.CheckExistence(ctx, rgName, nil)
	if err != nil {
		return fmt.Errorf("check existence of resource group %q: %w", rgName, err)
	}
	if exists.Success {
		return nil
	}

	_, err = client.CreateOrUpdate(ctx, rgName, armresources.ResourceGroup{
		Location: to.Ptr(location),
		Tags: map[string]*string{
			tagManagedBy: to.Ptr(managedByValue),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("create resource group %q: %w", rgName, err)
	}
	return nil
}

func ensureStateStorageAccount(ctx context.Context, subscriptionID, location, rgName, saName string, cred azcore.TokenCredential) error {
	client, err := armstorage.NewAccountsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create storage accounts client: %w", err)
	}

	// GetProperties returns 404 if the account doesn't exist; treat that as
	// "needs creation" and any other error as fatal.
	_, err = client.GetProperties(ctx, rgName, saName, nil)
	if err == nil {
		return nil
	}
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 404 {
		return fmt.Errorf("get storage account %q: %w", saName, err)
	}

	poller, err := client.BeginCreate(ctx, rgName, saName, armstorage.AccountCreateParameters{
		Location: to.Ptr(location),
		Kind:     to.Ptr(armstorage.KindStorageV2),
		SKU: &armstorage.SKU{
			Name: to.Ptr(armstorage.SKUNameStandardLRS),
		},
		Properties: &armstorage.AccountPropertiesCreateParameters{
			AllowBlobPublicAccess:  to.Ptr(false),
			MinimumTLSVersion:      to.Ptr(armstorage.MinimumTLSVersionTLS12),
			EnableHTTPSTrafficOnly: to.Ptr(true),
		},
		Tags: map[string]*string{
			tagManagedBy: to.Ptr(managedByValue),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("begin create storage account %q: %w", saName, err)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("create storage account %q: %w", saName, err)
	}
	return nil
}

// ensureStateBlobVersioning enables blob versioning on the storage account
// backing Terraform state, matching AWS's S3 versioning posture. Blob
// versioning is the Azure equivalent of S3 object versioning: every overwrite
// of a state blob preserves the previous version, providing a recovery path
// if a tofu apply produces a corrupt state file. The call is idempotent —
// setting IsVersioningEnabled=true when it's already enabled is a no-op on
// the service side.
func ensureStateBlobVersioning(ctx context.Context, subscriptionID, rgName, saName string, cred azcore.TokenCredential) error {
	client, err := armstorage.NewBlobServicesClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create blob services client: %w", err)
	}

	_, err = client.SetServiceProperties(ctx, rgName, saName, armstorage.BlobServiceProperties{
		BlobServiceProperties: &armstorage.BlobServicePropertiesProperties{
			IsVersioningEnabled: to.Ptr(true),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("enable blob versioning on %q: %w", saName, err)
	}
	return nil
}

func ensureStateContainer(ctx context.Context, subscriptionID, rgName, saName, containerName string, cred azcore.TokenCredential) error {
	client, err := armstorage.NewBlobContainersClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create blob containers client: %w", err)
	}

	_, err = client.Get(ctx, rgName, saName, containerName, nil)
	if err == nil {
		return nil
	}
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 404 {
		return fmt.Errorf("get container %q: %w", containerName, err)
	}

	_, err = client.Create(ctx, rgName, saName, containerName, armstorage.BlobContainer{
		ContainerProperties: &armstorage.ContainerProperties{
			PublicAccess: to.Ptr(armstorage.PublicAccessNone),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("create container %q: %w", containerName, err)
	}
	return nil
}
