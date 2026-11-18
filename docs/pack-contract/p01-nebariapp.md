# P1: Routing, TLS, SSO and landing tile through NebariApp

## Promise

A pack gets a hostname, TLS, optional SSO and a landing tile by creating a NebariApp in a namespace labelled `nebari.dev/managed=true`.

## Level

stable

## State

partly built. NebariApp exists and is specified in the operator spec (`nebari-operator/docs/pack-specification.md`). Two problems are to be fixed before contract 1.0: `gateway: internal` resolves to a Gateway named `nebari-internal-gateway` that NIC never creates, and the operator publishes a route before auth succeeds, so an auth error leaves the route unprotected unless groups are set (nebari-operator #152). Tracking issue: to be filed when the contract is accepted.

## Interface

A `reconcilers.nebari.dev/v1` NebariApp resource, as defined by the operator spec. The operator spec stays the source of truth for NebariApp fields, conditions and naming, including the `nebari.dev/managed=true` namespace label. This reference pins the specification version it includes (see [Versioning and support](policy/versioning.md)).

## Packs may rely on

- Registering a pack with the platform through a NebariApp for routing, TLS, SSO and the landing tile.
- The NebariApp fields, conditions and naming in the pinned operator spec version.

## Packs must not rely on

- `gateway: internal`. Today every NebariApp with this value fails with `GatewayNotFound`. The value may be removed from the enum or NIC may create the Gateway.
- A route being protected when an auth error occurs, unless groups are set, until nebari-operator #152 is fixed.
- The shared Gateway's name or namespace (see do-not-rely-on).

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack's NebariApp is routed over TLS, redirects an unauthenticated browser to login and appears as a landing tile on every target.
