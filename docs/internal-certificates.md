# Internal Certificates for Software Packs

Every Nebari cluster runs a private certificate authority for TLS that a pack terminates inside the cluster: admission webhooks, databases, and apps that require TLS between pods. It runs on every `certificate.type` (`selfsigned`, `letsencrypt` and `existing`). The gateway's public certificate is separate and unchanged; see [Custom TLS Certificate for the Nebari Gateway](custom-tls-certificate.md) for that.

## What the platform provides

| Object | Name | Where |
|--------|------|-------|
| Root CA `Certificate` and `Secret` | `nebari-root-ca` (subject `CN=Nebari root CA (<domain>)`) | `cert-manager` namespace |
| `ClusterIssuer` that signs with the root | `nebari-ca-issuer` | cluster-scoped |
| Trust bundle `ConfigMap` | `nebari-trust-bundle`, key `ca-certificates.crt` | every namespace, projected by trust-manager |

The trust bundle always holds the public root CAs, the cluster's root CA, and the organization CA from the top-level `trust_bundle` when one is configured (see [Trust Bundle Configuration](configuration/trust-bundle.md)).

## Get a certificate for your Service

Request a cert-manager `Certificate` from `nebari-ca-issuer` for your Service's in-cluster names:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-service-tls
  namespace: my-pack
spec:
  secretName: my-service-tls
  issuerRef:
    name: nebari-ca-issuer
    kind: ClusterIssuer
  dnsNames:
    - my-service.my-pack.svc
    - my-service.my-pack.svc.cluster.local
```

cert-manager writes the key, the certificate and the root (`ca.crt`) to the Secret named in `secretName` and renews the certificate before it expires. Your server must pick up the renewed files, either by watching them or by restarting.

## What the root CA may sign

The root CA carries critical X.509 name constraints, so clients reject any certificate it signs for names outside them, even if cert-manager issues it:

- DNS names under `svc` and `svc.cluster.local` on every cluster. `svc` is needed because the API server calls webhooks at `<service>.<namespace>.svc`.
- DNS names under the cluster's `domain` only on `certificate.type: selfsigned`, where the root also signs the gateway and the operator's per-app certificates. On `letsencrypt` and `existing` clusters the root cannot vouch for names under the domain.
- No IP addresses. Every IPv4 and IPv6 range is excluded, so a certificate with an IP SAN does not verify. Use the Service DNS names instead.
- URI SANs are not constrained, so a future service mesh can issue SPIFFE identities from the same root.

Pod DNS names such as `10-0-0-1.my-pack.pod.cluster.local` are outside the permitted names.

## Trust certificates from inside a pod

Mount the `nebari-trust-bundle` ConfigMap and point your client at `ca-certificates.crt`:

```yaml
volumes:
  - name: nebari-trust-bundle
    configMap:
      name: nebari-trust-bundle
containers:
  - name: client
    env:
      - name: SSL_CERT_FILE
        value: /etc/nebari/trust/ca-certificates.crt
    volumeMounts:
      - name: nebari-trust-bundle
        mountPath: /etc/nebari/trust
        readOnly: true
```

Variables such as `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE` and `NODE_EXTRA_CA_CERTS` replace the default trust store rather than extend it; the bundle includes the public roots so outbound TLS to public services keeps working.

## Admission webhooks and conversion webhooks

Serve the webhook with a `nebari-ca-issuer` certificate for `<service>.<namespace>.svc`, and let cert-manager's CA injector fill in the `caBundle` by annotating the `ValidatingWebhookConfiguration`, `MutatingWebhookConfiguration` or `CustomResourceDefinition`:

```yaml
metadata:
  annotations:
    cert-manager.io/inject-ca-from: my-pack/my-webhook-tls
```

The value is `<namespace>/<Certificate name>`; cert-manager copies that Certificate's CA into the configuration.

## Who can issue

Anyone who can create cert-manager `Certificate` or `CertificateRequest` resources in any namespace can get a certificate from `nebari-ca-issuer`. cert-manager's built-in approver approves every request to it, and nothing ties a request to the requester's own namespace, so a principal who can create Certificates in one namespace can get a certificate for another namespace's Service name. Grant those permissions only to principals you would trust to serve any in-cluster Service name.

## What is not provided

- Pod-to-pod mutual TLS. The platform issues server certificates on request; it does not issue workload identities or enforce mTLS. That is a service mesh's job.
- Automatic trust for pods that do not mount the bundle. The nebari-operator does not yet mount it for every NebariApp.

## Upgrading a Let's Encrypt or existing-certificate cluster

Earlier versions ran trust-manager on `letsencrypt` and `existing` clusters only when the top-level `trust_bundle` was set. When such a cluster without `trust_bundle` is upgraded, Keycloak's pod is restarted with the trust bundle mounted and waits in `ContainerCreating` until trust-manager has projected `nebari-trust-bundle` into the `keycloak` namespace. Expect a few minutes of SSO outage during the upgrade. The gateway's public certificate is not reissued.
