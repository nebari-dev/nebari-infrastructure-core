# Nebari Operator

## 11.1 Scope (and What This Document Is Not)

The Nebari Operator is **not implemented in this repository**. It lives in its own project at [`github.com/nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator) with its own release cadence, CRD schema, and reconciliation logic.

NIC's only role with respect to the operator is to **deploy it as a foundational ArgoCD application** so that user-installed software packs can rely on its CRDs being present.

This document describes:

1. How NIC deploys the operator
2. The contract NIC depends on the operator providing (the `NebariApp` CRD)
3. The provider-shaped capabilities NIC passes into the operator

For the operator's CRD schema, reconciliation rules, controller code, and release notes, see the upstream repository.

## 11.2 How NIC Deploys the Operator

The operator is deployed as a foundational ArgoCD application from `pkg/argocd/templates/apps/nebari-operator.yaml`. The actual manifests are pulled from the upstream `nebari-operator` repository via Kustomize, with NIC-specific patches layered on top:

```
pkg/argocd/templates/manifests/nebari-operator/
└── kustomization.yaml      # Points at github.com/nebari-dev/nebari-operator
                            # at a pinned ref (e.g. v0.1.0-alpha.19), with
                            # patches for ingress hostname, Keycloak base path,
                            # and HTTPS port
```

The operator runs in its own namespace and watches for `NebariApp` CRs across the cluster.

## 11.3 The `NebariApp` CRD

The CRD shape is owned by the upstream operator. The relevant fields, at a high level (consult the upstream repo for the authoritative schema):

```yaml
apiVersion: nebari.dev/v1
kind: NebariApp
metadata:
  name: jupyter-hub
  namespace: jupyter
spec:
  hostname: jupyter.example.com
  routing:
    routes:
      - path: /
        backend:
          name: jupyterhub
          port: 8000
    publicRoutes: []           # Paths that should bypass OIDC
    tls: { ... }
  auth:
    enforceAtGateway: true     # If true, operator creates a SecurityPolicy
  landingPage:
    displayName: "JupyterHub"
    icon: "..."
```

Critically:

- **`spec.routing.routes`** drives the main `HTTPRoute` that the operator creates. The operator's `SecurityPolicy` targets this main route when `auth.enforceAtGateway` is true.
- **`spec.routing.publicRoutes`** drives a *second*, separate `HTTPRoute` that is intentionally not protected by the SecurityPolicy.
- **`auth.enforceAtGateway`** is orthogonal to `publicRoutes`. The operator creates the SecurityPolicy if and only if `enforceAtGateway` is true (or unset, since it defaults to true).
- **Cert and landing page** depend on `spec.hostname` (for the cert) and `spec.landingPage` + `spec.hostname` (for the landing page entry), independent of any `routes` block.

Operators of Nebari clusters and software-pack authors should treat the upstream operator's docs as authoritative.

## 11.4 Provider-Shaped Inputs from NIC

The operator's manifests need a small number of cluster-shaped values to route correctly. NIC supplies these via Kustomize patches sourced from `provider.InfraSettings(cfg)`:

| `InfraSettings` field | Operator use |
|------------------------|--------------|
| `KeycloakBasePath` | Path prefix the operator uses when constructing OIDC issuer URLs (`/auth` for the keycloakx chart used today; empty for upstream/Bitnami) |
| `HTTPSPort` | Port to use when constructing user-facing URLs in the operator's status output and landing-page registration |

The operator does not see any other parts of `NebariConfig`. In particular, it does not know which cluster provider is in use.

## 11.5 NIC's Responsibilities (Summary)

- Pin a known-good operator release in `pkg/argocd/templates/manifests/nebari-operator/kustomization.yaml`
- Render the operator's ArgoCD Application into the GitOps repo with the correct sync wave (after Keycloak, cert-manager, and Envoy Gateway are ready)
- Pass `InfraSettings.KeycloakBasePath` and `InfraSettings.HTTPSPort` into the operator manifests via Kustomize patches

That's it. NIC does not reconcile `NebariApp` CRs, does not implement the operator's controller, and does not ship any `api/v1alpha1/` package. If you find documentation that says otherwise, it is out of date.

## 11.6 Operator Upgrade Path

Bumping the operator version:

1. Update the `ref:` in `pkg/argocd/templates/manifests/nebari-operator/kustomization.yaml` to the new upstream tag.
2. Verify the operator's CRD schema hasn't broken NIC's Kustomize patches.
3. Land the change; on next `nic deploy` or `argocd app sync`, the new operator version rolls out.

## 11.7 References

- Upstream operator repo: <https://github.com/nebari-dev/nebari-operator>
- ArgoCD app manifest: `pkg/argocd/templates/apps/nebari-operator.yaml`
- Kustomize patches: `pkg/argocd/templates/manifests/nebari-operator/`
- Related discussion of `publicRoutes` + `enforceAtGateway` interaction: [`nebari-operator#118`](https://github.com/nebari-dev/nebari-operator/issues/118)
