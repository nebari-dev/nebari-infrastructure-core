# The rest of the backend configuration will be populated dynamically during initialization
terraform {
  backend "s3" {
    encrypt      = true
    use_lockfile = true
  }
}
