# OpenShift Deployment Notes — live ROSA HCP run (2026-06-25)

What actually happened deploying Nebari onto a live ROSA HCP cluster, the manual
steps required, and what the `openshift` provider must encode to make `nic deploy`
do it end-to-end. This is the field report behind the Phase B plan.

## Cluster

- ROSA HCP, OpenShift 4.20.25, single-AZ, 2× m5.xlarge, `gp3-csi` default storage.
- Provisioned manually (see `phase-a-runlog.md`): account roles, OIDC, single-AZ VPC
  (`rosa create network`), `rosa create cluster --hosted-cp`.
- Hard requirement: ROSA refuses **root** AWS creds — a dedicated IAM user is
  mandatory. Billing must be enabled via the AWS console Marketplace subscription
  (one-time, not CLI-scriptable).

## Deploy path used

`nic deploy` with the **stock `existing` provider** pointed at the cluster's
kubeconfig + a remote GitOps repo (`brandonrc/nebari-ocp-poc-gitops`, HTTPS token
auth). Result: ArgoCD + full foundational stack (cert-manager, Envoy Gateway,
Keycloak, postgresql, nebari-operator, landing page, OTel) all Healthy.

## Manual interventions required (the automation gaps)

### 1. SecurityContextConstraints — the big one
OpenShift's default `restricted-v2` SCC assigns pods an arbitrary high UID and
forbids fixed UIDs. NIC's upstream Helm charts hardcode `runAsUser: 999` **and**
`seccompProfile: RuntimeDefault`. No stock SCC allows *both*:
- `restricted-v2`: allows the seccomp profile, **forbids UID 999**.
- `anyuid`: allows UID 999, **forbids setting any seccomp profile**.

Only `privileged` (of the stock SCCs) permits both, so the foundational pods
(starting with `argocd-redis`) will not schedule until granted it. Applied
manually:
```
oc adm policy add-scc-to-group privileged system:serviceaccounts:<ns>
```
for: argocd, cert-manager, envoy-gateway-system, keycloak, nebari-system,
nebari-operator-system, monitoring, nebari.

**Timing matters:** these must be in place *before* ArgoCD is installed. On the
first run the ArgoCD Helm `--wait` timed out (5 min) because `argocd-redis` was
SCC-blocked; NIC logged a warning, continued, but then skipped foundational-app
creation and hung on "waiting for load balancer endpoint". A re-run (with SCCs in
place) succeeded immediately.

### 2. Ingress / DNS exposure
NIC exposes the Envoy Gateway via a `Service type=LoadBalancer` (an AWS ELB) and
expects external DNS: `*.nebari.<domain>` CNAME → ELB. On the OpenShift `*.apps`
wildcard domain we used, we bridged with **passthrough OpenShift Routes** to the
gateway service for each host (landing, keycloak, argocd):
```
oc create route passthrough <name> -n envoy-gateway-system \
  --service=<envoy-gateway-svc> --port=https-443 --hostname=<host>
```
With a real domain + DNS (CNAME to the ELB) these Routes are unnecessary.

### 3. TLS
Deployed with `certificate: selfsigned` for speed → browser warnings, and the
cross-domain redirect to the `keycloak.*` host must be cert-accepted separately.
Production wants `letsencrypt` (needs real DNS) or trusted certs.

## What the `openshift` provider must encode (remaining work)

| Gap | Where it lands |
|---|---|
| Grant the right SCC (privileged, or a custom anyuid+seccomp SCC) to foundational namespaces **in `provider.Deploy`, before ArgoCD install** | B8 + revise B5 (currently grants `anyuid`; live run proved `privileged` is required for the redis UID+seccomp combo) |
| Provision-mode kubeconfig retrieval (rosa/OCM admin) | B7 provision path |
| Deploy/Destroy/Summary wiring (existing: SCC→longhorn; provision: state→tofu→kubeconfig→SCC) | B8 |
| Optional: auto-create OpenShift Routes for the gateway hosts when no external DNS | new, OpenShift-specific exposure helper |
| Example config + provider docs | B9 |
| End-to-end provision-mode test | B10 |

### Key correction to the plan
B5 was written to grant `anyuid`. The live cluster proved `anyuid` is insufficient
for charts that set both a fixed UID and a seccomp profile (argocd-redis). The
provider should grant `privileged` to the foundational namespaces (simplest, demo-
ready) or ship a custom SCC `nebari-anyuid-seccomp` (RunAsAny + seccomp
RuntimeDefault) for least-privilege. This must run in `Deploy` before ArgoCD.

## Access (this deployment)

- Nebari landing: `https://nebari.apps.rosa.nebari-ocp-poc.s4gl.p3.openshiftapps.com`
- Keycloak: `https://keycloak.nebari.apps.rosa.nebari-ocp-poc.s4gl.p3.openshiftapps.com`
- ArgoCD: `https://argocd-argocd.apps.rosa.nebari-ocp-poc.s4gl.p3.openshiftapps.com`
- Credentials live in cluster secrets (`keycloak-admin-credentials`,
  `nebari-realm-admin-credentials`, `argocd-initial-admin-secret`) — not committed.
