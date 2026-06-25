# The rest of the backend configuration is populated dynamically during init
# (bucket/key/region), matching the aws provider's state pattern.
terraform {
  backend "s3" {
    encrypt      = true
    use_lockfile = true
  }
}
