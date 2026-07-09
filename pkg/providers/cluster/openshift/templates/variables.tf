variable "region" {
  type        = string
  description = "AWS region for the ROSA HCP cluster."
}

variable "cluster_name" {
  type        = string
  description = "Name of the ROSA HCP cluster (NIC project name)."
}

variable "openshift_version" {
  type        = string
  description = "OpenShift version (e.g. 4.20.25)."
  default     = "4.20.25"
}

variable "machine_cidr" {
  type        = string
  description = "VPC CIDR block for the cluster's machine network."
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  type        = list(string)
  description = "Availability zones for the VPC/cluster. Single-AZ keeps cost low."
  default     = ["us-east-1a"]
}

variable "compute_machine_type" {
  type        = string
  description = "EC2 instance type for the worker machine pool."
  default     = "m5.xlarge"
}

variable "replicas" {
  type        = number
  description = "Number of worker nodes."
  default     = 2
}
