# Software Pack Spins: Implementation Design

- A spin is a one-time generation input, not a lifecycle-management mechanism or a second source of truth for installed packs. This design does not add an installed-pack inventory to the NIC config: after generation, the GitOps repository is authoritative for packs and the generated NIC config is the provider input for infrastructure. There is no supported way to switch a cluster from one spin to another or to uninstall a spin as a unit; the generated packs and infrastructure are managed individually from that point on.
- The spin itself is not a live resource or controller. It generates ordinary GitOps desired-state files in the target repository, and normal GitOps reconciliation creates the live resources.
- A spin may include the first-deployment infrastructure its packs require—for example, a GPU node group for an LLM-serving workload—while allowing the operator to review and edit the generated config before deployment.
- Spins support both one-step generation-and-deployment and a reviewable generation-only workflow using the same generated artifacts. The generation-only output is a complete, reviewable bundle; a later deploy consumes those reviewed GitOps artifacts by default, and regeneration is explicit so operator edits are preserved.
- A spin is a versioned, standalone manifest resolved from a spins repository or registry—not a NIC config key, embedded binary data, or live GitOps object. NIC materializes it into editable provider config and ordinary GitOps artifacts for deployment.
- The spin schema is separate from the deployment config: it defines pack composition, cross-pack integration settings, and infrastructure requirements, while NIC materializes it into a standard editable config and GitOps artifacts. Existing examples remain hand-authored deployment-config examples, not spin inputs or a second source of truth.
- Spin provenance is recorded in `.bootstrapped`, including the spin name, immutable version or digest, and generation timestamp—not as a live config field or a per-pack comment.
- A spin makes a pack combination CI-testable as one versioned artifact. Reusing the same spin from staging to production gives the same pack composition and generated integration settings when the spin, pack references, and provider translation profiles are immutable; provider configuration, credentials, and other environment-specific inputs may still differ.


- A spin states its packs' infrastructure requirements as declared node-group definitions under `cluster.<provider>.node_groups` in the generated config. Spins emit explicitly named pools, but do not generate cluster-autoscaler or node-autoprovisioning policy; any provider-level scaling behavior remains a provider or operator concern.

- Node-group schemas are provider-specific and not uniform: AWS declares instance type, GPU, disk, labels, structured taints, and spot; Azure takes string-form taints, zones, and a mode; GCP takes guest accelerators; Hetzner takes `instance_type`, `count`, `master`, and optional autoscaling. A spin therefore cannot assume one node-group shape across providers.
- The spin schema expresses infrastructure requirements at the level of provider-neutral named sizes and capabilities rather than provider-specific machine types, and a provider-specific translator maps those requirements to the target provider's node-group shape. This keeps a spin portable when the underlying capability exists on more than one provider and confines provider knowledge to the translation step.
- Emitting that config is new surface. The deployment-config schema is established and the committed examples are hand-authored; nothing in NIC writes a config file today. [ADR-0005](../../adr/0005-nic-config-cli-surface.md) proposes `nic config init` as that writer, so a spin becomes another input to the same generation path rather than a separate config format.
- Both first-deployment paths follow from that. Flag names are illustrative.
  Generate and deploy in one step:
  ```
  nic deploy --spin data-science --provider aws --size large
  ```
  Generate only, then review and edit the config and GitOps files before applying anything:
  ```
  nic config init --spin data-science --provider aws --size large
  <review generated config.yaml>
  nic deploy -f config.yaml
  ```
- A spin owns both sides of scheduling—the infrastructure that provides a resource and the pack values that request it. In the generated AWS config, `gpu: true` is sufficient on the infrastructure side: the provider derives the NVIDIA AMI and the `nvidia.com/gpu` taint and installs the GPU Operator ahead of the GitOps stage, so the device plugin can advertise the resource before pack Applications sync ([ADR-0006](../../adr/0006-conditional-foundational-software-helm.md)). The corresponding toleration, node selector, and GPU resource limit are not inferred from arbitrary provider config and must be emitted into the pack's values. A spin that emits one side without the other produces idle GPU nodes and a pending pack.

- NIC supports that GPU infrastructure cascade end-to-end only on AWS today. An `existing` cluster is different: if the operator has already provided GPU nodes, drivers, taints, and a device plugin, the spin does not configure GPU infrastructure and treats that capability as an operator-supplied precondition.

  | Provider | GPU surface |
  |---|---|
  | AWS | Full cascade: `gpu: true` selects the NVIDIA AMI and applies the `nvidia.com/gpu` taint; the GPU Operator install and teardown are gated on the flag |
  | GCP | A differently shaped `guest_accelerators` field (accelerator name and count) that nothing consumes — the provider has no OpenTofu templates |
  | Azure | No GPU field; GPU appears only as an example in taint-syntax documentation |
  | Hetzner | No GPU field |
  | Local | No node-group configuration |
  | Existing | No NIC-managed node-group configuration; GPU support is an operator-supplied precondition |

- The AWS `gpu: true` field is a provider translation detail, not a portable spin field. The spin expresses a provider-neutral accelerator requirement; the AWS translator emits `gpu: true`, and a future provider translator can map the same requirement to its own node-group shape. Generation validates requirements against provider capabilities and fails unsupported combinations before deployment rather than switching on provider names. Full support for another provider also requires that provider to implement its accelerator-specific node provisioning and conditional GPU Operator/device-plugin lifecycle in the provider layer, as described by [ADR-0006](../../adr/0006-conditional-foundational-software-helm.md).
- Spin manifests start as a `spins/` directory in this repo and are resolved as versioned data, not compiled into the `nic` binary. A separate `nebari-spins` repo or registry is a later distribution decision. Remote sources must use immutable refs, and hardened mode requires digest pinning for fetched deployment inputs ([ADR-0010](../../adr/0010-high-security-mode.md) M-08).
