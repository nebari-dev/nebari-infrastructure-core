---
promise: P5
level: stable
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: 22bafd3d9a8c7d6f
---
# P5: Platform facts

## Promise

A pack reads the platform's domain, issuer URL and telemetry endpoints from one ConfigMap instead of hardcoding names. Five or more packs hardcode `keycloak-keycloakx-http.keycloak.svc.cluster.local` today, and the issuer URL replaces the split-horizon workarounds (a CoreDNS patch, `verifyCert=false`, an SSRF allowlist, `keycloak.internalUrl` values).

## Level

stable

## State

planned. Nothing projects a `nebari-platform` ConfigMap today. NIC will write one source ConfigMap and the operator will project a copy into every namespace labelled `nebari.dev/managed=true`. It depends on in-cluster resolution of `*.<domain>` on local (#769) and on the cluster CA being in the trust bundle in self-signed mode (#776). Tracking issue: to be filed when the contract is accepted.

## Interface

ConfigMap `nebari-platform` in each opted-in namespace, kept in sync with the source by the operator. Packs read it with `configMapKeyRef` or a volume mount. A namespace without the `nebari.dev/managed=true` label has no copy.

| Key | Meaning |
|---|---|
| `contract-version` | the contract version this cluster implements, as an EffVer version such as `0.1.0` (see [Versioning and support](policy/versioning.md)). Published only when NIC installs and manages all of the foundational software; absent on a partial installation, which the contract does not cover (see [Conformance](policy/conformance.md#partial-installations)) |
| `domain` | the cluster's base domain |
| `oidc-issuer-url` | the OIDC issuer URL of the platform's identity provider (realm `nebari` on Keycloak) |
| `otlp-grpc-endpoint` | OTLP gRPC endpoint (P9) |
| `otlp-http-endpoint` | OTLP HTTP endpoint (P9) |

## Packs may rely on

- The keys in the table above.
- A missing `contract-version` key, or a missing `nebari-platform` ConfigMap in an opted-in namespace, meaning the cluster is outside the contract.
- Pods reaching the issuer URL on every target.
- The issuer URL's certificate verifying against the trust bundle (P7), so a backend uses the same URL for discovery, token exchange and validation.
- The issuer URL as the only identity-provider address in the contract.

## Packs must not rely on

- Keycloak's in-cluster Service name or port.
- The Keycloak namespace.
- The master realm.
- Any key not listed in the table. Micro versions may add keys, so packs ignore keys they do not know.

## Per-target notes

- AWS: No difference.
- Azure: No difference.
- Hetzner: No difference.
- local (kind): Depends on in-cluster resolution of `*.<domain>` (#769) and, in self-signed mode, on the cluster CA being in the trust bundle (#776).
- existing: No difference in the promise. In self-signed mode the cluster CA must be in the trust bundle (#776).

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. The ConfigMap exists in an opted-in namespace with every listed key, and a pod can fetch the issuer's discovery document over TLS using the trust bundle.
