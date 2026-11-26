---
promise: P18
level: experimental
state: planned
bound_to: cert-manager
changes:
  - date: 2026-11-19
    kind: founding
    hash: 4b9a93757abb721a
---
# P18: Internal certificates

## Promise

A pack can get certificates for TLS it terminates itself inside the cluster from a private CA the platform owns, and pods trust them through the trust bundle (P7). This covers admission and conversion webhooks, databases, and apps whose upstream requires TLS. Public hostnames get their certificates through NebariApp (P1), not from this issuer.

## Level

experimental

## State

planned. #776 adds the private CA (a root CA and the `nebari-ca-issuer` ClusterIssuer) on self-signed clusters only, and limits it to names under the cluster's domain. #785 (PR #787) runs it on every certificate type and lets it sign in-cluster Service names; names under the cluster's domain stay limited to self-signed clusters, and every IP address is excluded. llm-serving-pack depends on this today through `selfsigned-issuer`, which Let's Encrypt clusters do not have (llm-serving-pack #234).

## Interface

The cert-manager ClusterIssuer `nebari-ca-issuer`. A pack creates `cert-manager.io/v1` `Certificate` resources in its own namespace with `issuerRef` kind `ClusterIssuer` and name `nebari-ca-issuer`, for in-cluster Service names (names under `svc` and `svc.cluster.local`, such as `<service>.<namespace>.svc`) and, on self-signed clusters only, names under the cluster's domain. The root's name constraints exclude every IP address, so certificates from it carry DNS names only. The CA's root is in the trust bundle (P7) on every cluster. Webhook configurations get the CA through cert-manager's CA injection (`cert-manager.io/inject-ca-from`).

This is the standard split: public certificates for the gateway, a private CA the platform owns for everything inside the cluster. Pod-to-pod mTLS is not part of it; that is a mesh's job (see [Not yet](not-yet.md)), and a mesh can issue from the same root.

**Bound to cert-manager.** The interface is cert-manager's API, so another certificate controller cannot satisfy it. Path to a neutral interface: Kubernetes pod certificates (KEP-4317, `PodCertificateRequest`) and `ClusterTrustBundle` (KEP-3257), once every Kubernetes version NIC supports serves them, which would let any signer satisfy the promise.

## Packs may rely on

- The ClusterIssuer `nebari-ca-issuer` on every cluster.
- Certificates from it for the pack's in-cluster Service names, under `svc` and `svc.cluster.local`, on every cluster.
- Certificates from it for names under the cluster's domain on self-signed clusters only.
- Its root in the P7 trust bundle, so a pod with the bundle mounted verifies these certificates.
- cert-manager CA injection for webhook configurations.

## Packs must not rely on

- `selfsigned-issuer`, `letsencrypt-issuer` or any other ClusterIssuer name.
- The root CA's Secret, namespace, lifetime or key algorithm.
- Certificates for names under the cluster's domain on Let's Encrypt or existing-certificate clusters, or for any other name outside `svc` and `svc.cluster.local`. The root's name constraints reject them.
- Certificates with IP address SANs. The root excludes every IPv4 and IPv6 range.
- Certificates for Service names in another namespace.
- This issuer for public hostnames, which NebariApp (P1) covers.
- Pod-to-pod mTLS between packs.

## Per-target notes

- AWS: No difference.
- Azure: No difference.
- Hetzner: No difference.
- local (kind): No difference.
- existing: No difference in the promise. The Service DNS domain may not be `cluster.local` on an existing cluster; there, only names under `svc` (such as `<service>.<namespace>.svc`) work, because the root permits `svc.cluster.local` and not the cluster's own Service domain.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack gets a certificate from `nebari-ca-issuer` for its Service name, a pod with the trust bundle mounted verifies a TLS server using it, and a webhook using CA injection is called by the API server.
