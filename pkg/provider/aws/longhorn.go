package aws

// Longhorn install logic moved to pkg/storage/longhorn so the existing-cluster
// provider (and any future on-prem provider) can install Longhorn the same
// way the AWS provider does. AWS-specific behaviour lives in this package as
// LonghornEnabled / LonghornReplicaCount on the AWS Config (see config.go).
