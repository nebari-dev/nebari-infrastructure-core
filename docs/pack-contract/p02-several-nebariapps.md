---
promise: P2
level: stable
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: 91342168c7c591bc
---
# P2: Several NebariApps per pack

## Promise

A pack with several hostnames or several services conforms to the contract by creating one NebariApp for each.

## Level

stable

## State

planned. The operator spec still says a pack must create exactly one NebariApp. The rule is to be dropped in the spec, but that change is not made yet. javo, lgtm, data-engineering and artifact-keeper already create several. Tracking issue: to be filed when the contract is accepted.

## Interface

NebariApp, per the operator pack specification. The specification (version `0.1.0`) says "A pack MUST register with the platform by creating exactly one `NebariApp` resource". This promise needs that rule relaxed to allow one or more per pack, through a platform RFD and a Micro release of the specification.

## Packs may rely on

- Creating several NebariApps in one pack, for several hostnames or several services.
- Packs that consumed one NebariApp per pack keep working unchanged.

## Packs must not rely on

- A limit of one NebariApp per pack, in either direction.
- Any ordering between a pack's NebariApps.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack with two NebariApps gets two working routes on every target.
