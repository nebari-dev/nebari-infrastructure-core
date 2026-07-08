# Verifying enterprise CA-bundle propagation against a mock TLS-inspecting proxy

This is the developer recipe for issue #312 (child of the enterprise CA-bundle
epic #307). It lets you confirm, locally and without a corporate sandbox, that
a cluster deployed with `trust_bundle` actually trusts an org CA end-to-end when
all egress is intercepted by a TLS-inspecting proxy.

The idea: run [mitmproxy](https://mitmproxy.org/) as an egress proxy that
re-signs every upstream certificate with a throwaway "org CA", deploy a cluster
that trusts that CA via `trust_bundle`, then drive outbound HTTPS from the
affected components through the proxy and assert it succeeds.

## Prerequisites

- Docker, `kubectl`, `openssl`.
- A local cluster (kind or k3d) whose node(s) sit on a docker network. Find the
  network name with `docker inspect <node-container> --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}'` (kind: `kind`; k3d: `k3d-<clustername>`).

## Recipe

1. **Generate a throwaway org CA:**

   ```bash
   scripts/verify-tls-proxy.sh --gen-ca /tmp/mockca
   # prints /tmp/mockca/org-ca.crt (deploy with this) and org-ca.key (verify with this)
   ```

2. **Deploy the cluster trusting that CA.** Point `trust_bundle` at the cert and
   deploy as usual:

   ```yaml
   trust_bundle:
     path: /tmp/mockca/org-ca.crt
   ```

   This propagates the org CA to the worker-node trust store (AWS), and — once
   the foundational apps sync — to the ArgoCD repo-server (#353) and Keycloak
   (#350) via trust-manager's projected `nebari-trust-bundle` (#346).

3. **Run the verification:**

   ```bash
   scripts/verify-tls-proxy.sh \
     --kubeconfig ~/.kube/config \
     --docker-network k3d-mycluster \
     --ca-cert /tmp/mockca/org-ca.crt \
     --ca-key  /tmp/mockca/org-ca.key
   ```

   The script starts mitmproxy on the cluster's docker network signing with the
   org CA, then from the repo-server it:
   - clones an HTTPS git repo through the proxy using the propagated bundle and
     asserts **success**, and
   - repeats the clone forced onto the system-only CA bundle and asserts
     **failure** (`x509: certificate signed by unknown authority`).

   The negative control matters: it proves the success is due to the injected
   org CA, not to the target happening to be reachable another way.

## Extending to other components

The same proxy works for any in-cluster client — point it at
`http://<proxy-ip>:8080`. For example, to check Keycloak's outbound trust
(#311/#350), exec the Keycloak pod and run an HTTPS request through the proxy
using its truststore. The repo-server case is scripted here because it is the
clearest DoD (#310/#353); the others are left as manual `kubectl exec` checks
for now.

## CI

Deferred, deliberately. This harness needs Docker (to run mitmproxy on the
cluster's docker network) plus a live kind/k3d cluster deployed with
`trust_bundle`, which is heavier than the current unit/template test tiers. Run
it locally or in a nightly/optional job rather than on every PR. Revisit if the
CA-bundle stack starts regressing.
