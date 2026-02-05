# AWS Credential Validation Design

**Issue**: [#6 - Implement credential validation in AWS provider](https://github.com/nebari-dev/nebari-infrastructure-core/issues/6)
**Date**: 2026-02-05
**Status**: Approved

## Overview

Add credential validation to the AWS provider with two modes:

| Mode | Trigger | Behavior |
|------|---------|----------|
| **Default** | Always in `Validate()` | `sts:GetCallerIdentity` - silent on success |
| **Thorough** | `--validate-creds` flag | IAM Policy Simulator + EC2 dry-run |

## CLI Interface

```bash
# Fast validation (default) - silent on success
nic validate -f config.yaml

# Thorough credential check - shows identity on success
nic validate -f config.yaml --validate-creds
```

## Output Behavior

### Default Mode

- **Success**: Silent (no output)
- **Failure**: Error message with details

### Thorough Mode (`--validate-creds`)

**Success**:
```
AWS credentials validated
  Identity: arn:aws:iam::123456789012:user/deploy-user
  Account:  123456789012
```

**Failure**:
```
Error: AWS credential validation failed

Identity: arn:aws:iam::123456789012:user/deploy-user
Account:  123456789012

Missing permissions:
  - ec2:CreateVpc
  - eks:CreateCluster
  - iam:PassRole
```

### Unsupported Provider

When `--validate-creds` is used with a provider that doesn't support it:
```
Note: The local provider does not support --validate-creds
```

## Architecture

### Optional Interface Pattern

New interface in `pkg/provider/provider.go`:

```go
// CredentialValidator is an optional interface for providers that support
// thorough credential validation beyond basic authentication.
type CredentialValidator interface {
    ValidateCredentials(ctx context.Context, cfg *config.NebariConfig) error
}
```

CLI checks for interface support:

```go
if validateCreds {
    if cv, ok := p.(provider.CredentialValidator); ok {
        if err := cv.ValidateCredentials(ctx, cfg); err != nil {
            return err
        }
    } else {
        fmt.Printf("Note: The %s provider does not support --validate-creds\n", p.Name())
    }
}
```

### Provider Support Matrix

| Provider | Implements `CredentialValidator` | Reason |
|----------|----------------------------------|--------|
| AWS | Yes | IAM permissions can be validated |
| GCP | Future | Similar IAM model |
| Azure | Future | Similar RBAC model |
| Local | No | No cloud credentials to validate |

## Implementation Details

### Config-Aware Permission Checking

The permission list is built based on the configuration:

```go
func getRequiredPermissions(cfg *Config) []string {
    perms := []string{
        // Always required - core infrastructure
        "ec2:DescribeAvailabilityZones",
        "ec2:CreateTags",
        "eks:CreateCluster",
        "eks:DescribeCluster",
        // ... base permissions
    }

    // VPC permissions - skip if using existing VPC
    if cfg.ExistingVPCID == "" {
        perms = append(perms,
            "ec2:CreateVpc",
            "ec2:DeleteVpc",
            "ec2:CreateSubnet",
            // ...
        )
    }

    // IAM role permissions - skip if using existing roles
    if cfg.ExistingClusterRoleArn == "" {
        perms = append(perms,
            "iam:CreateRole",
            "iam:AttachRolePolicy",
            // ...
        )
    }

    // EFS permissions - only if enabled
    if cfg.EFS.Enabled {
        perms = append(perms,
            "elasticfilesystem:CreateFileSystem",
            // ...
        )
    }

    return perms
}
```

### Validation Methods

1. **STS GetCallerIdentity**: Confirms credentials work, retrieves ARN
2. **IAM Policy Simulator**: Checks if principal has required permissions
3. **EC2 Dry-Run**: For EC2 actions that support dry-run mode

## File Changes

### New Files

- `pkg/provider/aws/credentials.go` - Credential validation logic
- `pkg/provider/aws/credentials_test.go` - Unit tests

### Modified Files

- `pkg/provider/provider.go` - Add `CredentialValidator` interface
- `pkg/provider/aws/provider.go` - Call validation from `Validate()`
- `cmd/nic/validate.go` - Add `--validate-creds` flag

## Testing Strategy

### Unit Tests

| Test | Purpose |
|------|---------|
| `TestGetRequiredPermissions` | Verify config-aware permission list building |
| `TestGetRequiredPermissions_WithExistingVPC` | Skip VPC permissions when using existing VPC |
| `TestGetRequiredPermissions_WithEFSEnabled` | Include EFS permissions when enabled |
| `TestValidateCredentials_Success` | Mock STS + IAM clients, verify success path |
| `TestValidateCredentials_InvalidCreds` | Mock STS error, verify error message |
| `TestValidateCredentials_MissingPermissions` | Mock IAM simulator returning denied, verify output |

### Mock Interfaces

```go
type mockSTSClient struct {
    GetCallerIdentityFunc func(ctx context.Context, input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error)
}

type mockIAMClient struct {
    SimulatePrincipalPolicyFunc func(ctx context.Context, input *iam.SimulatePrincipalPolicyInput) (*iam.SimulatePrincipalPolicyOutput, error)
}
```

No integration tests - would require real AWS credentials.

## Required AWS IAM Permissions

Full list based on code review of `pkg/provider/aws/` and Terraform modules:

### Always Required

#### STS
- `sts:GetCallerIdentity`

#### S3 (State Bucket Management)

These permissions are required by the Go CLI for Terraform state bucket lifecycle management:

- `s3:HeadBucket` - Check if state bucket exists
- `s3:CreateBucket` - Create state bucket
- `s3:PutBucketVersioning` - Enable versioning on state bucket
- `s3:PutPublicAccessBlock` - Block public access to state bucket
- `s3:ListObjectVersions` - List objects before deletion (destroy)
- `s3:DeleteObject` - Delete objects in bucket (destroy)
- `s3:DeleteBucket` - Delete state bucket (destroy)

#### EC2 (Core)
- `ec2:DescribeAvailabilityZones`
- `ec2:CreateTags`
- `ec2:DeleteTags`

#### EKS
- `eks:CreateCluster`
- `eks:DeleteCluster`
- `eks:DescribeCluster`
- `eks:UpdateClusterVersion`
- `eks:UpdateClusterConfig`
- `eks:CreateNodegroup`
- `eks:DeleteNodegroup`
- `eks:DescribeNodegroup`
- `eks:ListNodegroups`
- `eks:UpdateNodegroupConfig`
- `eks:TagResource`
- `eks:UntagResource`

### VPC Creation (skip if `existing_vpc_id` set)

#### EC2 (VPC)

- `ec2:CreateVpc`
- `ec2:DeleteVpc`
- `ec2:DescribeVpcs`
- `ec2:ModifyVpcAttribute`
- `ec2:CreateSubnet`
- `ec2:DeleteSubnet`
- `ec2:DescribeSubnets`
- `ec2:CreateInternetGateway`
- `ec2:DeleteInternetGateway`
- `ec2:AttachInternetGateway`
- `ec2:DetachInternetGateway`
- `ec2:DescribeInternetGateways`
- `ec2:AllocateAddress`
- `ec2:ReleaseAddress`
- `ec2:DescribeAddresses`
- `ec2:CreateNatGateway`
- `ec2:DeleteNatGateway`
- `ec2:DescribeNatGateways`
- `ec2:CreateRouteTable`
- `ec2:DeleteRouteTable`
- `ec2:DescribeRouteTables`
- `ec2:CreateRoute`
- `ec2:AssociateRouteTable`
- `ec2:DisassociateRouteTable`
- `ec2:CreateSecurityGroup`
- `ec2:DeleteSecurityGroup`
- `ec2:DescribeSecurityGroups`
- `ec2:AuthorizeSecurityGroupIngress`
- `ec2:AuthorizeSecurityGroupEgress`
- `ec2:CreateVpcEndpoint`
- `ec2:DeleteVpcEndpoints`
- `ec2:DescribeVpcEndpoints`
- `ec2:DescribeNetworkInterfaces`

### IAM Role Creation (skip if `existing_cluster_role_arn` set)

- `iam:CreateRole`
- `iam:DeleteRole`
- `iam:GetRole`
- `iam:AttachRolePolicy`
- `iam:DetachRolePolicy`
- `iam:ListAttachedRolePolicies`
- `iam:PassRole`
- `iam:TagRole`

### EFS (only if `efs.enabled: true`)

- `elasticfilesystem:CreateFileSystem`
- `elasticfilesystem:DeleteFileSystem`
- `elasticfilesystem:DescribeFileSystems`
- `elasticfilesystem:CreateMountTarget`
- `elasticfilesystem:DeleteMountTarget`
- `elasticfilesystem:DescribeMountTargets`
- `elasticfilesystem:TagResource`

## Related Issues

- [#54 - Add --gen-perms flag to output required cloud provider permissions](https://github.com/nebari-dev/nebari-infrastructure-core/issues/54)

## References

- [AWS STS GetCallerIdentity](https://docs.aws.amazon.com/STS/latest/APIReference/API_GetCallerIdentity.html)
- [IAM Policy Simulator](https://docs.aws.amazon.com/IAM/latest/UserGuide/access_policies_testing-policies.html)
- [AWS SDK Error Handling](https://aws.github.io/aws-sdk-go-v2/docs/handling-errors/)
