# P11: API clients with bearer tokens through the gateway

## Promise

A NebariApp route can serve API clients that authenticate with bearer tokens.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: a NebariApp route accepts `Authorization: Bearer` tokens issued by the realm, returns 401 without one, and never redirects an API client to a browser login.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A request with a realm-issued bearer token succeeds, a request without one gets 401, and neither is redirected to a login page.
