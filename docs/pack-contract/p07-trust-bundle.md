---
promise: P7
level: stable
state: partly built
conformance_test: pending
changes:
  - date: 2026-11-17
    kind: founding
    hash: d97572038353301f
---
# P7: Trust bundle

## Promise

A pack can verify TLS to platform endpoints without disabling certificate checks, on every cluster.

## Level

stable

## State

partly built. The ConfigMap `nebari-trust-bundle` exists only when `trust_bundle` is set. There is no cluster CA in self-signed mode yet (#776 adds that CA and its issuer). The contract requires the bundle on every cluster. data-science-pack uses it today.

## Interface

ConfigMap `nebari-trust-bundle`, key `ca-certificates.crt`, in every opted-in namespace on every cluster, whether or not `trust_bundle` is set, so packs can mount it unconditionally. It holds the system roots, the organisation CA when one is configured, and the cluster's own CA in self-signed mode. Packs point `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE` or the language equivalent at it.

## Packs may rely on

- The ConfigMap name and the key `ca-certificates.crt`.
- The bundle verifying the platform's own certificates, including the Keycloak issuer URL.

## Packs must not rely on

- Other key names such as `ca.crt` or `ca-bundle.crt`.
- The trust-manager `Bundle` resource.

## Per-target notes

- AWS: No difference.
- Azure: No difference.
- Hetzner: No difference.
- local (kind): In self-signed mode the cluster CA depends on #776.
- existing: No difference in the promise. Today trust-manager is installed only when `trust_bundle` is set; it will be installed on every cluster.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A pod with the bundle mounted verifies the issuer URL's certificate on every target, with and without `trust_bundle` set.
