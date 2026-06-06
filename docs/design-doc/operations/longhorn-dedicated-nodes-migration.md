# Migrating Longhorn `dedicated_nodes` modes

This document describes how to switch an existing cluster between colocated
Longhorn (`longhorn.dedicated_nodes: false`, replicas on every node) and
dedicated storage nodes (`dedicated_nodes: true`, replicas confined to a tainted
storage node group), and how to upgrade a cluster from the pre-fix behavior.

> **Switching modes is a manual migration, not a config toggle.** The
> `dedicated_nodes` setting only governs *future* default-disk creation. Neither
> NIC nor Longhorn moves or re-syncs replicas that already exist when the flag
> changes — you migrate them yourself. See the `Config.DedicatedNodes` godoc in
> `pkg/storage/longhorn/config.go` for the short version.

## Background

- `dedicated_nodes: false` renders `createDefaultDiskLabeledNodes` unset, so
  Longhorn creates a default disk on **every** node — replicas spread across all
  nodes.
- `dedicated_nodes: true` renders `createDefaultDiskLabeledNodes: true`, so
  Longhorn creates a default disk **only** on nodes labeled
  `node.longhorn.io/create-default-disk=true` (the AWS provider injects this onto
  the storage node group). Replicas can only land where a disk exists, so they are
  confined to the storage nodes. The system pods (csi-plugin, manager, engine-role
  instance-manager) are **not** pinned — they run everywhere so workloads on any
  node can mount volumes.

Changing the flag does not delete existing disks or rebuild existing replicas.
That is why every transition below has an explicit replica-migration step.

## Pre-flight (all transitions)

Take a fresh **off-cluster** backup before touching topology. In-cluster
snapshots live on the same disks you may be about to remove, so they do not
protect you — only an S3 backup does.

- Install / confirm [`nebari-longhorn-backup-pack`](https://github.com/nebari-dev/nebari-longhorn-backup-pack)
  and a configured `BackupTarget/default` (S3 bucket + credential secret).
- Trigger an **on-demand backup of every volume** right before the switch (the
  scheduled job is daily — don't rely on a backup up to 24h old).
- Restore path if anything goes wrong: Longhorn UI → Backup → restore each volume
  onto the new topology.

## Case 1: colocated → dedicated (`false` → `true`), with live data

This is the careful one — you have real replicas on the general/user node disks.

1. **Back up** (pre-flight above).
2. **Add the storage node group** and deploy. The group must carry the storage
   label, the disk-creation label, and the matching taint:
   ```yaml
   node_groups:
     storage:
       instance: m7g.large
       ami_type: AL2023_ARM_64_STANDARD
       min_nodes: 2
       max_nodes: 2
       disk_size: 500
       labels:
         node.longhorn.io/storage: "true"
         node.longhorn.io/create-default-disk: "true"   # NIC also injects this
       taints:
         - key: node.longhorn.io/storage
           value: "true"
           effect: NO_SCHEDULE
   ```
   `nic deploy`. New storage nodes come up with Longhorn disks; existing replicas
   stay where they are.
3. **Flip the flag**: set `longhorn.dedicated_nodes: true`, `nic deploy`. This
   sets `createDefaultDiskLabeledNodes: true` and un-pins the system pods. Existing
   general/user-node disks are **not** removed, so replicas still live there — the
   cluster is now in a hybrid state.
4. **Migrate replicas onto the storage nodes.** For each non-storage node, request
   eviction so Longhorn rebuilds its replicas elsewhere (only the storage nodes
   have schedulable disks, so that's where they go):
   ```bash
   for n in $(kubectl get nodes -l '!node.longhorn.io/storage' -o name | sed 's|node/||'); do
     kubectl -n longhorn-system patch nodes.longhorn.io "$n" \
       --type=merge -p '{"spec":{"allowScheduling":false,"evictionRequested":true}}'
   done
   ```
   Wait until every volume is `Healthy` with replicas only on storage nodes:
   ```bash
   kubectl -n longhorn-system get replicas.longhorn.io -o wide   # nodeID column all storage nodes
   kubectl -n longhorn-system get volumes.longhorn.io -o custom-columns=NAME:.metadata.name,ROBUST:.status.robustness
   ```
5. The general/user nodes now hold no replicas, so they are safe to scale down and
   their default disks can be removed (clear `evictionRequested` once drained, or
   let the node group scale in).

## Case 2: upgrading a `main` `dedicated_nodes: true` cluster to this topology

On `main`, `dedicated_nodes: true` never actually worked (#369: storage nodes
never got the disk label → zero disks → every volume faulted). So there is no
data to preserve — this is a straight upgrade.

1. `nic deploy` off the new build. The system-pod pinning is removed and the AWS
   provider injects `node.longhorn.io/create-default-disk=true` onto the storage
   node group.
2. **Verify the label reached the *running* storage nodes.** An EKS managed
   node-group label change does not reliably relabel already-running instances:
   ```bash
   kubectl get nodes -l node.longhorn.io/storage=true \
     -L node.longhorn.io/create-default-disk
   ```
   If the `CREATE-DEFAULT-DISK` column is blank, either recycle the storage nodes
   or apply the label directly:
   ```bash
   for n in $(kubectl get nodes -l node.longhorn.io/storage=true -o name | sed 's|node/||'); do
     kubectl label node "$n" node.longhorn.io/create-default-disk=true --overwrite
   done
   ```
   Disks appear within ~30s. The install-time guard (`warnIfMissingStorageDiskLabel`)
   also emits a warning during deploy if no node carries the label.
3. Un-pinning applies cleanly (a broken cluster has no attached volumes to block
   the setting change).

## Case 3: dedicated → colocated (`true` → `false`)

**Danger:** flipping the flag *and* removing the storage node group in the same
step tears down the nodes holding the only replicas before they are rebuilt
elsewhere — straight data loss (the #354 node-removal hazard).

1. **Back up** (pre-flight).
2. Set `dedicated_nodes: false` but **keep the storage node group for now**,
   `nic deploy`. `createDefaultDiskLabeledNodes` goes off, so general/user nodes
   start getting default disks. Existing replicas stay on the storage nodes.
3. **Migrate replicas off the storage nodes** onto the now-disk-bearing
   general/user nodes (same eviction pattern as Case 1, step 4, but evicting the
   *storage* nodes). Wait for every volume `Healthy` with replicas on non-storage
   nodes.
4. Only **after** replicas have rebuilt elsewhere, remove the storage node group
   from the config and `nic deploy`.

## Caveats

- **csi-plugin on tainted workload pools (GPU).** `taintToleration` covers the
  storage taint only, and the csi-plugin DaemonSet (system-managed) inherits its
  tolerations from that setting — not from the tolerate-all on manager/driver. A
  tainted GPU pool (`nvidia.com/gpu:NoSchedule`) therefore won't run csi-plugin and
  its pods can't mount Longhorn volumes until that taint is added to
  `taintToleration`. Tracked in #363 / #368; Refs #366.
- **Exact-value label match.** NIC identifies the storage group by an exact
  key/value match against `NodeSelector` (default `node.longhorn.io/storage=true`).
  A label with any other value silently won't match and gets no disk label.

## Verification (after any transition to dedicated)

```bash
# csi-plugin + manager on EVERY node (mounts work cluster-wide)
kubectl -n longhorn-system get pods -l app=longhorn-csi-plugin -o wide
kubectl -n longhorn-system get ds longhorn-manager -o wide

# disks present and schedulable on storage nodes
kubectl -n longhorn-system get nodes.longhorn.io <storage-node> -o jsonpath='{.status.diskStatus}'

# replicas confined to storage nodes
kubectl -n longhorn-system get replicas.longhorn.io -o wide

# end-to-end: a PVC + pod on a workload (user/general) node mounts and runs
```
