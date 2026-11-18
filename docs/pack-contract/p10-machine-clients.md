# P10: Machine clients

## Promise

A pack can get a Keycloak client for its own service accounts without receiving any Keycloak admin credential.

## Level

experimental

## State

planned. Nothing is built. Tracking issue: to be filed when the contract is accepted.

## Interface

Fixed by its own design before it is built. The contract fixes only what it must deliver.

## Packs may rely on

Once built: a pack declares a confidential client with service-account access and client roles, and receives its credentials in a Secret, without any Keycloak admin credential reaching the pack namespace. This replaces data-science-pack's admin Job, unity-catalog's `kcadm.sh` and the hand-made clients in external-auth, llm-serving and apps.

## Packs must not rely on

Until it is built, nothing.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A pack that declares a machine client receives working credentials and no admin credential is present in its namespace.
