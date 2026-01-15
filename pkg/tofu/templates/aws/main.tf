# Backend configuration will be populated dynamically during initialization
# terraform {
#   backend "s3" {}
# }

# Variables will be passed during apply/destroy using a tfvars.json file
module "eks_cluster" {
  source = "github.com/nebari-dev/terraform-aws-eks-cluster?ref=3071b1419efc126744e223ae5b59072a60bef263"
}
