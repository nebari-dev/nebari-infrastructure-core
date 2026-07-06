# Longhorn Backups

NIC provisions off-cluster backups natively, without any third-party Helm pack.
When `backups.longhorn.enabled: true`, a `nic deploy` creates:

- **Two RecurringJobs** — `default-hourly-snapshot` and `default-daily-backup` —
  managed as ArgoCD-rendered Longhorn CRDs.
- **The `allow-recurring-job-while-volume-detached` cluster Setting** — set to
  `true` by default (see [Coverage model](#coverage-model) below).
- **`BackupTarget/default`** — the S3 or azblob:// target URL, synced via ArgoCD.
- **`longhorn-backup-credentials` Secret** in `longhorn-system` — created
  imperatively by NIC from the env vars named in the config.

## Coverage model

The two RecurringJobs target Longhorn's built-in `default` recurring-job-group.
Every volume created on the cluster's default `longhorn` StorageClass is
automatically a member of that group, so no per-volume wiring is required.

The `allow-recurring-job-while-volume-detached` setting deserves attention:
JupyterHub user PVCs detach when servers idle out. Longhorn's stock default is
`false`, which silently skips detached volumes at the cron tick. NIC defaults
this setting to `true` so that Longhorn briefly auto-attaches idle PVCs to take
the scheduled snapshot or backup before detaching again.

## Configuration

Add a `backups:` block to your NebariConfig. See
[`examples/longhorn-backups-config.yaml`](../examples/longhorn-backups-config.yaml)
for a complete, annotated AWS example.

```yaml
backups:
  longhorn:
    enabled: true
    s3:
      bucket: my-nebari-backups
      region: us-east-1
      prefix: clusterA/
      create_bucket: true       # AWS/Azure only; omit when using an external bucket
      retain_on_destroy: true   # default true; keeps bucket on `nic destroy`
      access_key_id_env: LONGHORN_S3_ACCESS_KEY_ID
      secret_access_key_env: LONGHORN_S3_SECRET_ACCESS_KEY
    allow_recurring_job_while_volume_detached: true
    schedules:
      snapshot:
        cron: "0 * * * *"
        retain: 24
        concurrency: 5
      backup:
        cron: "0 3 * * *"
        retain: 30
        concurrency: 3
```

### Required environment variables

NIC reads the env var names from `s3.access_key_id_env` /
`s3.secret_access_key_env` (or `azure.account_name_env` / `azure.account_key_env`
for Azure). The defaults shown above map to:

| Variable | Purpose |
|----------|---------|
| `LONGHORN_S3_ACCESS_KEY_ID` | AWS or S3-compatible access key |
| `LONGHORN_S3_SECRET_ACCESS_KEY` | Corresponding secret key |

Set these before running `nic deploy`. They are written only to the in-cluster
Secret, never committed to the GitOps repository.

### `create_bucket` vs external endpoint

`create_bucket: true` (AWS) / `create_container: true` (Azure) tells NIC to
provision the bucket or storage container via the provider's Terraform module.
This is only supported on `aws` and `azure` providers.

When `s3.endpoint` is set, NIC assumes the bucket is externally managed and
**never** attempts to create it. `create_bucket` and `endpoint` are mutually
exclusive — `nic validate` rejects both being set together.

### `retain_on_destroy`

Defaults to `true`. On `nic destroy`, NIC removes the bucket (AWS) or storage
account+container (Azure) from Terraform state before running destroy, orphaning
them so their contents survive. Set `retain_on_destroy: false` to have them
deleted with the cluster (data loss — use with care).

## Third-party / non-AWS S3

Longhorn uses the same AWS S3 client for all S3-compatible targets. Three env
var hooks control non-AWS endpoints:

| Config field | Longhorn env var | Purpose |
|---|---|---|
| `s3.endpoint` | `AWS_ENDPOINTS` | Custom endpoint URL (MinIO, Wasabi, Cloudflare R2, …) |
| `s3.virtual_hosted_style` | `VIRTUAL_HOSTED_STYLE` | `true` = `bucket.host` style; `false` = path style |
| `s3.ca_cert` | `AWS_CERT` | PEM CA bundle for self-signed TLS |

When `endpoint` is set, `create_bucket` must be omitted or `false` — NIC never
creates buckets against a third-party endpoint.

`ca_cert` references a **pre-existing** Secret or ConfigMap in the cluster. NIC
reads the named key at deploy time and injects the PEM bundle as `AWS_CERT`:

```yaml
s3:
  endpoint: https://minio.internal.example.com
  virtual_hosted_style: false
  bucket: my-bucket
  region: us-east-1
  access_key_id_env: LONGHORN_S3_ACCESS_KEY_ID
  secret_access_key_env: LONGHORN_S3_SECRET_ACCESS_KEY
  ca_cert:
    kind: secret          # "secret" or "configmap"
    name: longhorn-s3-ca
    namespace: longhorn-system
    key: ca.crt
```

See the Longhorn docs for further detail:
[Set Backup Target](https://longhorn.io/docs/latest/snapshots-and-backups/backup-and-restore/set-backup-target/)

## Azure (azblob)

> **Not yet functional.** NIC does not install Longhorn on Azure clusters — the
> Azure provider's storage layer is `managed-csi`, not Longhorn — so the
> `longhorn.io` CRDs that backups depend on are never present. Enabling
> `backups.longhorn` on an Azure cluster currently **fails `nic validate` /
> `nic deploy`** with a clear error. The `azure:` configuration below is
> documented as forward-looking scaffolding for a future "Longhorn on Azure"
> prerequisite; it will become usable once NIC can install Longhorn on Azure.

Azure uses Longhorn's native `azblob://` target. Configure the `azure:` sub-block
instead of `s3:`:

```yaml
backups:
  longhorn:
    enabled: true
    azure:
      container: my-nebari-backups
      storage_account: mynebaribackupssa
      prefix: clusterA/
      create_container: true   # requires the azure provider
      retain_on_destroy: true
      account_name_env: LONGHORN_AZURE_ACCOUNT_NAME
      account_key_env: LONGHORN_AZURE_ACCOUNT_KEY
```

`create_container: true` provisions the storage account and container via the
Azure Terraform module. Only valid with the `azure` provider.

**`retain_on_destroy` caveat:** the `azurerm` Terraform provider has no
`force_destroy` for non-empty containers. NIC enforces `retain_on_destroy: true`
(the default) by removing the storage account and container from Terraform state
before running destroy — they are orphaned and kept. With `retain_on_destroy:
false`, the storage account and container are deleted with the cluster (data loss
— use with care).

## Restore runbook (same cluster)

Restoring a volume is a Longhorn UI/CLI operation. NIC schedules and configures
backups but does not orchestrate restores.

1. **Stop the user's pod** to unbind the PVC. In JupyterHub, shut down the server
   from the admin panel or let the idle culler terminate it.
2. **Identify the Longhorn volume name** from the PVC spec:
   ```bash
   kubectl get pvc <pvc-name> -n <namespace> -o jsonpath='{.spec.volumeName}'
   ```
3. **Revert or restore:**
   - *In-place revert* — in the Longhorn UI, find the volume, choose a snapshot,
     and click **Revert**. The volume must be detached first.
   - *Restore from backup* — in the Longhorn UI, select a backup and click
     **Restore**. This creates a new Volume CR. Update the PVC's
     `spec.volumeName` to point to the restored volume name, then delete and
     recreate the PVC-to-volume binding (or patch `spec.volumeName` on the PVC
     if the volume is in Released state).
4. **Update `spec.volumeName`** on the PVC to point to the restored volume if
   restoring from backup into a new volume.
5. **Restart the user's server** — KubeSpawner re-attaches the PVC to the new
   pod.

**RWX note:** shared volumes (ReadWriteMany) are served by a `share-manager` pod.
After restoring, wait for the share-manager pod to reach `Ready` before user pods
can mount the restored volume.

## Cross-cluster disaster recovery

A fresh Longhorn cluster pointed at the same S3 bucket (or Azure container)
discovers backup metadata after its poll cycle runs. The bucket is the source of
truth; the cluster is replaceable. To restore onto a new cluster:

1. Deploy a new cluster with the same `backups.longhorn.s3.bucket` (and matching
   credentials).
2. Let the Longhorn backup poll cycle complete (default 5 minutes).
3. In the Longhorn UI under **Backup**, locate the backup volume and restore it
   as described in the [Restore runbook](#restore-runbook-same-cluster).

## Migration from nebari-longhorn-backup-pack

If you previously managed Longhorn backups via the
[nebari-longhorn-backup-pack](https://github.com/nebari-dev/nebari-longhorn-backup-pack)
Helm chart, migrate with a one-time cutover:

1. Uninstall the pack release:
   ```bash
   helm uninstall <release-name> -n longhorn-system
   ```
2. Add the `backups.longhorn` block to your NebariConfig and run `nic deploy`.

Removing the RecurringJobs does **not** delete already-taken snapshots or backups
in S3. NIC recreates resources with the same names (`default-hourly-snapshot`,
`default-daily-backup`), so coverage is seamless across the cutover.

## Teardown behaviour

`retain_on_destroy` defaults to `true` for both S3 and Azure targets. On
`nic destroy`:

- **AWS** — NIC removes the S3 bucket from Terraform state before the destroy
  run, so the bucket and its contents are orphaned and kept.
- **Azure** — NIC removes the storage account and container from Terraform state
  before destroy. The azurerm provider cannot force-delete a non-empty container,
  so the state-removal approach is the only reliable way to keep the data.

With `retain_on_destroy: false`, the bucket or storage account+container is
deleted with the cluster. This is a destructive, irreversible operation.

When you are truly done with a cluster's backups, delete the retained bucket or
storage account manually after verifying no data needs to be recovered.
