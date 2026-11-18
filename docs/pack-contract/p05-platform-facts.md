---
promise: P5
level: stable
state: planned
changes:
  - date: 2026-11-17
    kind: founding
    hash: dad63f1a2c952816
---
# P5: Platform facts

## Promise

A pack gets the platform's domain, OIDC issuer URL and telemetry endpoints as Helm values when its chart is rendered, instead of an admin typing them into the pack's values or the pack hardcoding Service names. Five or more packs hardcode `keycloak-keycloakx-http.keycloak.svc.cluster.local` today, packs such as nebari-langfuse-pack ship `REPLACE-ME` placeholders for the hostname and issuer, and the issuer URL replaces the split-horizon workarounds (a CoreDNS patch, `verifyCert=false`, an SSRF allowlist, `keycloak.internalUrl` values). The facts arrive at render time because that is when a pack needs them: Helm writes the NebariApp `spec.hostname` (P1) and an upstream app's public URL and OIDC settings into manifests, and Argo CD renders charts without access to the cluster, so a chart cannot read anything from the cluster at that point.

## Level

stable

## State

planned. Nothing provides these values today. NIC will write them from `config.yaml` into the GitOps repository on every `nic deploy`, the same way it already gives its own foundational Applications a value file from that repository, and a pack's Argo CD Application will load them as a value file. How a pack's Application references that file depends on how packs are installed (#670). The nebari-app library chart in nebari-operator will add helpers that consume the values (issue to be filed). The promise also depends on in-cluster resolution of `*.<domain>` on local (#769) and on the cluster CA being in the trust bundle in self-signed mode (#776). Tracking issue: to be filed when the contract is accepted.

## Interface

Helm values under `global.nebari` in the pack's chart, on full installations. They sit under `global` so that subcharts receive them too.

| Key | Meaning |
|---|---|
| `contractVersion` | the contract version this cluster implements, as an EffVer version such as `0.1.0` (see [Versioning and support](policy/versioning.md)). Set only when NIC installs and manages all of the foundational software; absent on a partial installation, which the contract does not cover (see [Conformance](policy/conformance.md#partial-installations)) |
| `domain` | the cluster's base domain |
| `oidcIssuerUrl` | the OIDC issuer URL of the platform's identity provider (realm `nebari` on Keycloak) |
| `otlpGrpcEndpoint` | OTLP gRPC endpoint (P9) |
| `otlpHttpEndpoint` | OTLP HTTP endpoint (P9) |

How the values reach the pack's Application, including the path of the value file in the GitOps repository, is delivery and not part of the interface.

The recommended way to consume the values is the nebari-app library chart (planned helpers): it derives a default NebariApp hostname from `domain`, builds the pack's public URL and OIDC settings, and fails the render with a clear message when the values are missing. Packs may read `global.nebari` directly instead.

Helm passes `global` to subcharts, but most upstream charts do not template their values. A pack that wraps such a chart renders what the subchart needs into a ConfigMap or Secret with a fixed name in its own templates, and points the subchart at that object (for example through an `extraEnv` entry), which needs no templating in the subchart's values.

## Packs may rely on

- The keys in the table above, under `global.nebari`, on every full installation.
- The values matching the cluster: when a `nic deploy` changes one, Argo CD renders the pack again with the new value.
- A missing `global.nebari.contractVersion`, or a missing `global.nebari` block, meaning the cluster is outside the contract or the pack was installed without the platform values.
- Pods reaching the issuer URL on every target.
- The issuer URL's certificate verifying against the trust bundle (P7), so a backend uses the same URL for discovery, token exchange and validation.
- The issuer URL as the only identity-provider address in the contract.

## Packs must not rely on

- Keycloak's in-cluster Service name or port.
- The Keycloak namespace.
- The master realm.
- The name or path of the file the values come from, or how the Application references it.
- Reading the values from the cluster at runtime. A pod that needs one gets it from the pack's own templates, for example as an environment variable or from a ConfigMap the pack renders from `global.nebari`.
- Any key not listed in the table. Micro versions may add keys, so packs ignore keys they do not know.

## Per-target notes

- AWS: No difference.
- Azure: No difference.
- Hetzner: No difference.
- local (kind): Depends on in-cluster resolution of `*.<domain>` (#769) and, in self-signed mode, on the cluster CA being in the trust bundle (#776).
- existing: No difference in the promise. In self-signed mode the cluster CA must be in the trust bundle (#776).

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack installed the documented way renders with every listed key set to values that match the cluster, and a pod using the rendered issuer URL fetches the issuer's discovery document over TLS using the trust bundle.
