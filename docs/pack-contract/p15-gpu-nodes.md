---
promise: P15
level: experimental
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: bc6eec066d3ff498
---
# P15: GPU node label and taint

## Promise

One node selector and one toleration select GPU nodes on every target that offers them.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: every target that offers GPU nodes labels and taints them the same way, so one selector and one toleration work everywhere. Targets without GPU nodes say so in this reference. Keeping the AWS taint `nvidia.com/gpu=true:NoSchedule` as the contract taint avoids a breaking change for data-science-pack, llm-serving-pack and rayserve-pack.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

- AWS: GPU node groups carry an `nvidia.com/gpu` taint today (`pkg/providers/cluster/aws/tofu.go`). The contract label is not yet defined.
- Azure: Not confirmed. To be stated when this promise is designed.
- Hetzner: Not confirmed. To be stated when this promise is designed.
- local (kind): Not confirmed. To be stated when this promise is designed.
- existing: Not confirmed. To be stated when this promise is designed.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A pod with the contract selector and toleration schedules on a GPU node on every target that offers GPU nodes.
