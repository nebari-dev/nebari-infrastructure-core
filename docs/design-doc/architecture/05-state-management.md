# State Management with Terraform State

## 5.1 Overview

NIC uses **Terraform state files** to track infrastructure. The state file records what resources Terraform has created and their current configuration, enabling drift detection and safe updates.

## 5.2 Terraform State Backends

NIC generates backend configuration based on the cloud provider and user preferences.

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

## 5.3 State Locking

Terraform handles locking automatically to prevent concurrent modifications:

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

## 5.4 Drift Detection

Drift detection compares state file with actual cloud infrastructure via `terraform plan`:

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

## 5.5 State Operations

NIC exposes Terraform state commands:

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

## 5.6 State Migration

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

## 5.7 State File Security

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

## 5.8 State Backend Setup

NIC can automatically create state backend resources:

```bash
# Initialize state backend (creates S3 bucket, DynamoDB table, etc.)
nic init-backend -f config.yaml

Creating state backend resources...
  - S3 Bucket: nebari-prod-terraform-state
  - DynamoDB Table: nebari-prod-terraform-locks

State backend initialized successfully.
```

Or users can create resources manually and configure in config.yaml:

```yaml
# config.yaml
project_name: nebari-prod
provider: aws

state_backend:
  type: s3
  bucket: my-existing-state-bucket
  key: nebari/terraform.tfstate
  region: us-west-2
  dynamodb_table: my-existing-lock-table
```

## 5.9 Working Directory Management

OpenTofu requires a working directory with state and modules:

```
.nic/
├── terraform/          # Working directory
│   ├── .terraform/    # Terraform plugins and modules
│   ├── terraform.tfstate  # State file (if using local backend)
│   ├── vars.json      # Generated from config.yaml
│   └── backend.tf     # Generated backend configuration
```

The Go CLI manages this working directory lifecycle:
1. Create working directory if not exists
2. Copy Terraform modules from embedded FS or git clone
3. Generate `vars.json` from `config.yaml`
4. Generate `backend.tf` from config
5. Run `tofu init`, `tofu plan`, `tofu apply`
6. Cleanup temporary files

---

## Summary

NIC uses standard Terraform state management with remote backends, providing:

- **Team collaboration**: State locking prevents concurrent modifications
- **Drift detection**: `terraform plan` compares state with actual infrastructure
- **State versioning**: Remote backends support versioning for recovery
- **Ecosystem compatibility**: Works with Atlantis, Terraform Cloud, and other tools

See [Terraform-Exec Integration](../implementation/08-terraform-exec-integration.md) for how the Go CLI manages state operations.
