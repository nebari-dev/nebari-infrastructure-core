# P4: Postgres through CloudNativePG

## Promise

A pack gets Postgres by creating CloudNativePG resources, instead of bundling its own Postgres. About twelve packs bundle their own today.

## Level

stable

## State

built. NIC deploys the CloudNativePG operator on every cluster (the `cloudnative-pg` chart, pinned at 0.29.0 today) and llm-serving-pack and unity-catalog-pack already use it. Until this page states a supported operator version range, the supported version is the one NIC installs. Tracking issue: to be filed when the contract is accepted.

## Interface

The `postgresql.cnpg.io/v1` API. Packs create `Cluster` resources, and `Database` and `Pooler` where needed, in their own namespace. Storage for a pack's Cluster comes from P3.

## Packs may rely on

- Creating `postgresql.cnpg.io/v1` resources in the pack's own namespace.
- The `postgresql.cnpg.io/v1` API as served by the CNPG operator NIC installs. NIC upgrades the operator only to versions that keep that API compatible for packs, and this page states the supported range before 1.0.
- Cluster storage from the default StorageClass (P3).

## Packs must not rely on

- CNPG backups to object storage. They depend on buckets, which the contract does not create yet.
- The operator's namespace.
- Any NebariApp `database` field. ADR-0007 proposed one; the contract offers the CNPG API directly instead.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack creates a CNPG Cluster and connects to the database on every target.
