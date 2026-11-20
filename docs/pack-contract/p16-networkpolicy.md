---
promise: P16
level: experimental
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: 799670fa09c6b6a9
---
# P16: NetworkPolicy enforced

## Promise

A pack can write a standard NetworkPolicy and rely on it being enforced.

## Level

experimental

## State

planned. NetworkPolicy enforcement is not guaranteed on every target today. Tracked in NIC #691.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver. The interface is the standard `networking.k8s.io/v1` NetworkPolicy.

## Packs may rely on

Once built: NetworkPolicy is enforced on every target (#691). On existing clusters, install checks enforcement and refuses without it.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

- AWS: To be stated in the design for #691.
- Azure: To be stated in the design for #691.
- Hetzner: To be stated in the design for #691.
- local (kind): To be stated in the design for #691.
- existing: NIC does not control the CNI. Install will check enforcement and refuse without it.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A policy that denies traffic blocks it on every target.
