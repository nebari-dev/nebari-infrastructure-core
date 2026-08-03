# Software Pack Spins: Implementation Design

NIC provides a foundational layer plus operator-selected packs. A spin is a curated, install-time composition of packs and any infrastructure they require. After generation, it becomes an ordinary NIC deployment: operators manage the resulting packs and infrastructure individually, and NIC does not reapply the spin. A spin is a default answer to the composition question, not a new lifecycle; it stops existing once generation finishes.

A durable `spin:` config field is rejected. Pack Applications use `prune: true` and `selfHeal: true`; changing spin membership could remove an existing Application. Generated Applications are user-owned once written, so changing or removing a pack requires an explicit operator edit. A spin version must not change installed software automatically.

## Lifecycle and generated artifacts

- A spin is a one-time input, not a runtime object, lifecycle mechanism, or pack inventory. It generates ordinary GitOps desired-state files; after generation, GitOps owns packs and NIC config drives infrastructure. Operators manage the generated outputs individually.
- A spin may include first-deployment infrastructure its packs require—for example, a GPU node group for an LLM-serving workload—while allowing the operator to review and edit the generated config before deployment.
- Spins support one-step generation-and-deployment and a generation-only review flow. Both produce the same bundle; a later deploy consumes reviewed GitOps artifacts by default, and regeneration is explicit so operator edits are preserved.
- Generation also needs deployment context, such as the cluster domain and GitOps repository settings. Remote repositories additionally need auth environment-variable names. A spin name alone is not a deployable config; this context can come from flags or a partial config.
- For each selected pack, a spin writes an ordinary Argo CD `Application` to `<git-repository>/<git-path>/user-apps/<id>.yaml`. It includes the pack's namespace, Helm or Git source, pinned version or revision, `project: nebari-apps`, and generated values or secret references. No separate spin descriptor is used. Operators can also place their own Applications in `user-apps/`.
- `user-apps/` is a top-level sibling of NIC's `apps/` directory. Everything under it is user-owned after it is written. Foundational generation and `--regen-apps` must not overwrite or delete it; enforce this in the deletion path, as with the user-owned overlay seam described in [#499](https://github.com/nebari-dev/nebari-infrastructure-core/pull/499), rather than by convention.
- NIC owns only `apps/user-apps-root.yaml`, an app-of-apps that discovers Applications under `user-apps/` in the `nebari-apps` project. The operator owns the Applications it discovers.
- Foundational apps use sync waves 1–6. Pack Applications use a later wave because spins install them during first deployment; otherwise they can start before the required CRDs, gateways, and operators are ready.
- A spin is a versioned, standalone manifest resolved from a spins repository or registry—not a NIC config key, embedded binary, or live GitOps object. NIC materializes it into editable provider config and ordinary GitOps artifacts.
- The spin schema is separate from the deployment config: it defines pack composition, cross-pack value wiring, and infrastructure requirements. Existing examples remain hand-authored deployment-config examples, not spin inputs or another source of truth.
- Spin provenance is recorded in `.bootstrapped`—the spin name, immutable version or digest, and generation timestamp—not in live config or per-pack comments. The recorded version is provenance only; no code path reads it as desired state or uses it to reconcile packs. Only deploy writes this marker: a generation-only run must not create it because its presence makes deploy skip foundational manifest generation. The marker writer must merge existing fields so regeneration preserves provenance.
- A spin gives CI a versioned pack composition to validate. Reusing the same spin from staging to production gives the same composition and wiring when the spin, pack references, and provider mappings are immutable; provider configuration, credentials, and other environment-specific inputs may still differ.
- Spins pin the pack versions CI validated together, and there is deliberately no spin upgrade. Once generated, pack versions live in GitOps and are upgraded there per pack. A new spin release is a known-good set to compare and apply deliberately, not a mechanism that reasserts itself on existing clusters.
- Every spin release requires re-validating its pinned set. The roster should grow only as fast as someone can own that curation.

## Provider translation

- A spin states its packs' infrastructure requirements as declared node-group definitions under `cluster.<provider>.node_groups` in the generated config. Spins emit explicitly named pools, but do not generate cluster-autoscaler or node-autoprovisioning policy; provider-level scaling remains a provider or operator concern.
- Node-group schemas are provider-specific: AWS declares instance type, GPU, disk, labels, structured taints, and spot; Azure uses string-form taints, zones, and a mode; GCP uses guest accelerators; Hetzner uses `instance_type`, `count`, `master`, and optional autoscaling.
- The spin schema expresses requirements through provider-neutral capabilities and named size tiers. A data-only table in the spin layer maps `(provider, requirement, tier)` to a node-group definition. It needs no credentials or API calls, so adding a size tier does not expand the provider plugin interface.
- Requirements are per pack, while `--size` selects one tier for the whole spin. One spin can therefore emit separate CPU and accelerator pools from a single size selection; per-pack size flags are deliberately avoided.
- Emitting this config is new surface. The deployment-config schema is established and the committed examples are hand-authored; NIC does not write config files today. [ADR-0005](../../adr/0005-nic-config-cli-surface.md) proposes `nic config init` as that writer, so a spin uses the same generation path rather than a separate config format.
- Flag names are illustrative. Deployment-context flags are omitted below:

  Generate and deploy in one step:

  ```
  nic deploy --spin data-science --provider aws --size large
  ```

  Generate only, then review and edit the config and GitOps files:

  ```
  nic config init --spin data-science --provider aws --size large
  <review generated config.yaml>
  nic deploy -f config.yaml
  ```

## GPU and other external capabilities

- A spin owns both sides of scheduling: the infrastructure that provides a resource and the pack values that request it. In generated AWS config, `gpu: true` selects the NVIDIA AMI, adds the `nvidia.com/gpu` taint, and installs the GPU Operator before GitOps begins. Pack values must still include the matching toleration, node selector, and GPU resource limit. Emitting only one side leaves idle GPU nodes or pending workloads.
- NIC manages this GPU cascade end-to-end only on AWS today. For `existing`, GPU support is an operator-supplied precondition; the spin emits no GPU infrastructure. For `local`, spins emit pack-side artifacts only, so a pack that needs capabilities Kind cannot provide may remain pending.

  | Provider | GPU surface |
  |---|---|
  | AWS | Full cascade: `gpu: true` selects the NVIDIA AMI and applies the `nvidia.com/gpu` taint; GPU Operator install and teardown are gated on the flag |
  | GCP | `guest_accelerators` exists, but the provider is a stub and nothing consumes it |
  | Azure | No GPU field |
  | Hetzner | No GPU field |
  | Local | No node-group configuration |
  | Existing | No NIC-managed node-group configuration; GPU support is operator-supplied |

- Generation validates requirements against provider capabilities. For providers that manage node groups, an unsupported requirement fails before deployment. For `local` and `existing`, infrastructure requirements are external preconditions and may result in pending workloads. Full support for another provider also requires accelerator-specific node provisioning and a conditional GPU Operator/device-plugin lifecycle in that provider, as described by [ADR-0006](../../adr/0006-conditional-foundational-software-helm.md).
- A spin is not privileged. Its infrastructure requirements and packs go through the same checks as individual installs: catalog membership, source allow-listing, pinned versions or digests, namespace, values schema, and no inline secrets. A spin is a preset batch of ordinary install requests, not a path around those checks.
- Cross-pack integration means value wiring, not ordering. There is no pack dependency mechanism ([#428](https://github.com/nebari-dev/nebari-infrastructure-core/issues/428)); pack Applications are independent, and manifest order does not order them. CI validates each spin as a unit, reducing operator burden but not resolving #428.
- Singleton capabilities are declared in the spin and validated at generation. For example, only one pack may own the foundational OpenTelemetry Collector override ([ADR-0008](../../adr/0008-otel-collector-software-pack-override-point.md)). Generation rejects duplicate claims within a spin; conflicts with packs already in the target repository remain part of per-pack validation.
- Spin manifests start under `spins/` in this repo and are resolved as versioned data, not compiled into the `nic` binary. A separate `nebari-spins` repo or registry is a later distribution decision. Remote sources must use immutable refs, and hardened mode requires digest pinning for fetched deployment inputs ([ADR-0010](../../adr/0010-high-security-mode.md) M-08).
