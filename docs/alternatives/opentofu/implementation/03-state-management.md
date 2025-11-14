# State Management with Terraform State

**Note**: This document describes state management in the OpenTofu alternative design. See [../README.md](../README.md) for comparison with the stateless native SDK design.

## Overview

The OpenTofu alternative design uses **Terraform state files** to track infrastructure, unlike the main design which is stateless and queries cloud APIs directly.

### Key Difference

**Native SDK Design (Main)**:
- Stateless: No state files
- Queries cloud APIs for actual state on every run
- Uses tag-based discovery to find NIC-managed resources

**OpenTofu Design (Alternative)**:
- Stateful: Terraform state file tracks expected state
- Compares state file with cloud APIs to detect drift
- Requires state backend configuration and locking

## Terraform State Backends

NIC generates backend configuration based on provider and user preferences.

### AWS (S3 + DynamoDB for Locking)

```hcl
terraform {
  backend "s3" {
    bucket         = "nebari-prod-terraform-state"
    key            = "nic/terraform.tfstate"
    region         = "us-west-2"
    encrypt        = true
    dynamodb_table = "nebari-prod-terraform-locks"
  }
}
```

**Setup Requirements**:
1. Create S3 bucket for state storage
2. Enable versioning on S3 bucket
3. Create DynamoDB table for locking (primary key: `LockID` string)
4. Configure appropriate IAM permissions

### GCP (Cloud Storage)

```hcl
terraform {
  backend "gcs" {
    bucket = "nebari-prod-terraform-state"
    prefix = "nic"
  }
}
```

**Setup Requirements**:
1. Create Cloud Storage bucket
2. Enable object versioning
3. Configure appropriate IAM permissions
4. Locking handled automatically by GCS

### Azure (Blob Storage)

```hcl
terraform {
  backend "azurerm" {
    storage_account_name = "nebaristate"
    container_name       = "tfstate"
    key                  = "nic/terraform.tfstate"
  }
}
```

**Setup Requirements**:
1. Create Azure Storage Account
2. Create blob container
3. Configure appropriate RBAC permissions
4. Locking handled via blob lease mechanism

### Local (Development Only)

```hcl
terraform {
  backend "local" {
    path = "terraform.tfstate"
  }
}
```

**Not Recommended for Production**:
- No team collaboration support
- No state locking across multiple users
- State file lost if local machine fails

## State Locking

Terraform handles locking automatically:

| Backend | Locking Mechanism | Configuration Required |
|---------|------------------|----------------------|
| **S3** | DynamoDB table | Create table with `LockID` primary key |
| **GCS** | Object generation metadata | None (automatic) |
| **Azure Blob** | Blob lease | None (automatic) |
| **Local** | File locking | None (automatic) |

### Lock Behavior

When `nic deploy` runs:
1. Terraform acquires lock before `terraform plan`
2. Lock prevents concurrent modifications
3. Lock released after `terraform apply` completes
4. If NIC crashes, lock auto-expires (configurable timeout)

### Lock Conflicts

```bash
$ nic deploy -f config.yaml

Error: Error acquiring the state lock

Lock Info:
  ID:        a1b2c3d4-5678-90ef-ghij-klmnopqrstuv
  Path:      nebari-prod-terraform-state/nic/terraform.tfstate
  Operation: OperationTypeApply
  Who:       user@hostname
  Version:   1.6.0
  Created:   2025-01-14 15:30:00 UTC
  Info:

Another operation is currently holding the state lock.
If you're sure no other operation is running, you can force unlock:
  nic unlock -f config.yaml
```

## Drift Detection

Drift detection compares state file with actual cloud infrastructure:

```go
func (p *TofuProvider) DetectDrift(ctx context.Context) (*DriftReport, error) {
    ctx, span := tracer.Start(ctx, "TofuProvider.DetectDrift")
    defer span.End()

    slog.InfoContext(ctx, "detecting infrastructure drift")

    // terraform plan compares state file with actual cloud state
    hasChanges, err := p.executor.Plan(ctx, []string{"terraform.tfvars"})
    if err != nil {
        return nil, fmt.Errorf("running terraform plan: %w", err)
    }

    if !hasChanges {
        slog.InfoContext(ctx, "no drift detected")
        return &DriftReport{DriftsDetected: 0}, nil
    }

    // terraform-exec provides structured plan output
    plan, err := p.executor.ShowPlanFile(ctx, "tfplan")
    if err != nil {
        return nil, fmt.Errorf("parsing plan: %w", err)
    }

    // Parse changes into drift report
    drifts := []Drift{}
    for _, change := range plan.ResourceChanges {
        if change.Change.Actions.Delete() || change.Change.Actions.Update() {
            drifts = append(drifts, Drift{
                Resource: change.Address,
                Type:     change.Type,
                Action:   change.Change.Actions.String(),
            })
        }
    }

    slog.WarnContext(ctx, "infrastructure drift detected", "drift_count", len(drifts))

    return &DriftReport{
        DriftsDetected: len(drifts),
        Drifts:         drifts,
    }, nil
}
```

### Drift Scenarios

**Scenario 1: Resource Deleted Outside Terraform**
```
Plan: 1 to add, 0 to change, 0 to destroy

  # aws_eks_node_group.workers will be created
  + resource "aws_eks_node_group" "workers" {
      + arn           = (known after apply)
      + cluster_name  = "nebari-prod"
      ...
    }
```

**Scenario 2: Resource Modified Outside Terraform**
```
Plan: 0 to add, 1 to change, 0 to destroy

  # aws_eks_node_group.workers will be updated in-place
  ~ resource "aws_eks_node_group" "workers" {
      ~ desired_size = 3 -> 5
        ...
    }
```

**Scenario 3: No Drift**
```
No changes. Your infrastructure matches the configuration.
```

## State Operations

Terraform provides built-in state commands that NIC can expose:

```bash
# List resources in state
nic state list

# Show specific resource
nic state show aws_eks_cluster.main

# Remove resource from state (doesn't destroy infrastructure)
nic state rm aws_eks_node_group.old_pool

# Move resource to different address
nic state mv aws_eks_node_group.workers aws_eks_node_group.renamed
```

## State Migration

When changing backend configuration:

```bash
# Old backend configuration
terraform {
  backend "local" {
    path = "terraform.tfstate"
  }
}

# New backend configuration
terraform {
  backend "s3" {
    bucket = "nebari-prod-terraform-state"
    key    = "nic/terraform.tfstate"
  }
}

# NIC handles migration
$ nic deploy -f config.yaml

Detected backend configuration change.
Migrating state from local to s3...

Terraform will perform the following actions:

  Copying state from "local" backend to "s3" backend.

Do you want to copy existing state to the new backend?
  Enter a value: yes

Successfully migrated state to new backend.
```

## State File Security

### Encryption at Rest

- **S3**: Enable bucket encryption (SSE-S3 or SSE-KMS)
- **GCS**: Enable bucket encryption (Google-managed or customer-managed keys)
- **Azure Blob**: Enable storage account encryption

### Access Control

- **S3**: IAM policies restricting bucket access
- **GCS**: IAM policies for Cloud Storage
- **Azure Blob**: RBAC for storage account access

### Sensitive Data in State

Terraform state files contain **sensitive data**:
- Kubernetes cluster credentials
- Database passwords
- API keys
- Certificate private keys

**Best Practices**:
1. Never commit state files to version control
2. Use encrypted remote backends
3. Restrict state file access via IAM/RBAC
4. Enable state file versioning for recovery
5. Regularly rotate credentials stored in state

## Comparison with Native SDK Stateless Approach

| Aspect | OpenTofu (Stateful) | Native SDK (Stateless) |
|--------|---------------------|----------------------|
| **State Storage** | Terraform state file in S3/GCS/Azure Blob | No state file |
| **State Queries** | Read from state file | Query cloud APIs directly |
| **Drift Detection** | Compare state file with cloud APIs | Compare config with cloud APIs |
| **Locking** | Terraform backend locking | No locking needed (stateless) |
| **Setup** | Create state backend resources | No setup needed |
| **Collaboration** | State backend shared across team | Tag-based discovery shared across team |
| **State Drift Risk** | State can diverge from reality | Always queries actual state |
| **Performance** | Faster (reads from state file) | Slower (queries cloud APIs) |

## Trade-offs

### Advantages of Terraform State

- ✅ Standard format understood by Terraform ecosystem
- ✅ Built-in locking prevents concurrent modifications
- ✅ State history/versioning for rollback
- ✅ Terraform tooling works (atlantis, terraform-docs, etc.)
- ✅ Faster operations (reads from state vs. cloud API queries)

### Disadvantages of Terraform State

- ⚠️ State drift: state file can diverge from actual infrastructure
- ⚠️ Additional setup: must create and manage state backend
- ⚠️ Security: state file contains sensitive data
- ⚠️ Complexity: state locking, migration, corruption recovery
- ⚠️ Single point of failure: corrupted state file breaks deployments

## Summary

The OpenTofu alternative uses standard Terraform state management, which provides familiarity for teams with Terraform experience but adds stateful complexity compared to the main design's stateless approach.

For a stateless alternative that avoids these trade-offs, see the main native SDK design documentation.

---
