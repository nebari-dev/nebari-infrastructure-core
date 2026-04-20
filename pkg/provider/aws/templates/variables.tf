variable "region" {
  type = string
}

variable "project_name" {
  type = string
}

variable "tags" {
  type    = map(string)
  default = {}
}

variable "availability_zones" {
  type    = list(string)
  default = []
}

variable "create_vpc" {
  type = bool
}

variable "vpc_cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

variable "existing_vpc_id" {
  type    = string
  default = null
}

variable "existing_private_subnet_ids" {
  type    = list(string)
  default = []
}

variable "create_security_group" {
  type = bool
}

variable "existing_security_group_id" {
  type    = string
  default = null
}

variable "kubernetes_version" {
  type    = string
  default = null
}

variable "endpoint_private_access" {
  type = bool
}

variable "endpoint_public_access" {
  type = bool
}

variable "eks_kms_arn" {
  type    = string
  default = null
}

variable "cluster_enabled_log_types" {
  type    = list(string)
  default = []
}

variable "create_iam_roles" {
  type = bool
}

variable "existing_cluster_iam_role_arn" {
  type    = string
  default = null
}

variable "existing_node_iam_role_arn" {
  type    = string
  default = null
}

variable "iam_role_permissions_boundary" {
  type    = string
  default = null
}

variable "node_groups" {
  type = any
}

variable "efs_enabled" {
  type = bool
}

variable "efs_performance_mode" {
  type    = string
  default = "generalPurpose"
}

variable "efs_throughput_mode" {
  type    = string
  default = "bursting"
}

variable "efs_provisioned_throughput_in_mibps" {
  type    = number
  default = null
}

variable "efs_encrypted" {
  type = bool
}

variable "efs_kms_key_arn" {
  type    = string
  default = null
}

variable "node_security_group_additional_rules" {
  type    = any
  default = {}
}
