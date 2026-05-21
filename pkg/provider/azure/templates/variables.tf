variable "project_name" {
  type = string
}

variable "location" {
  type = string
}

variable "tags" {
  type    = map(string)
  default = {}
}

variable "create_resource_group" {
  type    = bool
  default = true
}

variable "existing_resource_group_name" {
  type    = string
  default = null
}

variable "create_vnet" {
  type    = bool
  default = true
}

variable "vnet_cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

variable "node_subnet_cidr_block" {
  type    = string
  default = "10.0.0.0/22"
}

variable "existing_vnet_id" {
  type    = string
  default = null
}

variable "existing_node_subnet_id" {
  type    = string
  default = null
}

variable "network_plugin" {
  type    = string
  default = "azure"
}

variable "network_plugin_mode" {
  type    = string
  default = "overlay"
}

variable "pod_cidr" {
  type    = string
  default = "10.244.0.0/16"
}

variable "service_cidr" {
  type    = string
  default = "10.0.16.0/22"
}

variable "dns_service_ip" {
  type    = string
  default = "10.0.16.10"
}

variable "kubernetes_version" {
  type    = string
  default = null
}

variable "private_cluster_enabled" {
  type    = bool
  default = false
}

variable "authorized_ip_ranges" {
  type    = list(string)
  default = []
}

variable "sku_tier" {
  type    = string
  default = "Free"
}

variable "identity_type" {
  type    = string
  default = "UserAssigned"
}

variable "node_groups" {
  type = map(object({
    vm_size         = string
    min_count       = number
    max_count       = number
    mode            = optional(string, "User")
    os_disk_size_gb = optional(number, 128)
    labels          = optional(map(string), {})
    taints          = optional(list(string), [])
    zones           = optional(list(string), [])
  }))
}
