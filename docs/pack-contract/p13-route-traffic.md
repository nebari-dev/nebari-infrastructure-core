# P13: Per-route timeouts, streaming and buffer size

## Promise

A pack can set request timeout, streaming and request buffer size for its routes without editing the shared gateway.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: request timeout, streaming and request buffer size per NebariApp route, without editing the shared gateway.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A route with a long timeout, a streaming response and a large request body works on every target without a gateway edit.
