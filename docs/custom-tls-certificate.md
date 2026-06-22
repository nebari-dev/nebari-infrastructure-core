# Custom TLS Certificate for the Nebari Gateway

By default, NIC has [cert-manager](https://cert-manager.io/) mint the TLS
certificate for the Nebari gateway, using either a self-signed or Let's Encrypt
`ClusterIssuer`. If you already manage certificates yourself (a corporate CA, a
wildcard cert, an external ACME setup, etc.), you can supply your own certificate
instead with `certificate.type: existing`.

## How TLS termination works

Nebari fronts all in-cluster HTTPS traffic with a single Envoy Gateway in the
`envoy-gateway-system` namespace. That one gateway terminates TLS for **every**
hostname under your configured `domain`, so a single certificate must cover all
of them.

## SAN requirements

The certificate must include Subject Alternative Names (SANs) for:

- `<domain>` — e.g. `nebari.example.com`
- `keycloak.<domain>` — e.g. `keycloak.nebari.example.com`
- `argocd.<domain>` — e.g. `argocd.nebari.example.com`

A wildcard plus the apex also works: `*.nebari.example.com` **and**
`nebari.example.com` (a wildcard alone does not match the bare apex).

> NIC parses the certificate and **warns** — it does not fail — when one of these
> hostnames is missing. This is intentional: many valid setups carry additional
> hostnames NIC doesn't know about, and hard-failing would block them. If you see
> a SAN warning, double-check the served hostnames before going to production.

## The three sources

Set `certificate.type: existing` and provide **exactly one** of the following
sources. `secret_name` is optional and defaults to `nebari-gateway-tls`.

### 1. `existing_secret` — reference a secret you already created

Use this when you (or another controller, e.g. a `Certificate` you manage, an
external-secrets sync, etc.) already maintain a `kubernetes.io/tls` secret in the
cluster.

```yaml
certificate:
  type: existing
  existing_secret:
    name: my-gateway-tls
    # namespace defaults to envoy-gateway-system
```

Create the secret with `kubectl`:

```bash
kubectl create secret tls my-gateway-tls \
  --cert=tls.crt \
  --key=tls.key \
  --namespace envoy-gateway-system
```

NIC does **not** create or overwrite this secret; it only reads it to run the SAN
check. Make sure it exists before traffic is served.

### 2. `files` — read PEM material from disk

NIC reads the PEM files at deploy time and creates the `kubernetes.io/tls` secret
directly in `envoy-gateway-system`.

```yaml
certificate:
  type: existing
  files:
    cert_file: /path/to/tls.crt
    key_file: /path/to/tls.key
```

The cert/key are written **only** to the cluster secret — they are never committed
to the GitOps repository.

### 3. `env` — read PEM material from environment variables

The variables hold **raw PEM** (not base64-encoded). NIC reads them at deploy
time and creates the secret directly.

```yaml
certificate:
  type: existing
  env:
    cert_env: NEBARI_TLS_CERT
    key_env: NEBARI_TLS_KEY
```

```bash
export NEBARI_TLS_CERT="$(cat tls.crt)"
export NEBARI_TLS_KEY="$(cat tls.key)"
nic deploy -c config.yaml
```

As with `files`, the material is never persisted to the GitOps repository.

## Cross-namespace secrets and ReferenceGrant

If your `existing_secret` lives in a namespace **other than**
`envoy-gateway-system`, the Gateway API requires explicit permission for the
gateway to read it across namespaces. NIC handles this automatically: it sets the
`namespace` on the gateway's `certificateRefs` and renders a
[`ReferenceGrant`](https://gateway-api.sigs.k8s.io/api-types/referencegrant/) in
your secret's namespace.

```yaml
certificate:
  type: existing
  existing_secret:
    name: my-gateway-tls
    namespace: my-tls-namespace
```

> **Prerequisite:** for a cross-namespace `existing_secret`, both the namespace
> **and** the secret must already exist in the cluster before you deploy. NIC
> does not create the namespace (it belongs to you), and Argo CD cannot apply the
> generated `ReferenceGrant` into a namespace that doesn't exist yet.

The generated `ReferenceGrant` (committed to the GitOps repo) looks like:

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: nebari-gateway-tls-grant
  namespace: my-tls-namespace
spec:
  from:
    - group: gateway.networking.k8s.io
      kind: Gateway
      namespace: envoy-gateway-system
  to:
    - group: ""
      kind: Secret
      name: my-gateway-tls
```

This only applies to `existing_secret`. The `files` and `env` sources always
create the secret in `envoy-gateway-system`, so no `ReferenceGrant` is needed.

## Validation rules

`nic validate` (and `nic deploy`) reject:

- `type: existing` with **no** source set
- `type: existing` with **more than one** of `existing_secret` / `files` / `env`
- `files` or `env` with only one of the pair set (both are required)
- `existing_secret` without a `name`
- combining `acme` with `type: existing`

## Verifying the served certificate

After deploying, confirm the gateway is serving your certificate:

```bash
echo | openssl s_client -connect argocd.nebari.example.com:443 \
  -servername argocd.nebari.example.com 2>/dev/null \
  | openssl x509 -noout -issuer -subject -ext subjectAltName
```

Repeat for `keycloak.<domain>` and the apex `<domain>`.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| SAN warning during deploy | Cert is missing `keycloak.` / `argocd.` / apex | Reissue the cert with all required SANs (or a wildcard + apex). |
| Browser shows the default self-signed cert | The gateway can't find the secret | Confirm the secret exists in the right namespace and matches `secret_name` / `existing_secret.name`. |
| `existing_secret` in another namespace not served | Missing cross-namespace permission | NIC renders a `ReferenceGrant` automatically; ensure the GitOps sync applied it (check Argo CD). |
| Deploy warning: "Failed to configure gateway TLS certificate" | Bad `files` path, empty `env` var, or mismatched cert/key | Fix the path/var, ensure the cert and key are a valid pair, redeploy. |
| `invalid TLS certificate/key pair` | Cert and key don't match, or malformed PEM | Verify with `openssl x509 -noout -modulus -in tls.crt` vs `openssl rsa/ec -noout -modulus -in tls.key`. |

See [`examples/custom-tls-config.yaml`](../examples/custom-tls-config.yaml) for a
complete, annotated configuration.
