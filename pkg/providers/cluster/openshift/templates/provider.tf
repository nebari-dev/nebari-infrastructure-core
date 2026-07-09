terraform {
  required_version = ">= 1.6.0"
  required_providers {
    rhcs = {
      source  = "terraform-redhat/rhcs"
      version = ">= 1.6.2, < 2.0.0"
    }
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

# The rhcs provider reads its OCM token from the RHCS_TOKEN environment variable,
# which NIC's provision-mode Validate requires. No token is written to disk.
provider "rhcs" {}

provider "aws" {
  region = var.region
}
