# ADR-0005: Velero Backup for JupyterHub Singleuser PVCs

## Status

Proposed

## Date

2026-05-01

## Context

JupyterHub deployments managed by NIC dynamically provision a PersistentVolumeClaim per user (`claim-{username}`) for their home directory. These PVCs are the primary store of user-generated work — notebooks, data files, configuration. If a PVC is lost (node failure, accidental deletion, storage backend issue), the user's data is gone. There is currently no backup or recovery mechanism.

The underlying storage (e.g., Hetzner Cloud Volumes, AWS EBS) uses `ReadWriteOnce` with a `Delete` reclaim policy, meaning deleting the PVC destroys the backing volume. Users have no self-service way to recover data.

Two properties of JupyterHub make backup non-trivial:

1. **User servers are ephemeral.** JupyterHub culls idle servers, so at any given time most user PVCs are not mounted by any pod.
2. **Velero's file-system backup requires a running pod.** Velero's node-agent (Kopia) reads files from the host-level kubelet mount path. If no pod mounts the volume, there are no files to read. This is not a limitation that can be configured around — it is architectural to how Velero's file-system backup works.

A separate but related constraint affects restore: Velero populates restored volumes by injecting an init container into pods it creates during the restore process. If the pod is created by something other than Velero (e.g., JupyterHub's KubeSpawner), no init container is injected and the volume remains empty.

## Decision Drivers

- **Data durability.** Admins must be able to recover user data after storage failure or accidental deletion.
- **Works with idle servers.** Backups must capture PVCs even when no user server is running.
- **Restore must actually restore data.** Not just PVC metadata — the file contents must be written back to the volume.
- **Cloud-agnostic.** The solution should work across providers (Hetzner, AWS, GCP, bare-metal) with minimal provider-specific configuration.
- **Operationally simple.** Should run unattended; ad-hoc backup/restore should be a single command.
- **No dependency on CSI snapshot support.** Not all storage backends or cluster configurations support `VolumeSnapshot`. The solution must work with basic block storage.

## Considered Options

1. **CSI volume snapshots**: Use Kubernetes `VolumeSnapshot` resources to snapshot the underlying block device directly.
2. **Velero file-system backup with helper pods** (this proposal): Use Velero with Kopia, creating temporary pods to mount PVCs at backup time.
3. **Direct rsync/rclone CronJob**: A script that mounts each PVC and copies files to object storage without Velero.

## Decision Outcome

Chosen option: **Option 2, Velero file-system backup with helper pods**, because it works across all storage backends without requiring CSI snapshot support, provides a standard restore workflow, and integrates with the broader Kubernetes backup ecosystem.

### Consequences

**Good:**

- Works on any storage backend that supports `ReadWriteOnce` — no CSI snapshot controller, `VolumeSnapshotClass`, or provider-specific snapshot plugin required.
- Velero is a well-established CNCF project with broad community support and documentation.
- Kopia provides deduplication and compression, reducing storage costs for incremental backups.
- Backup storage is S3-compatible, so any object storage backend works (AWS S3, MinIO, Wasabi, Backblaze B2, etc.).
- Ad-hoc backup is a single command: `kubectl create job --from=cronjob/velero-jhub-backup <name> -n velero`.
- The helper-pod pattern is transparent to JupyterHub — no changes to the hub, spawner, or user images.

**Bad:**

- Helper pods add operational complexity: a CronJob must create, monitor, and clean up pods per PVC.
- Restore requires a two-step process: restore the helper pods (to trigger data download), then clean them up before the user starts their server.
- File-system backup is slower than block-level snapshots for large volumes, since Kopia must read every file.
- If the helper-pod CronJob fails silently (e.g., image pull error, RBAC misconfiguration), backups stop without obvious alerting unless monitoring is configured.
- MinIO (or equivalent) adds another stateful service to manage. In production, backup storage should be external (cloud S3) rather than in-cluster.
- RWO constraint means the helper pod must land on the same node as any running user server for that PVC. Kubernetes scheduling handles this automatically, but it can cause backup delays if the node is under pressure.

## Options Detail

### Option 1: CSI Volume Snapshots

Use the Kubernetes `VolumeSnapshot` API to snapshot the underlying block device. Snapshots are taken at the storage layer — no pod needs to be mounting the volume.

**Pros:**

- No helper pods needed; snapshots work on unmounted volumes.
- Fast: block-level copy-on-write, not file-by-file.
- Native Kubernetes API; Velero can orchestrate snapshots via its CSI plugin.
- Restore creates a new PVC pre-populated from the snapshot — no init container injection needed.

**Cons:**

- Requires the CSI driver to support `VolumeSnapshot` and a `VolumeSnapshotClass` to be configured.
- Requires the snapshot controller to be installed (not included in all distributions; k3s does not ship it by default).
- Provider-specific: each storage backend needs its own snapshot class and potentially a Velero snapshot plugin.
- Snapshot storage is provider-managed and may have different cost/retention characteristics than object storage.
- Not all environments support it (bare-metal with local-path provisioner, some managed Kubernetes offerings).

### Option 2: Velero File-System Backup with Helper Pods

Deploy Velero with Kopia (file-system backup engine) and an S3-compatible backend. A CronJob creates lightweight `pause` pods that mount each user PVC, triggers a Velero backup targeting those pods, waits for completion, then cleans up.

**Architecture:**

```
CronJob (velero-jhub-backup, daily 03:00 UTC)
  ├── Lists all claim-* PVCs in jupyterhub namespace
  ├── Creates a pause pod per PVC (mounts volume read-only)
  ├── Waits for pods to be Ready
  ├── Creates a Velero Backup CR targeting helper pods
  ├── Velero node-agent reads files via Kopia → uploads to S3
  └── Cleans up helper pods

Restore:
  ├── Velero recreates PVC (dynamic provisioning)
  ├── Velero creates helper pod with injected init container
  ├── Node-agent downloads Kopia snapshot → writes to new volume
  ├── Admin cleans up helper pod
  └── User starts server — data is present
```

**Components:**

| Component | Purpose |
|-----------|---------|
| MinIO (or external S3) | S3-compatible backup storage |
| Velero + AWS plugin | Backup orchestration, file-system backup via Kopia |
| Node-agent DaemonSet | One pod per node, reads/writes volume data |
| CronJob + RBAC | Creates helper pods, triggers backups, cleans up |

**Backup procedure:**

1. CronJob lists `claim-*` PVCs in the JupyterHub namespace.
2. For each PVC, creates a pod using `registry.k8s.io/pause:3.9` with minimal resource requests (1m CPU, 4Mi memory), mounting the PVC read-only.
3. Waits for all helper pods to be Ready (300s timeout).
4. Creates a Velero `Backup` CR with `labelSelector` targeting helper pods and `defaultVolumesToFsBackup: true`.
5. Polls until the backup reaches a terminal phase.
6. Deletes all helper pods (via `trap cleanup EXIT` for reliability).

**Restore procedure:**

1. Ensure the user's server is stopped.
2. Create a Velero `Restore` CR from the backup, including `pods`, `persistentvolumeclaims`, and `persistentvolumes`, with `labelSelector` matching `app.kubernetes.io/component: velero-backup-helper`.
3. Velero recreates the PVC and the helper pod. The helper pod gets an init container injected by Velero that blocks until the node-agent finishes writing data.
4. Wait for `PodVolumeRestore` resources to show `Completed`.
5. Delete helper pods.
6. User starts their server.

**Pros:**

- Works on any storage backend; no CSI snapshot support required.
- Kopia provides deduplication and compression.
- S3 backend is cloud-agnostic and cheap.
- Standard Velero workflow; documented restore procedure.
- Helper pods use `pause` image (minimal footprint, ~500KB, no shell, no attack surface).

**Cons:**

- Extra moving parts: CronJob, helper pods, cleanup logic.
- Restore is two-step (restore helper pods for data download, then clean up).
- Slower than block-level snapshots for large volumes.
- Requires monitoring to detect silent CronJob failures.

### Option 3: Direct rsync/rclone CronJob

A custom CronJob that mounts each user PVC and copies files to object storage using rsync or rclone, bypassing Velero entirely.

**Pros:**

- Simple to understand; no Velero dependency.
- Full control over backup format and storage layout.
- Restore is a straightforward file copy.

**Cons:**

- Reinvents backup orchestration: scheduling, retention, incremental backups, error handling, reporting.
- No deduplication or compression without additional tooling.
- No integration with Kubernetes resource backup (PVC metadata, labels, annotations).
- Still requires helper pods for the same reason (unmounted volumes).
- No ecosystem support; custom restore tooling needed.
- Maintenance burden falls entirely on the team.

## Open Questions

1. **Backup storage for production.** The POC uses in-cluster MinIO. Production deployments should use external S3 (or equivalent) to avoid losing backups if the cluster is lost. Should NIC configure this per-provider (e.g., auto-create an S3 bucket for AWS clusters)?
2. **CSI snapshots as an upgrade path.** On providers that support `VolumeSnapshot` (AWS EBS, GCP PD, Hetzner CSI), CSI snapshots are faster and simpler. Should NIC offer both strategies and select based on provider capabilities?
3. **Alerting on backup failure.** The CronJob can fail silently. Should we require a Prometheus alert rule or webhook notification as part of the deployment?
4. **Shared storage PVCs.** The current implementation only targets `claim-*` PVCs (per-user). If `sharedStorage` is enabled, that PVC also needs to be included.
5. **Backup frequency and retention.** The POC uses daily backups with 30-day retention. Should this be configurable via NIC's config, or left to the operator?
6. **Credential management.** The POC has MinIO credentials inline in Helm values. Production should use Kubernetes Secrets (potentially managed by the provider plugin or external secret operator).
7. **Restore automation.** The current restore is a manual kubectl procedure. Should NIC provide a `nic restore user-data` command that automates the helper-pod restore workflow?

## Links

- [Velero Documentation](https://velero.io/docs/)
- [Velero File System Backup](https://velero.io/docs/v1.18/file-system-backup/)
- [nebari-dev/nebari-data-science-pack#49](https://github.com/nebari-dev/nebari-data-science-pack/issues/49)
- [POC Implementation (NIC-argocd-tyler-dev PR#1)](https://github.com/openteams-ai/NIC-argocd-tyler-dev/pull/1)
