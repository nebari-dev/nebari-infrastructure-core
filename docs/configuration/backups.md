# Backups Configuration

Off-cluster backup scheduling for Longhorn volumes (S3/Azure targets, retention, keyless auth).

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [BackupsConfig](#backupsconfig)
- [LonghornBackupConfig](#longhornbackupconfig)
- [S3BackupTarget](#s3backuptarget)
- [AzureBackupTarget](#azurebackuptarget)
- [CACertRef](#cacertref)
- [BackupSchedules](#backupschedules)
- [ScheduleConfig](#scheduleconfig)

---

## BackupsConfig

BackupsConfig is the top-level `backups:` block. Today it only carries
Longhorn backup configuration, but the block exists to group future backup
concerns under one key.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Longhorn | `longhorn` | `*LonghornBackupConfig` | No |  |

---

## LonghornBackupConfig

LonghornBackupConfig drives the Longhorn snapshot/backup schedules, the
cluster Setting, and the S3/azblob BackupTarget + credential Secret. Exactly
one of S3 / Azure may be set when Enabled.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Enabled | `enabled` | `*bool` | No |  |
| S3 | `s3` | `*S3BackupTarget` | No |  |
| Azure | `azure` | `*AzureBackupTarget` | No |  |
| AllowRecurringJobWhileVolumeDetached | `allow_recurring_job_while_volume_detached` | `*bool` | No | AllowRecurringJobWhileVolumeDetached maps to the cluster-wide Longhorn Setting. nil defaults to true (the pack's behaviour): JupyterHub user PVCs detach when servers idle out, and Longhorn's stock ... |
| Schedules | `schedules` | BackupSchedules | No |  |

---

## S3BackupTarget

S3BackupTarget configures an AWS-native or S3-compatible backup target.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Bucket | `bucket` | string | Yes |  |
| Region | `region` | string | Yes |  |
| Prefix | `prefix` | string | No |  |
| CreateBucket | `create_bucket` | bool | No |  |
| RetainOnDestroy | `retain_on_destroy` | `*bool` | No |  |
| Endpoint | `endpoint` | string | No |  |
| VirtualHostedStyle | `virtual_hosted_style` | bool | No |  |
| AccessKeyIDEnv | `access_key_id_env` | string | Yes |  |
| SecretAccessKeyEnv | `secret_access_key_env` | string | Yes |  |
| CACert | `ca_cert` | `*CACertRef` | No |  |

---

## AzureBackupTarget

AzureBackupTarget configures a Longhorn-native azblob:// backup target.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Container | `container` | string | Yes |  |
| StorageAccount | `storage_account` | string | Yes |  |
| Prefix | `prefix` | string | No |  |
| CreateContainer | `create_container` | bool | No |  |
| RetainOnDestroy | `retain_on_destroy` | `*bool` | No |  |
| Endpoint | `endpoint` | string | No |  |
| AccountNameEnv | `account_name_env` | string | Yes |  |
| AccountKeyEnv | `account_key_env` | string | Yes |  |

---

## CACertRef

CACertRef references a pre-existing Secret or ConfigMap key holding a PEM CA
bundle. NIC reads it at deploy time and injects it as the AWS_CERT key.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Kind | `kind` | string | Yes | "secret" \| "configmap" |
| Name | `name` | string | Yes |  |
| Namespace | `namespace` | string | No |  |
| Key | `key` | string | Yes |  |

---

## BackupSchedules

BackupSchedules holds the two RecurringJob schedules.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Snapshot | `snapshot` | ScheduleConfig | Yes |  |
| Backup | `backup` | ScheduleConfig | Yes |  |

---

## ScheduleConfig

ScheduleConfig is one RecurringJob's cron/retain/concurrency.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Cron | `cron` | string | Yes |  |
| Retain | `retain` | int | Yes |  |
| Concurrency | `concurrency` | int | Yes |  |

