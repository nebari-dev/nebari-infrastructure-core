# ROSA HCP cluster, encoded from the Phase A manual flow
# (docs/superpowers/plans/phase-a-runlog.md). Composes the official
# terraform-redhat VPC submodule with the rosa-hcp cluster module so a single
# `nic deploy` stands up networking, IAM (account + operator roles), OIDC, and
# the cluster.

locals {
  # Account roles are account-global; reuse a stable prefix so repeated deploys
  # in the same AWS account share one set rather than creating duplicates.
  account_role_prefix  = "ManagedOpenShift-HCP"
  operator_role_prefix = var.cluster_name
}

module "vpc" {
  source  = "terraform-redhat/rosa-hcp/rhcs//modules/vpc"
  version = ">= 1.6.2, < 2.0.0"

  name_prefix              = var.cluster_name
  vpc_cidr                 = var.machine_cidr
  availability_zones       = var.availability_zones
}

module "rosa_hcp" {
  source  = "terraform-redhat/rosa-hcp/rhcs"
  version = ">= 1.6.2, < 2.0.0"

  cluster_name           = var.cluster_name
  openshift_version      = var.openshift_version
  machine_cidr           = var.machine_cidr
  aws_availability_zones = var.availability_zones
  aws_subnet_ids         = concat(module.vpc.private_subnets, module.vpc.public_subnets)

  replicas             = var.replicas
  compute_machine_type = var.compute_machine_type

  # IAM: account roles are account-global (reuse stable prefix); operator roles
  # + OIDC are per-cluster.
  create_account_roles  = true
  account_role_prefix   = local.account_role_prefix
  create_operator_roles = true
  operator_role_prefix  = local.operator_role_prefix
  create_oidc           = true
}
