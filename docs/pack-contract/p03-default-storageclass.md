# P3: Default storage class

## Promise

Every target has exactly one default StorageClass that gives a pack ReadWriteOnce storage without naming a class.

## Level

stable

## State

partly built. Every NIC-managed target has a default StorageClass and the behavior exists, but it is undocumented and the Azure default relies on AKS marking its own class as default instead of NIC setting the annotation. Tracking issue: to be filed when the contract is accepted.

## Interface

The cluster default StorageClass. Every target has exactly one. It provides ReadWriteOnce volumes, a PVC that omits `storageClassName` binds, a pod using it schedules, and data persists across pod restarts. Packs request storage by omitting `storageClassName`.

## Packs may rely on

- Omitting `storageClassName` to get ReadWriteOnce storage.
- A PVC binding and the pod scheduling.
- Data persisting across pod restarts.

## Packs must not rely on

- The class name. It differs on every target today.
- The provisioner.
- The volume binding mode.
- ReadWriteMany.
- Volume expansion.
- Snapshots.
- A volume following a pod to another node or zone. A pod may be pinned to the node or zone its volume lives on (kind and k3d `local-path` volumes are node-local; EBS volumes are zonal).

## Per-target notes

- AWS: `gp3` (EBS) by default. If Longhorn is opted in and made the default class, the default is `longhorn`. Volumes are zonal.
- Azure: `managed-csi`. NIC relies on AKS marking its own class as default today; NIC will set the annotation itself, as an in-place migration with no volume changes.
- Hetzner: `longhorn`.
- local (kind): `standard`. Volumes are node-local.
- existing: NIC does not control storage. The class NIC uses for its own workloads defaults to `standard`, which is a NIC setting and not a promise about the cluster. Install will check that the cluster has exactly one default StorageClass and refuse with a message naming the requirement if not.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A PVC with no `storageClassName` binds, a pod using it schedules, and data survives a pod restart. The test checks behavior, not the class name.
