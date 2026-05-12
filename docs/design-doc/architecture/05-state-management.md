# State Management

## 5.1 Overview

State management in NIC is **provider-specific**. There is no single state mechanism that spans all cluster providers.

| Provider | State Mechanism |
|----------|-----------------|
| AWS | OpenTofu state in S3, with native lockfile-based locking |
| Hetzner | `hetzner-k3s` writes a cluster state file managed by that tool |
| Local (Kind) | Kind manages its own cluster lifecycle; no NIC-owned state |
| Existing | No state; NIC adopts a cluster by `kubeconfig`/`context` |

This document focuses on the **AWS provider**, which is the only provider that uses OpenTofu today.

## 5.2 AWS State Backend

The AWS provider uses the standard Terraform S3 backend with native lockfile-based locking (introduced in OpenTofu/Terraform 1.10):

```hcl
# pkg/provider/aws/templates/backend.tf
terraform {
  backend "s3" {
    encrypt      = true
    use_lockfile = true
  }
}
```

Bucket and key are not hard-coded; they are populated via `-backend-config` flags at `tofu init` time from values computed in `pkg/provider/aws/state.go`.

### Bucket Naming

The bucket name is deterministic and not user-configurable today:

```
nic-tfstate-<project_name>-<region>-<8-hex-chars-of-sha256(account_id)>
```

For example, `nic-tfstate-my-nebari-us-west-2-1a2b3c4d`. The account ID is hashed rather than embedded directly. The total length is checked against the 63-character S3 bucket name limit; project names that would overflow it return an error.

The state object key is `<project_name>/terraform.tfstate`.

### Bucket Lifecycle

NIC creates the bucket automatically on first deploy (`ensureStateBucket` in `pkg/provider/aws/state.go`) with:

- Versioning enabled
- Public access fully blocked (`PutPublicAccessBlock`)
- SSE enabled at the backend level (`encrypt = true` in `backend.tf`)

On `nic destroy`, the bucket and all object versions are deleted (`destroyStateBucket`). The bucket lifecycle is owned by NIC; there is no separate "setup" step the user runs first.

### Locking

`use_lockfile = true` makes OpenTofu acquire the state lock by writing a `.tflock` object to S3 next to the state file. This replaces the older pattern of using a DynamoDB table for locks. NIC does **not** create or manage a DynamoDB table; if you see references to one anywhere, that is a documentation bug.

Lock conflicts surface as an error from `tofu apply` like:

```
Error: Error acquiring the state lock
Lock Info:
  ID:        ...
  Path:      <bucket>/<project>/terraform.tfstate
  Operation: OperationTypeApply
```

Today there is no `nic unlock` command; recovery from a stuck lock requires manual intervention via `tofu force-unlock` or by deleting the `.tflock` S3 object. Adding `nic unlock` is tracked in [issue #64](https://github.com/nebari-dev/nebari-infrastructure-core/issues/64); Ctrl-C-leaves-state-locked is tracked in [issue #63](https://github.com/nebari-dev/nebari-infrastructure-core/issues/63).

## 5.3 Drift Detection

Drift detection is exposed via `--dry-run`:

```bash
nic deploy -f config.yaml --dry-run
```

Under the hood, this calls `Provider.Deploy(ctx, ..., DeployOptions{DryRun: true})`. The AWS provider implementation runs `tofu plan` and streams structured plan output through the status channel; the CLI translates it into a human-readable summary.

There is no separate `nic status`, `nic plan`, or `nic state` subcommand today. Drift information is communicated through `--dry-run`.

## 5.4 State File Security

Terraform state files contain sensitive material (cluster credentials, certificate authority data, etc.). NIC mitigates this via:

- **Encryption at rest**: SSE enabled (`encrypt = true`) on the S3 backend
- **Public-access block**: NIC sets `BlockPublicAcls`, `BlockPublicPolicy`, `IgnorePublicAcls`, and `RestrictPublicBuckets` on the state bucket
- **IAM**: bucket access is controlled by the IAM identity NIC runs under; restrict it to the smallest set of principals that need to operate the cluster
- **Versioning**: enabled by default so accidental state corruption can be recovered

**Operator best practices:**

1. Never commit state files to version control
2. Restrict state-bucket access to a small operator group
3. Rotate credentials (e.g., Keycloak admin password) after they appear in plan output

## 5.5 Working Directory

`pkg/tofu.Setup` creates a fresh temporary working directory for each NIC invocation:

1. Allocates a temp directory via `afero.TempDir(appFs, "", "nic-tofu")`
2. Walks the embedded `templates/` filesystem and copies each file into the working dir
3. Downloads (or reuses, from `~/.cache/nic/tofu/`) the OpenTofu binary and writes it into the working dir
4. Sets `TF_PLUGIN_CACHE_DIR` to `~/.cache/nic/tofu/plugins` so provider plugins are reused across runs
5. Marshals provider-supplied tfvars to `terraform.tfvars.json` in the working dir
6. Returns a `TerraformExecutor` whose `Cleanup()` method removes the working dir

There is no `.nic/` directory in the user's home or project root; everything is ephemeral except the binary and plugin caches.

For dry-run scenarios where the remote state bucket might not yet exist, `WriteBackendOverride()` writes a `backend_override.tf.json` that overrides the backend with a local backend for that single run.

## 5.6 Future Work

The following are known gaps and tracked in GitHub issues:

- **`nic unlock` command** ([#64](https://github.com/nebari-dev/nebari-infrastructure-core/issues/64)) - graceful recovery from stuck S3 lockfiles
- **Ctrl-C cleanup during destroy** ([#63](https://github.com/nebari-dev/nebari-infrastructure-core/issues/63)) - currently can leave state locked
- **Redundant tofu init / module downloads** ([#241](https://github.com/nebari-dev/nebari-infrastructure-core/issues/241)) - module downloads are repeated unnecessarily because the working dir is ephemeral
- **`nic state` subcommands** - currently the only way to manipulate state is via the bundled tofu binary directly; first-class state subcommands are not implemented
- **GCP / Azure backend support** - blocked on those providers being implemented at all

See also [Terraform-Exec Integration](../implementation/08-terraform-exec-integration.md).
