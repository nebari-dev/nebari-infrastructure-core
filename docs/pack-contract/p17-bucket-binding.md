---
promise: P17
level: experimental
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: 24bff669b6868b6f
---
# P17: Bucket binding

## Promise

A pack receives object storage through one binding and never needs to know which backend a target uses. mlflow, lgtm, unity-catalog, harbor, langfuse and data-engineering need it.

## Level

experimental

## State

planned. Nothing is built. Automatic bucket creation (`NebariBucket`, #660) is not part of this contract version. Tracking issue: to be filed when the contract is accepted.

## Interface

The intended interface, which may change before it is built. A pack names a binding in its values. The binding is:

- a ConfigMap with `protocol` (`s3` or `azure-blob`; `gcs` reserved), `endpoint`, `bucket` (or container), `region` where the protocol has one, and `prefix`;
- the ServiceAccount the pack runs as, carrying workload identity where the target has it;
- a Secret with standard keys for targets without workload identity (`access-key-id` and `secret-access-key` for `s3`; the equivalent for `azure-blob` is fixed in this reference later).

| Target | Backend | Credentials |
|---|---|---|
| AWS | S3 | workload identity (Pod Identity) |
| Azure | Blob Storage | workload identity |
| Hetzner | Hetzner Object Storage (S3 API) | Secret |
| local | in-cluster S3-compatible server | Secret |
| existing | supplied by the admin | either |

A pack that supports only `s3` declares that, and does not install on a target whose binding uses another protocol.

## Packs may rely on

Once built:

- The binding's ConfigMap keys, ServiceAccount and Secret keys as above.
- Until bucket creation is in the contract, the admin creates the bucket, its access and the binding. This reference will give the steps per target.
- A future `NebariBucket` (#660) fills in the same binding, so packs do not change.

## Packs must not rely on

- Creating buckets themselves.
- A particular credential mechanism.
- Anything outside the binding.

## Per-target notes

- AWS: S3 with workload identity (Pod Identity). The admin creates the bucket.
- Azure: Blob Storage with workload identity. The admin creates the container.
- Hetzner: Hetzner Object Storage (S3 API) with a Secret. The admin creates the bucket.
- local (kind): NIC will install an in-cluster S3-compatible server. The admin creates buckets in it the same way, and credentials come from a Secret.
- existing: Backend supplied by the admin, with either workload identity or a Secret.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack reads the binding and writes and reads an object on every target.
