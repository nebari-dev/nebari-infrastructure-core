# P6: Groups claim as full paths

## Promise

A pack authorizes by group path and gets the same claim on every cluster, without writing the mapper itself.

## Level

stable

## State

partly built. Full-path groups claims exist today, but they come from data-science-pack, not NIC, and the realm setup job skips an existing mapper instead of reconciling it (#463). The operator's own groups mappers do not yet all use full paths (#690). NIC will own the setting and reconcile the existing mapper to `full.path=true`.

## Interface

The `groups` claim in ID tokens, access tokens and userinfo, containing full group paths such as `/team-a` and `/team-a/sub`. NIC owns this setting through the realm setup job.

## Packs may rely on

- A `groups` claim holding full paths in ID tokens, access tokens and userinfo.

## Packs must not rely on

- Bare group names.
- Any group NIC creates for its own UIs (`argocd-admins`, `longhorn-admins`).

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A user in a nested group receives a `groups` claim with full paths in the ID token, the access token and userinfo.
