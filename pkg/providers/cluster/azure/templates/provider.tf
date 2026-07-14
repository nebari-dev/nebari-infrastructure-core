provider "azurerm" {
  features {}
  # subscription_id picked up from ARM_SUBSCRIPTION_ID env (exported by NIC from
  # the user's AZURE_SUBSCRIPTION_ID).
}
