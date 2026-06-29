# Design: Native Longhorn backups in NIC

**Issue:** https://github.com/nebari-dev/nebari-infrastructure-core/issues/431
**Date:** 2026-06-29
**Status:** Approved (brainstorming) — pending implementation plan

## Goal

Bring Longhorn backup functionality natively into `nebari-infrastructure-core` (NIC)
and deprecate the standalone
[`nebari-dev/nebari-longhorn-backup-pack`](https://github.com/nebari-dev/nebari-longhorn-backup-pack)
repo. Today backup scheduling lives in a separate Helm chart that NIC does not
deploy, and the chart explicitly punts on the S3 prerequisites (`BackupTarget` +
credentials). The result is split-brain: scheduling in one repo, the S3 target
configured by hand somewhere else, nothing tied to `nic deploy`.

After this change, Longhorn backups are a first-class, config-driven part of a NIC
deployment: the full stack (S3/azblob target, credential secret, RecurringJobs,
cluster Setting) is rendered and reconciled by NIC, driven from `config.yaml`.

## What the pack does today (the thing being ported)

The chart (`v0.3.0`) renders three resources into `longhorn-system`:

| Resource | Spec | Default |
|---|---|---|
| `RecurringJob/default-hourly-snapshot` | `task: snapshot`, `groups: [default]` | cron `0 * * * *`, retain `24`, concurrency `5` |
| `RecurringJob/default-daily-backup` | `task: backup`, `groups: [default]` | cron `0 3 * * *`, retain `30`, concurrency `3` |
| `Setting/allow-recurring-job-while-volume-detached` | cluster-wide Longhorn `Setting` | `true` |

Plus render-time validation guards (5-field cron valid; retention `> 0`).

**Coverage model:** the RecurringJobs target Longhorn's built-in `default`
recurring-job-group. Longhorn auto-labels every volume
`recurring-job-group.longhorn.io/default=enabled` when its StorageClass has no
`recurringJobSelector` parameter — i.e. the cluster's default `longhorn`
StorageClass. So installing this covers every default-SC volume with no per-volume
wiring.

**Why `allow-recurring-job-while-volume-detached=true` matters:** JupyterHub user
PVCs detach when servers idle out; with Longhorn's stock default (`false`) those
detached volumes are silently skipped at the cron tick. `true` makes Longhorn
briefly auto-attach to take the snapshot/backup.

The pack explicitly leaves these as unmanaged prerequisites — this is the gap NIC
must close:
- Longhorn installed in `longhorn-system`.
- `BackupTarget/default` with a valid `backupTargetURL` (`s3://bucket@region/`) and
  `credentialSecret`.

## Decisions (from brainstorming)

| Decision | Choice |
|---|---|
| Apply mechanism | **Hybrid:** credential Secret applied imperatively by NIC (client-go); RecurringJobs/Setting/BackupTarget shipped as a foundational ArgoCD Application rendered to the GitOps repo. |
| Config placement | New **top-level `backups:` key**. |
| Scope | Design covers the **entire issue**; implementation lands as a **single PR** (see below). |
| `AWS_CERT` (custom CA) | Config **references a pre-existing k8s Secret/ConfigMap**; NIC reads the PEM at deploy time and injects it as the `AWS_CERT` key in the credential Secret. |
| Migration | **Documented one-time cutover** (`helm uninstall` pack → `nic deploy`). No auto-adoption code. |
| Azure target | **Native `azblob://`** target (storage account + container via the Azure TF module). |
| Bucket teardown | **Config-controlled `retain_on_destroy`**, default `true` — a normal `nic destroy` must not wipe backups. |

## Existing code this builds on

- `pkg/storage/longhorn/` — Longhorn installed **imperatively** via Helm in
  `Install()` right after `tofu apply` (it is *not* an ArgoCD app). Backup config
  is co-located conceptually but applied through the GitOps path (see §4).
- `pkg/config/` — `NebariConfig` with nil-safe `Validate()` methods;
  `UnmarshalProviderConfig`; the `token_env` convention for env-sourced secrets
  (mirrored from `pkg/git`).
- `pkg/argocd/` — foundational apps in `templates/apps/` (e.g. `cert-manager.yaml`,
  `keycloak.yaml`); `templates/manifests/`; `TemplateData` + `NewTemplateData` +
  `WriteAllToGit` render manifests into the GitOps repo with conditional skips;
  `InstallFoundationalServices` creates secrets imperatively (e.g. keycloak) before
  `ApplyRootAppOfApps`.
- `pkg/providers/cluster/{aws,azure}/` — `templates/*.tf` + `tofu.go` (`TFVars` →
  `terraform.tfvars.json`); outputs read back via `tf.Output`. GCP/local/existing
  have no TF module suitable for bucket creation.
- Deploy orchestration in `pkg/nic/` calls `clusterProvider.Deploy()` →
  `longhorn.Install()` → `InstallFoundationalServices()` → GitOps bootstrap →
  ArgoCD sync.

## 1. Config schema — top-level `backups:`

```yaml
backups:
  longhorn:
    enabled: true
    s3:                                       # AWS-native or any S3-compatible
      bucket: my-nebari-backups
      region: us-east-1
      prefix: clusterA/                       # optional; isolates clusters sharing a bucket
      create_bucket: true                     # provision via provider TF module; ignored if endpoint set
      retain_on_destroy: true                 # default true — keep bucket+backups on `nic destroy`
      endpoint: ""                            # 3rd-party (MinIO/Wasabi/R2); when set, bucket never created
      virtual_hosted_style: false             # path-style (default) vs virtual-hosted
      access_key_id_env: LONGHORN_S3_ACCESS_KEY_ID
      secret_access_key_env: LONGHORN_S3_SECRET_ACCESS_KEY
      ca_cert:                                # optional; references a pre-existing k8s resource
        kind: secret                          # secret | configmap
        name: longhorn-s3-ca
        namespace: longhorn-system
        key: ca.crt
    azure:                                     # alternative target (Longhorn-native azblob://)
      container: nebari-backups
      storage_account: nebaribackups
      prefix: clusterA/
      create_container: true
      retain_on_destroy: true
      account_name_env: LONGHORN_AZBLOB_ACCOUNT_NAME
      account_key_env: LONGHORN_AZBLOB_ACCOUNT_KEY
    allow_recurring_job_while_volume_detached: true
    schedules:
      snapshot: { cron: "0 * * * *", retain: 24, concurrency: 5 }
      backup:   { cron: "0 3 * * *", retain: 30, concurrency: 3 }
```

Go structs follow the existing nil-safe pattern (`Enabled *bool`, `IsEnabled()`,
`Validate()`). Exactly one of `s3` / `azure` may be set when enabled. Secrets are
**only** env-var names or k8s references — never literal values — consistent with
the `token_env` convention. Credentials are sourced from environment variables,
never the config file.

The `schedules.*` and `allow_recurring_job_while_volume_detached` fields map 1:1
onto the pack's `values.yaml` (`snapshot.*`, `backup.*`,
`clusterSettings.allowRecurringJobWhileVolumeDetached`).

## 2. Validation (`nic validate`)

`LonghornBackupConfig.Validate()`, nil-safe (returns nil when disabled), wired into
`NebariConfig.Validate()`. Ports the pack's render-time guards so failures surface
at `nic validate`, not at sync time:

- Exactly one of `s3` / `azure` set.
- 5-field cron for both schedules. Port the pack's regex `^(\S+\s+){4}\S+$` for
  parity; `github.com/robfig/cron/v3` is a stronger alternative (decide in the
  plan).
- `retain > 0` and `concurrency > 0` for both schedules.
- `create_bucket` / `create_container` valid **only** on a provider with a TF
  module (**aws, azure**); clear error for gcp/local/existing pointing at the
  external-endpoint path.
- `s3.endpoint` set ⇒ `create_bucket` must be `false` (NIC never creates an
  external bucket).
- `azure` target with `create_container: true` requires the azure provider.

Env-var *presence* (the values behind `*_env`) is checked at deploy time, like the
git token, not at static validation.

## 3. Credential Secret — imperative (client-go)

NIC builds one Opaque Secret `longhorn-backup-credentials` in `longhorn-system`,
created in `InstallFoundationalServices` alongside the keycloak secrets and **before**
`ApplyRootAppOfApps`, so it exists before ArgoCD syncs the backup app. The
`BackupTarget` (applied by ArgoCD) references it by name.

Keys:
- **S3 target:** `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` (from the env vars);
  `AWS_ENDPOINTS` (if `endpoint` set); `VIRTUAL_HOSTED_STYLE` (if set); `AWS_CERT`
  (PEM read from the referenced Secret/ConfigMap, if `ca_cert` set).
- **Azure target:** `AZBLOB_ACCOUNT_NAME`, `AZBLOB_ACCOUNT_KEY`.

> **TO-VERIFY during implementation:** exact Azure `AZBLOB_*` secret key names and
> the `azblob://` `backupTargetURL` format, against the Longhorn
> [Set Backup Target](https://longhorn.io/docs/latest/snapshots-and-backups/backup-and-restore/set-backup-target/)
> docs. Do not hardcode unverified names.

Missing env vars fail fast with a clear error (mirrors `pkg/git` token handling).
The `ca_cert` reference requires the named Secret/ConfigMap to already exist in the
cluster at deploy time — documented as a prerequisite for self-hosted-TLS providers.

## 4. Foundational ArgoCD app + rendered manifests

- New `pkg/argocd/templates/apps/longhorn-backup.yaml` — an Application using
  `source.path` pointing at a rendered manifest dir in the GitOps repo (the
  app-of-apps `directory` pattern, **not** `source.chart`, since these are raw CRs).
  `project: foundational`, a `sync-wave` after Longhorn is up, `prune`/`selfHeal`,
  `CreateNamespace=false` (longhorn-system already exists). The credential Secret is
  *not* part of this app (created out-of-band in §3), so prune never touches it.
- New `pkg/argocd/templates/manifests/longhorn-backup/*.yaml`:
  `BackupTarget/default`, `RecurringJob/default-hourly-snapshot`,
  `RecurringJob/default-daily-backup`,
  `Setting/allow-recurring-job-while-volume-detached`. All parameterized via
  `TemplateData`.
- Extend `TemplateData` + `NewTemplateData` with backup fields: target URL, prefix,
  crons, retains, concurrencies, the detached-volume setting, and the credential
  Secret name.
- The whole app is skipped in `WriteAllToGit` when backups are disabled (the same
  conditional-skip pattern as MetalLB/Certificate templates).
- BackupTarget URL: `s3://<bucket>@<region>/<prefix>` for S3; the `azblob://` form is
  TO-VERIFY (see §3).

## 5. Terraform bucket/container provisioning + teardown

- **AWS:** add `aws_s3_bucket` (+ versioning + server-side encryption) to
  `pkg/providers/cluster/aws/templates/`, gated on a new backup TFVars set. Bucket
  name/region come from config — no output threading needed (NIC already knows the
  name).
- **Azure:** add a storage account + container to
  `pkg/providers/cluster/azure/templates/`, gated likewise; backs the native
  `azblob://` target.
- **Wiring:** `pkg/nic` deploy builds a provider-agnostic `BackupBucketSpec` from the
  top-level `backups` config and passes it through `cluster.DeployOptions`; each
  provider's `toTFVars` consumes it. This keeps the top-level backups config out of
  `ClusterConfig` while still reaching the provider.
- **`retain_on_destroy`** (default `true`): Terraform cannot take a variable in
  `lifecycle.prevent_destroy`, so the exact mechanism (likely `force_destroy=false`
  so a non-empty backup bucket blocks deletion, or excluding the bucket from the
  destroy set) is settled in the plan. The spec fixes the **intent**: a normal
  `nic destroy` must not wipe backups, matching the pack's "bucket is the source of
  truth, cluster is replaceable" DR model.

## 6. Deploy orchestration (sequence)

1. `nic validate` — now includes backup validation (§2).
2. `clusterProvider.Deploy()` → `tofu apply` creates the cluster **and** the backup
   bucket/container (gated on `create_bucket`/`create_container`).
3. `longhorn.Install()` — unchanged.
4. `InstallFoundationalServices()` → **new** `createLonghornBackupSecret()` before
   `ApplyRootAppOfApps()`.
5. GitOps bootstrap / `WriteAllToGit` renders the `longhorn-backup` app + manifests.
6. ArgoCD syncs the backup app; `BackupTarget` binds using the out-of-band Secret.

## 7. Third-party S3 support

Longhorn talks S3 to anything S3-compatible (MinIO, Wasabi, Backblaze B2, Cloudflare
R2, Ceph RGW, DigitalOcean Spaces). Mapping:
- `s3.endpoint` → `AWS_ENDPOINTS` (the single most important field for 3rd-party).
- `s3.virtual_hosted_style` → `VIRTUAL_HOSTED_STYLE`.
- `s3.ca_cert` → `AWS_CERT` (PEM from the referenced k8s resource).
- credentials → `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`.

When `endpoint` is set the bucket is assumed to exist and is never created. This is
also the only path available for GCP/local/existing providers (no TF module).

## 8. Azure (native azblob)

Azure Blob doesn't natively speak S3, so Azure clusters use Longhorn's native
`azblob://` target backed by a TF-created storage account + container. The `azure:`
config block is the Azure analogue of `s3:` (see §1), and its secret keys/URL are
TO-VERIFY (§3).

## 9. Testing

Table-driven unit tests (preferred pattern in this repo):
- Config parse + validate: valid/invalid crons, retention, concurrency,
  mutually-exclusive targets, provider gating, `endpoint`-vs-`create_bucket`.
- Credential-secret key assembly per target (S3 with/without endpoint, CA cert;
  Azure).
- BackupTarget URL construction (S3 and azblob).
- `TFVars` gating for bucket/container creation.

Deferred PR: a MinIO round-trip integration test (backup + restore) under
`-tags=integration` (MinIO is cheapest to stand up).

## 10. Docs migration + 11. repo deprecation

- Migrate the pack's restore runbook and design docs
  (`docs/2026-05-04-longhorn-backup-*.md`) into NIC `docs/`.
- Document the one-time cutover: `helm uninstall` the pack release, then
  `nic deploy`. Safe because removing RecurringJobs does **not** delete existing
  snapshots/backups in S3, and NIC recreates same-named resources.
- Archive `nebari-longhorn-backup-pack` and point its README at NIC.

## Out of scope

- **Restore orchestration.** Stays a Longhorn operation (UI/CLI). NIC schedules and
  configures; it does not orchestrate restores. The runbook is migrated to NIC docs.
- **Installing Longhorn itself.** Assumed present in `longhorn-system` (already
  handled by `longhorn.Install()`).
- **Per-workload backup group targeting / dedicated paired StorageClasses.**
  Intentionally cluster-default-scoped, as in the pack.

## Implementation: single PR

All NIC changes land in one PR, organized as logical commits roughly following the
order below. Each commit should keep the tree building and tests green.

1. Config schema + validation (§1, §2).
2. Credential Secret, imperative (§3).
3. ArgoCD app + rendered manifests + `TemplateData` wiring (§4).
4. Terraform bucket/container provisioning + `retain_on_destroy`, AWS + Azure
   (§5), plus the `BackupBucketSpec`-through-`DeployOptions` wiring (§6).
5. Third-party S3 + Azure azblob coverage (§7, §8).
6. Unit tests throughout; MinIO round-trip integration test under
   `-tags=integration` (§9).
7. Docs migration into NIC `docs/` (§10).

**Necessarily out-of-band (cannot live in this NIC PR):** archiving the
`nebari-longhorn-backup-pack` repo (a GitHub repo setting) and updating its README
to point at NIC (a commit in that separate repo). Tracked as a follow-up action
once the NIC PR merges.

## Open items to settle in the plan (not blockers)

- Exact `azblob://` URL format + `AZBLOB_*` secret key names (verify vs Longhorn
  docs).
- Cron validation: ported regex vs `robfig/cron/v3`.
- `retain_on_destroy` Terraform mechanism (`force_destroy=false` vs destroy-set
  exclusion).
