---
promise: P12
level: experimental
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: a0aa200494b51538
---
# P12: Logged-in identity passed to the app

## Promise

An app behind SSO receives the logged-in user's identity in documented request headers.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: an app behind SSO receives the logged-in user's identity in documented request headers, and the gateway strips those headers from client requests. This replaces decoding Envoy's `IdToken-*` cookie.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. The app sees the user's identity in the documented headers, and a client-supplied copy of those headers is stripped.
