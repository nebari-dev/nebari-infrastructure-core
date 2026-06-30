# OpenShift provider — end-to-end validation

The `openshift` cluster provider is dual-mode (`mode: provision | existing`).
Provision mode stands up a **ROSA HCP** cluster via OpenTofu, derives a
cluster-admin kubeconfig, and applies the SecurityContextConstraints bootstrap.

There are two layers to validate:

1. **Infra lifecycle** (this provider's code) — provision → kubeconfig → SCC →
   destroy. Covered by the gated live tests in `provision_e2e_test.go`.
2. **Full site** (the whole Nebari stack) — ArgoCD, Keycloak, foundational
   services, and the landing page on top of a provisioned cluster. Runbook below.

All live tests are gated behind the `ocp_e2e` build tag so they never run in CI.
They require `RHCS_TOKEN` (Red Hat OCM offline token) and AWS credentials in the
environment, plus the `rosa` and `oc` CLIs on PATH.

## Layer 1 — infra lifecycle (VALIDATED)

```bash
source .rosa-session.env   # RHCS_TOKEN + AWS creds

# Cheap: tofu plan only, creates nothing (~30s)
go test -tags ocp_e2e -run TestProvisionDryRunE2E -timeout 15m -v \
    ./pkg/providers/cluster/openshift/

# Full lifecycle: provision -> kubeconfig -> SCC -> verify nodes -> destroy
go test -tags ocp_e2e -run TestProvisionLifecycleE2E -timeout 90m -v \
    ./pkg/providers/cluster/openshift/
```

Status: provision + kubeconfig (`rosa create admin` → poll `oc login`) + SCC
bootstrap + `oc get nodes` verification all pass against live ROSA HCP.

### Teardown note (handled by `destroyProvision`)

`tofu destroy` alone fails on `DeleteVpc` (DependencyViolation) because ROSA and
in-cluster controllers leave resources in the VPC that are NOT in our state:

- the security group ROSA's PrivateLink endpoint creates (`…-vpce-private-router`)
- load balancers created for `Service type=LoadBalancer` (e.g. the Gateway NLB)
  and the ENIs they attach

`destroyProvision` captures the `vpc_id` before destroying and, on a teardown
failure, sweeps these orphans (`cleanup.go`) and retries — up to 3 attempts.

## Layer 2 — full site (RUNBOOK, two-phase)

The cluster's `*.apps` domain embeds a random id assigned at creation, so it
cannot be known before provisioning. A single-command provision+site therefore
needs a custom domain + a DNS provider (only `cloudflare` is registered). The
two-phase flow below avoids that by reusing the ROSA-managed `*.apps` domain.

### Phase 1 — provision and leave up

```bash
go test -tags ocp_e2e -run TestProvisionOnlyE2E -timeout 60m -v \
    ./pkg/providers/cluster/openshift/
# writes: .nic-e2e.kubeconfig, .nic-e2e.context, .nic-e2e.appsdomain
```

### Phase 2 — full Nebari deploy onto the provisioned cluster (existing mode)

Write `phase2.yaml` using the discovered apps domain:

```yaml
project_name: nic-e2e
domain: nebari.<contents of .nic-e2e.appsdomain>   # e.g. nebari.apps.rosa.nic-e2e.<id>.p3.openshiftapps.com
certificate:
  type: selfsigned
git_repository:
  url: "https://github.com/brandonrc/nebari-ocp-poc-gitops.git"
  branch: main
  auth:
    token_env: GIT_TOKEN
cluster:
  openshift:
    mode: existing
    kubeconfig: ./.nic-e2e.kubeconfig
    context: "<contents of .nic-e2e.context>"
    storage_class: gp3-csi
    scc:
      manage: true
      name: privileged
```

```bash
# --regen-apps rewrites the shared GitOps manifests for this cluster's domain
go run ./cmd/nic deploy -f phase2.yaml --regen-apps

# Verify the stack and the site
oc --kubeconfig ./.nic-e2e.kubeconfig get pods -A | grep -Ev 'Running|Completed'
oc --kubeconfig ./.nic-e2e.kubeconfig get routes -A
curl -kI https://nebari.<appsdomain>/
```

### Teardown

```bash
go test -tags ocp_e2e -run TestDestroyOnlyE2E -timeout 40m -v \
    ./pkg/providers/cluster/openshift/
```

This exercises the full orphan sweep (Gateway NLB + PrivateLink SG) since the
app stack will have created a real load balancer in the VPC.
```
