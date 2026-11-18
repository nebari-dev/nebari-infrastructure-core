---
promise: P14
level: experimental
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: bbacd0148c42ae63
---
# P14: Generated secrets stable under Argo CD

## Promise

A pack can declare a secret with generated values that is created once and is not regenerated on resync.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: a pack declares a secret with generated values that is created once and survives Argo CD resyncs. This replaces Helm `lookup` (which does not run under Argo CD) and PreSync generator Jobs.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A generated secret keeps its values across repeated Argo CD syncs.
