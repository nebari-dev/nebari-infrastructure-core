# Design: OpenShift (ROSA HCP) Cluster Provider for NIC

**Date**: 2026-06-25
**Status**: Approved (pending spec review)
**Author**: brandonrc + Claude

## Goal

Add a first-class `openshift` cluster provider to nebari-infrastructure-core (NIC)
that stands up and deploys Nebari onto Red Hat OpenShift, matching the end-to-end
experience the `aws` provider gives for EKS. Target the managed cloud flavor first
(ROSA HCP on AWS), with a design that generalizes to ARO and self-managed/CRC later.

## Decisions (locked during brainstorming)

1. **Provisioning, not just connect.** NIC provisions ROSA itself via OpenTofu,
   mirroring the `aws` provider. The `existing` provider is the thin opt-out; the
   new provider is the real "NIC for OpenShift."
2. **Dual-mode provider.** One `openshift` provider with two config-selected modes:
   - `provision` — NIC stands up ROSA HCP via OpenTofu (aws-style).
   - `existing` — point at a kubeconfig/context, skip provisioning (existing-style).
3. **Target ROSA HCP** (Hosted Control Plane) first: ~10–15 min provision, no
   control-plane EC2 cost, Red Hat-managed control plane. Single-AZ test-sized
   cluster to start.
4. **Keep Nebari's stack.** Envoy Gateway (Gateway API) + cert-manager + Keycloak,
   exposed via a `LoadBalancer` Service on ROSA's AWS load balancer. No OpenShift
   Routes/OAuth integration in this scope.
5. **Storage configurable, native CSI default.** Default `storage_class: gp3-csi`
   (ROSA). Longhorn remains opt-in (it needs privileged SCC grants; discouraged on
   OpenShift).
6. **Sequencing: manual ROSA first, then encode.** Drive a manual ROSA HCP provision
   with the user's AWS creds + OCM token to get a working cluster and a proven
   command/Terraform flow, then encode that flow into the provider's OpenTofu
   templates. De-risks the unfamiliar `rhcs` Terraform provider.

## Non-goals (deferred behind reserved config)

- ROSA Classic (only HCP for now).
- ARO / self-managed / CRC / SNO provisioning (config shaped to add later).
- OpenShift-native Routes/Router or OpenShift OAuth integration.
- ReadWriteMany / shared storage class.
- Multi-AZ production hardening (single-AZ test cluster first).

## Architecture

The `openshift` provider maps almost 1:1 onto the `aws` provider, swapping the IaC
layer. It is an in-tree package `pkg/providers/cluster/openshift/` implementing the
existing `cluster.Provider` interface. No changes to the interface or CLI commands.

| AWS provider | OpenShift (ROSA HCP) provider |
|---|---|
| OpenTofu → `terraform-aws-eks-cluster` | OpenTofu → `terraform-redhat/rosa-hcp` + `rhcs` provider |
| S3 state bucket created/managed by provider | Same S3 backend pattern, reused |
| AWS creds from env | AWS creds **+ `RHCS_TOKEN`** (OCM offline token) from env |
| `Deploy`: bucket → tofu apply EKS → add-ons → kubeconfig (EKS API) | `Deploy`: bucket → tofu apply ROSA → apply SCCs → kubeconfig (OCM/rosa) |
| `Destroy`: LB cleanup → tofu destroy → drop bucket | `Destroy`: tofu destroy ROSA → drop bucket |
| `templates/{main,variables,outputs,provider,backend}.tf` | Same file set, ROSA modules inside |

### Provider interface implementation

- **`Name()`** → `"openshift"`.
- **`Validate()`** — unmarshal config; branch on mode:
  - `provision`: require region, validate node sizing, check AWS creds (reuse
    aws-sdk credential probe) and presence of `RHCS_TOKEN`.
  - `existing`: load kubeconfig + context (reuse `existing` provider logic), and
    probe the `security.openshift.io` API group to confirm the cluster really is
    OpenShift.
- **`Deploy()`**:
  - `provision`: ensure S3 state bucket (reuse aws state helpers / shared pattern)
    → `tofu.Setup` + Init(backend) + Apply against ROSA templates → fetch kubeconfig
    → apply SCC RoleBindings → optional Longhorn (opt-in).
  - `existing`: skip provisioning → apply SCC RoleBindings → optional Longhorn.
  - DryRun → tofu plan, no SCC writes.
- **`Destroy()`**: `provision` → tofu destroy → drop state bucket. `existing` → no-op
  (optional SCC cleanup).
- **`GetKubeconfig()`**: `provision` → from OCM/rosa (admin kubeconfig or generated);
  `existing` → filter-by-context (reuse `existing`). Cache in-memory like aws.
- **`Summary()`**: mode, region (provision) or context (existing), storage class.
- **`InfraSettings()`**: `StorageClass` (default `gp3-csi`, configurable),
  `NeedsMetalLB: false`, LB annotations suited to ROSA's AWS LB, `KeycloakBasePath`
  per Keycloak chart, `SupportsLocalGitOps: false`.

### Config schema (`cluster.openshift:`)

```yaml
cluster:
  openshift:
    mode: provision            # provision | existing
    # --- provision mode ---
    region: us-east-1
    openshift_version: "4.16"
    availability_zones: [us-east-1a]    # single-AZ test cluster
    compute:
      instance_type: m5.xlarge
      replicas: 2
    networking:
      machine_cidr: 10.0.0.0/16        # rosa-created VPC
    state_bucket: ""                    # optional; auto-derived like aws
    # --- existing mode ---
    # kubeconfig: path/to/kubeconfig
    # context: my-rosa-context
    # --- shared ---
    storage_class: gp3-csi              # default; native CSI
    scc:
      manage: true                      # apply SCC RoleBindings for Nebari namespaces
    longhorn:
      enabled: false                    # opt-in; needs privileged SCC
```

The `mode` discriminator plus reserved blocks satisfy the "both/configurable"
requirement and let ARO/self-managed be added later without breaking changes.

### SCC handling (the core OpenShift-specific work)

OpenShift's SecurityContextConstraints will block Nebari's foundational pods (Envoy
Gateway, cert-manager, Keycloak, nebari-operator) unless their service accounts are
granted suitable SCCs. The provider's `Deploy` applies a small, explicit set of SCC
RoleBindings (e.g. `nonroot-v2`, escalating only where required) bound to the
specific service accounts in Nebari's foundational namespaces, via the OpenShift API
using the in-cluster client pattern already used by `pkg/storage/longhorn`. Longhorn's
privileged SCC stays behind the opt-in Longhorn flag. SCC manifests/bindings are
generated in code and unit-tested.

## Phased plan

### Phase A — Manual ROSA HCP provision (drive with user creds)

1. Install `rosa` + `oc` (Homebrew). (`openshift-install` not needed for ROSA.)
2. `rosa login` with the OCM offline token; `rosa whoami`.
3. `rosa verify quota` / `rosa verify permissions`; enable ROSA if needed.
4. `rosa create account-roles --hosted-cp`; `rosa create oidc-config`.
5. `rosa create cluster --hosted-cp --sts` (rosa-created VPC, single AZ, small node
   count), wait for ready.
6. `rosa create admin` (or IdP); `oc login`; export a kubeconfig NIC can consume.
7. Capture the exact commands/Terraform the manual flow used → input to Phase B.

**Prerequisites the user must supply:** Red Hat account + ROSA offline token; ROSA
enabled in the AWS console (service-linked roles / marketplace); awareness of cost
(~$0.03/hr HCP fee + EC2). Note: current AWS creds are **root account keys** — flag
and recommend a scoped IAM user/role.

### Phase B — `openshift` provider in NIC

1. Create `pkg/providers/cluster/openshift/` (provider.go, config.go, tofu.go,
   kubeconfig.go, scc.go, state.go, version.go + `templates/*.tf`).
2. Encode the Phase A flow as OpenTofu using `terraform-redhat/rosa-hcp` + `rhcs`.
3. Implement the interface methods (dual-mode) per above.
4. Implement SCC bootstrap.
5. Register `openshift` in `pkg/nic/registry.go`.
6. Add `examples/openshift-config.yaml`.
7. Tests: config parsing, validation, SCC manifest generation, InfraSettings,
   tofu var mapping (mirror `aws`/`existing` test files).
8. End-to-end: `nic deploy` against a fresh ROSA HCP cluster from the provider itself,
   then `nic destroy`.

## Testing strategy

- **Unit**: config unmarshal/defaults, mode validation, SCC binding generation,
  InfraSettings outputs, tofu variable mapping. Mirror existing `_test.go` patterns.
- **Integration/e2e**: provision via `nic deploy` against AWS+OCM, verify Nebari
  foundational stack reaches healthy, then `nic destroy` leaves no residue (cluster +
  state bucket). Gated behind credentials like the aws integration tests.

## Risks / open items

- `rhcs` Terraform provider is unfamiliar in this environment; Phase A de-risks it.
- ROSA account-level prerequisites (account roles, OIDC, ROSA enablement) are
  one-time per AWS account and partly outside Terraform; decide what the provider
  assumes pre-exists vs creates.
- Root AWS credentials in use — recommend scoping before provisioning.
- Kubeconfig acquisition for `provision` mode (OCM API vs `rosa create admin` vs
  break-glass) — settle during Phase A.
