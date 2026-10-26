provider "azurerm" {
  features {}
  # subscription_id picked up from ARM_SUBSCRIPTION_ID env (exported by NIC from
  # the user's AZURE_SUBSCRIPTION_ID).

  resource_providers_to_register = [
    "Microsoft.Compute",
    "Microsoft.ContainerService",
    "Microsoft.ManagedIdentity",
    "Microsoft.Network",
    "Microsoft.Storage",
  ]
}
