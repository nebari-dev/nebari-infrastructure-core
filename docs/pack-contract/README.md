# Pack platform contract

This is the reference for what Nebari promises to software packs, and how those promises change over time. The decision behind it is [ADR-0023](../adr/0023-pack-platform-contract.md). The contract is proposed and open for feedback; nothing on these pages is a commitment until the ADR is accepted.

Contract version: `0.1.0` (proposed), versioned with [EffVer](policy/versioning.md). The contract is pre-1.0 while any required promise is not fully built. It becomes 1.0 when every required promise is built and its conformance test passes on every supported target.

Targets: AWS, Azure, Hetzner, local (kind) and existing clusters. Every promise works the same on every target, or its page says how a target differs and the pack is told. GCP joins when its provider is supported.

The NebariApp chapter lives in the nebari-operator [pack specification](https://github.com/nebari-dev/nebari-operator/blob/main/docs/pack-specification.md). This contract pins that specification at version `0.1.0`. [P2](p02-several-nebariapps.md) needs one rule in it relaxed, which is pending.

## Levels and states

- **Stable:** packs may rely on it without reservation. Covered by the [compatibility promise](policy/compatibility.md) once built.
- **Experimental:** offered and, once built, tested on every target, but may change shape between contract versions with notice. Packs that use it say so in their README.
- **Built:** exists today on every target as the page describes.
- **Partly built:** exists, but not yet as promised on every target; the page says what is missing.
- **Planned:** not built yet. The page gives the intended interface, which may change before it is built.

## Promises

All seventeen are required for 1.0.

| # | Promise | Level | State | Page |
|---|---|---|---|---|
| P1 | Routing, TLS, SSO and landing tile through NebariApp | stable | partly built | [P1](p01-nebariapp.md) |
| P2 | Several NebariApps per pack | stable | planned | [P2](p02-several-nebariapps.md) |
| P3 | Default storage class | stable | partly built | [P3](p03-default-storageclass.md) |
| P4 | Postgres through CloudNativePG | stable | built | [P4](p04-cloudnativepg.md) |
| P5 | Platform facts (`nebari-platform` ConfigMap) | stable | planned | [P5](p05-platform-facts.md) |
| P6 | Groups claim as full paths | stable | partly built | [P6](p06-groups-claim.md) |
| P7 | Trust bundle | stable | partly built | [P7](p07-trust-bundle.md) |
| P8 | Argo CD project for packs | stable | built | [P8](p08-argocd-project.md) |
| P9 | Telemetry endpoint | stable | partly built | [P9](p09-telemetry.md) |
| P10 | Machine clients | experimental | planned | [P10](p10-machine-clients.md) |
| P11 | API clients with bearer tokens through the gateway | experimental | planned | [P11](p11-api-clients.md) |
| P12 | Logged-in identity passed to the app | experimental | planned | [P12](p12-identity-headers.md) |
| P13 | Per-route timeouts, streaming and buffer size | experimental | planned | [P13](p13-route-traffic.md) |
| P14 | Generated secrets stable under Argo CD | experimental | planned | [P14](p14-generated-secrets.md) |
| P15 | GPU node label and taint | experimental | planned | [P15](p15-gpu-nodes.md) |
| P16 | NetworkPolicy enforced | experimental | planned | [P16](p16-networkpolicy.md) |
| P17 | Bucket binding | experimental | planned | [P17](p17-bucket-binding.md) |

Before 1.0, these known problems are fixed, each with a regression test: NebariApp `gateway: internal` points at a Gateway NIC never creates; several repositories name the `selfsigned-issuer` ClusterIssuer, which exists only in self-signed mode; the operator publishes an auth-enabled route before its SecurityPolicy is accepted (nebari-operator #152); the software pack template uses `project: default`, `gateway: internal` and a `clientSecretRef` that does not work; and the realm's bootstrap `admin` user collides with packs' own `admin` users.

## What is not promised

- [Provided, not promised](provided-not-promised.md): exists on some targets; packs may use it, but must work or fail clearly where it is missing.
- [Not yet](not-yet.md): considered and not offered yet.
- [Do not rely on](do-not-rely-on.md): names and behaviors that look stable and are not.

## Policies

- [Compatibility](policy/compatibility.md): the promise that packs keep working, and its exceptions.
- [Versioning and support](policy/versioning.md): what version numbers mean, the support window, the compatibility matrix and pack-declared versions.
- [Proposals and governance](policy/proposals.md): how new capabilities are adopted through platform RFDs, how experimental promises graduate, and who decides.
- [Deprecation and behavior changes](policy/deprecation.md): how promises are retired and how behavior changes behind compatibility settings.
- [Security exceptions](policy/security.md): when and how a promise may break to fix a vulnerability.
- [Conformance](policy/conformance.md): how clusters and packs show they meet the contract, and what a pack must do.

## Upgrading existing clusters

Clusters already deployed reach the contract by running `nic deploy` with a NIC release that implements it; nobody has to recreate a cluster. Each change that touches a live cluster migrates in place, and the release notes for that NIC version list what changes for admins.
