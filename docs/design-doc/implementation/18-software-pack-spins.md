# Software Pack Spins: Implementation Design

NIC provides a foundational layer plus operator-selected packs. A spin is a curated, install-time set of packs and the infrastructure they require. Generation turns it into ordinary NIC config and GitOps files; after that, operators manage the outputs individually. A spin is not a runtime object or an upgrade mechanism.

There is no durable `spin:` config field. Generated pack Applications use `prune: true` and `selfHeal: true`, but the files under `user-apps/` are user-owned after generation. Adding or removing a pack therefore requires an explicit edit, and releasing a new spin never changes installed software automatically.

## Boundaries

- **Spin and pack.** A spin selects and wires packs; once installed, each is an ordinary pack that operators upgrade or remove individually.
- **Spin and per-pack integration ([ADR-0003](../../adr/0003-software-pack-codegen.md)).** ADR-0003 defines how an individual pack publishes its integration surface: conventions plus `nebari-integration.yaml` plus user overrides. A spin's `wiring` and `outputs` are composition metadata layered on top of that per-pack interface, not a replacement for it. The GitOps layout and ArgoCD project in this document record the shipped posture; ADR-0003's original `software_packs:` config key and `software-packs` project are superseded by its Update section.
- **Spin and infrastructure.** A spin may add the first-deployment node groups its packs require. The generated config is operator-owned; later scaling is an ordinary `nic deploy`.
- **Spin and `nic`.** `nic` resolves and materializes the spin during generation, then forgets it. No reconcile path reads a spin.
- **Spin and GitOps.** After generation, the files under `user-apps/` are the desired state for packs.
- **`nebi` is adjacent but out of scope here.** It may eventually manage NIC cluster state, including a deployment's environment and package set. This document defines a spin as the install-time starting composition; whether a `nebi` spec owns or references a spin is a separate question.

## Lifecycle and generated artifacts

**The spin input.**

- A spin is a versioned, standalone manifest resolved from a Git repository or local path—not a NIC config key, embedded binary, or live GitOps object. NIC materializes it into editable provider config and ordinary GitOps artifacts.
- The spin schema is separate from the deployment config: it defines pack composition, cross-pack value wiring, and infrastructure requirements. Existing examples remain hand-authored deployment-config examples, not spin inputs or another source of truth.
- A spin may include first-deployment infrastructure its packs require—for example, a GPU node group for an LLM-serving workload—while allowing the operator to review and edit the generated config before deployment.

**Generation flows.**

- Spins support one-step generation-and-deployment and a generation-only review flow. Both produce the same bundle; a later deploy consumes reviewed GitOps artifacts by default, and regeneration is explicit so operator edits are preserved.
- Generation needs deployment context such as the domain and GitOps repository settings. Remote repositories also need auth environment-variable names. This context can come from flags or a partial config.
- Generation reuses `git.Client`: initialize the repository, write through its work directory, commit and push, then clean up. Remote repositories use a temporary clone; local `file://` repositories use their configured path.

**Generated artifacts and ownership.**

- For each selected pack, generation writes an ordinary Argo CD `Application` to `<git-repository>/<git-path>/user-apps/<id>.yaml`. It includes the namespace, pinned source revision, `project: nebari-apps`, and generated values or Secret references. Operators may also add Applications there.
- `user-apps/` is a top-level sibling of NIC's `apps/` directory. It becomes user-owned when first written. Foundational generation and `--regen-apps` must not touch it; enforce that in the deletion path, as described in [#499](https://github.com/nebari-dev/nebari-infrastructure-core/pull/499).
- Generation never overwrites an existing file under `user-apps/`; it fails and names the path.
- NIC owns `apps/user-apps-root.yaml`, an app-of-apps that discovers Applications under `user-apps/`. Those child Applications use the `nebari-apps` project and remain operator-owned.
- `apps/root.yaml` syncs foundational applications in waves 1–6. `apps/user-apps-root.yaml` takes a later wave, so packs start after the foundational CRDs, gateways, and operators. The child-Application waves under `user-apps/` are a separate sequence used only to order packs relative to one another.

**Provenance and releases.**

- Deploy records spin provenance in `.bootstrapped`: the name, immutable version or digest, and generation timestamp. This is metadata only; no reconcile path reads it. This deliberately extends the marker beyond its skip-path role ([ADR-0001](../../adr/0001-git-provider-for-gitops-bootstrap.md)) rather than adding a second metadata file, so writers must preserve existing fields, and generation-only must not write the file at all because its presence controls the bootstrap skip path.
- CI validates each spin as an immutable pack composition. Reusing the same spin, pack references, and provider mappings in staging and production gives the same composition and wiring; provider configuration and credentials may differ. There is no spin upgrade: every new release must be revalidated, then applied deliberately.

## Provider translation

- A spin's infrastructure requirements materialize as declared node-group definitions under `cluster.<provider>.node_groups` in the generated config. Spins emit explicitly named pools.
- Spins use fixed node groups. Cluster autoscaling and node auto-provisioning are deferred.
- Node-group schemas are provider-specific: AWS declares instance type, GPU, disk, labels, structured taints, and spot; Azure uses string-form taints, zones, and a mode; GCP uses guest accelerators; Hetzner uses `instance_type`, `count`, `master`, and optional autoscaling.
- The spin schema expresses requirements through provider-neutral capabilities and named size tiers. A data-only table maps `(provider, requirement, tier)` to a node-group definition. The table lives in the spin source as versioned data alongside the spin manifests, not in NIC Go code, so NIC core carries no provider-keyed logic and out-of-tree providers ([ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md)) work the same way: a spin ships mappings only for the providers it lists in `providers.supported`. Spin CI validates each mapping against the target provider's node-group config schema. The table needs no credentials or API calls, so adding a size tier does not expand the provider plugin interface.
- Requirements are per pack, while `--size` selects one tier for the whole spin. One spin can therefore emit separate CPU and accelerator pools from a single size selection; per-pack size flags are deliberately avoided.
- Emitting this config is new surface. The deployment-config schema is established and the committed examples are hand-authored; NIC does not write config files today. [ADR-0005](../../adr/0005-nic-config-cli-surface.md) proposes `nic config init` as that writer, so a spin uses the same generation path rather than a separate config format. This design does not gate on that decision: `nic config init` remains deferred in ADR-0005, and if it stays deferred, spin generation ships its own writer for the same config format, which `nic config init` can absorb later.
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

- A spin owns both sides of scheduling: the infrastructure that provides a resource and the pack values that request it. In AWS, `gpu: true` selects the NVIDIA AMI, adds the `nvidia.com/gpu` taint, and installs the GPU Operator before GitOps begins. Pack values must still include the matching toleration, node selector, and GPU resource limit; otherwise nodes can be idle or workloads can remain pending.
- NIC manages this GPU cascade end-to-end only on AWS today. For `existing`, GPU support is an operator-supplied precondition; the spin emits no GPU infrastructure. For `local`, spins emit pack-side artifacts only, so a pack that needs capabilities Kind cannot provide may remain pending.

  | Provider | GPU surface |
  |---|---|
  | AWS | Full cascade: `gpu: true` selects the NVIDIA AMI and applies the `nvidia.com/gpu` taint; GPU Operator install and teardown are gated on the flag |
  | GCP | `guest_accelerators` exists, but the provider is a stub and nothing consumes it |
  | Azure | No GPU field |
  | Hetzner | No GPU field |
  | Local | No node-group configuration |
  | Existing | No NIC-managed node-group configuration; GPU support is operator-supplied |

- Generation checks requirements against provider capabilities. Managed providers reject unsupported requirements before deployment. `local` and `existing` treat infrastructure as an external precondition and may leave workloads pending. Supporting accelerators on another provider also requires provider-specific node provisioning and a conditional GPU Operator/device-plugin lifecycle ([ADR-0006](../../adr/0006-conditional-foundational-software-helm.md)).

## Validation and composition

- A spin uses the same checks as an individual install: catalog membership, source policy where configured, pinned versions or digests, namespace, values schema, and no inline secrets. It is a preset batch of install requests, not a bypass. That shared per-pack validation path does not exist in NIC today; the list above is the target contract for both individual installs and spins, and building it is a prerequisite for spins.
- Cross-pack integration is primarily value wiring. Wiring is a distinct schema key rather than inline output references in pack values: the initial design only sets consumer values, but integration between two packs may eventually need artifacts that belong to neither pack alone—a shared service account or an RBAC grant—and the declared producer-to-consumer edge is where those would attach. A spin may also assign sync waves to the Applications it generates for initial deployment, but that is not a general pack dependency mechanism ([#428](https://github.com/nebari-dev/nebari-infrastructure-core/issues/428)): it does not order packs added later or re-impose order during upgrades.
- Singleton capabilities are declared in the spin and validated at generation. For example, only one pack may own the foundational OpenTelemetry Collector override ([ADR-0008](../../adr/0008-otel-collector-software-pack-override-point.md)). Duplicate claims within a spin fail; conflicts with already-installed packs remain part of per-pack validation, which does not exist yet — until it does, a conflicting claim surfaces only at sync time in ArgoCD.

## Spin sources

- Spin manifests start under `spins/` in this repo and are resolved as versioned data, not compiled into `nic`. A separate `nebari-spins` repo is a later decision, not a precondition.
- A spin source is an ordinary Git repository. `--spin data-science` uses the default first-party source; an explicit `--spin` reference can point to a private or third-party source. All sources use the same checks listed above. First-party sources add curation and CI validation, not extra access.
- Remote sources must use immutable refs, and hardened mode requires digest pinning for fetched inputs ([ADR-0010](../../adr/0010-high-security-mode.md) M-08). Air-gapped installs cannot fetch at generation time, so `--spin` must also accept a local path or internally mirrored repository.

## Worked example: an `llm-chat` spin on AWS

This example is intentionally small. It shows the contract between a spin, two packs, and the AWS provider; it is not a final pack schema or a copy-paste deployment.

The identifiers below are placeholders, especially the chart coordinates, versions, capability names, and Helm values. The spin schema and its provenance fields are proposed. The established pieces are the generated node groups, the AWS `gpu: true` cascade, the `nebari-apps` project, app-of-apps sync waves, and the `.bootstrapped` marker.

The spin combines two ordinary packs:

- `llm-serving` requires an NVIDIA accelerator pool and declares outputs for an internal endpoint and default model.
- `chat` runs on a general-purpose pool and consumes those outputs.

This shows how one input produces both infrastructure and cross-pack values. The packs still own their charts, runtime behavior, and pack-specific validation.

### Spin input

```yaml
apiVersion: spins.nebari.dev/v1alpha1
kind: Spin
metadata:
  name: llm-chat
  version: 0.1.0 # immutable; a new pinned set is a new version

providers:
  supported: [aws]

packs:
  - id: llm-serving
    namespace: nebari-llm-serving-system
    source:
      helm:
        repoURL: example.registry/charts
        chart: llm-serving
        version: 0.1.2
    requires:
      - capability: accelerator.nvidia
        tier: "{{ .Size }}"
        pool: llm-serving-gpu
    values:
      scheduling:
        nodeSelector:
          nebari.dev/pool: llm-serving-gpu
        tolerations:
          - key: nvidia.com/gpu
            operator: Exists
            effect: NoSchedule
        resources:
          limits:
            nvidia.com/gpu: 1
    outputs:
      internalAPIURL: "https://llm-internal.{{ .Domain }}/v1"
      defaultModel: example/model

  - id: chat
    namespace: nebari-chat
    source:
      helm:
        repoURL: example.registry/charts
        chart: chat
        version: 0.3.0
    requires:
      - capability: general
        tier: "{{ .Size }}"
        pool: general
    values:
      hostname: "chat.{{ .Domain }}"

wiring:
  - from: llm-serving
    to: chat
    set:
      llm.baseURL: "{{ .Outputs.internalAPIURL }}" # outputs are scoped to the `from` pack
      llm.model: "{{ .Outputs.defaultModel }}"
```

The `requires` entry is provider-neutral. The `pool` name connects the generated node group to the pack's scheduling values. Wiring sets values at generation time; it does not create a runtime dependency between the Applications. Neither pack claims a singleton capability, so this example does not exercise that part of the schema.

#### Pack interface for wiring

This example wires two ordinary values: the serving endpoint and model name. A real integration may also need a credential; credentials are handled separately below.

The serving pack can publish its endpoints and model names. The chat pack currently exposes only a free-form backend object, with no documented path for those values and no published backend schema. NIC therefore has no stable path it can safely write to.

Before implementing this spin, the consumer pack must publish stable, validated values paths as part of its interface. A free-form passthrough is not enough: operators need to know when a wiring contract changes.

### Resolving the requirements on AWS

With `--provider aws --size small`, a data-only mapping resolves the requirements without cloud API calls:

| Requirement | Generated AWS output |
|---|---|
| `accelerator.nvidia`, `small` | A named `llm-serving-gpu` node group with `gpu: true` and the provider-specific instance, disk, and min/max settings for that tier |
| `general`, `small` | A general-purpose node group selected by the same tier |

The generator validates that every requirement has a mapping for the selected provider and size. For this AWS-only spin, an unsupported combination fails before deployment instead of producing a config that leaves workloads pending.

### Generation and outputs

The proposed generation-only command and the one-step path produce the same artifacts:

```text
nic deploy --spin llm-chat --provider aws --size small

# Or, for review first:
nic config init --spin llm-chat --provider aws --size small
# review and edit config.yaml and the generated GitOps files
nic deploy -f config.yaml
```

The domain, GitOps repository, and credentials are omitted here; they come from flags or a partial config as described above.

The generated GitOps layout is:

```text
<git-path>/
├── apps/                       # NIC-owned foundational applications
│   ├── root.yaml
│   └── user-apps-root.yaml     # NIC-owned app-of-apps
└── user-apps/                  # operator-owned after generation
    ├── llm-serving.yaml
    └── chat.yaml
```

NIC owns `user-apps-root.yaml`; the child Applications are operator-owned. There is no spin controller or runtime spin object.

The relevant generated config carries the infrastructure half of the requirement. It has no `spin:` key:

```yaml
cluster:
  aws:
    region: us-west-2
    node_groups:
      general:
        instance: m7i.xlarge
        min_nodes: 1
        max_nodes: 3
      # emitted for llm-serving's accelerator.nvidia requirement
      llm-serving-gpu:
        instance: g6e.xlarge
        min_nodes: 1
        max_nodes: 2
        disk_size: 300
        gpu: true # NVIDIA AMI, nvidia.com/gpu taint, and GPU Operator install
        labels:
          nebari.dev/pool: llm-serving-gpu # what the pack's nodeSelector targets
```

Once written, this is an ordinary operator-owned NIC config: scaling the pool is a config edit followed by `nic deploy`.

Each file under `user-apps/` carries the pack half. `user-apps/llm-serving.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: llm-serving
  namespace: argocd
  annotations:
    argocd.argoproj.io/sync-wave: "1" # ordered against other packs only
spec:
  project: nebari-apps
  source:
    repoURL: example.registry/charts
    chart: llm-serving
    targetRevision: 0.1.2
    helm:
      releaseName: llm-serving
      values: |
        scheduling:
          nodeSelector:
            nebari.dev/pool: llm-serving-gpu
          tolerations:
            - key: nvidia.com/gpu
              operator: Exists
              effect: NoSchedule
          resources:
            limits:
              nvidia.com/gpu: 1
  destination:
    server: https://kubernetes.default.svc
    namespace: nebari-llm-serving-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

`chat.yaml` has the same shape at wave 2, with the wired endpoint and model in its values. It uses the general pool and has no accelerator requirement. Both are ordinary Applications that an operator can edit or delete; nothing rewrites them.

Deploy-only provenance is stored in `.bootstrapped`, not in the generated Applications:

```yaml
spin:
  name: llm-chat
  version: 0.1.0
  digest: sha256:...
  generated_at: 2026-08-03T14:22:10Z
```

### Ownership boundary

| Concern | Owner after generation |
|---|---|
| Node groups and provider-specific infrastructure | Operator-owned NIC config, applied by NIC |
| Pack selection, versions, namespaces, and cross-pack values | GitOps files under `user-apps/` |
| Routes, certificates, gateway security policies, and Keycloak clients | `nebari-operator`, from the packs' `NebariApp` resources |
| Pack-specific in-process authentication settings | The pack, with values substituted by the spin when needed |
| Spin provenance | `.bootstrapped`, written by deploy only |

Gateway authentication and in-process authentication are different. A pack may need an issuer URL to validate tokens inside its own process, so the spin may pass it as an ordinary chart value. The spin must not create shared routes, certificates, gateway policies, or Keycloak clients; those come from the packs' `NebariApp` resources via `nebari-operator`.

### Ordering the packs a spin generates

The generated files use two sync-wave sequences:

- **`nebari-root` syncs `apps/`.** Foundational Applications use waves 1–6; `user-apps-root` uses a later wave. Packs therefore start after the foundational CRDs, gateways, and operators are healthy.
- **`user-apps-root` syncs `user-apps/`.** Pack Applications use a separate sequence. For example, `llm-serving` can use wave 1 and `chat` wave 2. Those numbers are compared only with other pack Applications, not with the foundational waves.

Argo CD waits for an earlier wave to become healthy before applying the next. In this example, the initial sync can wait for `llm-serving` before creating `chat`. A pack cannot be placed between foundational waves; software that must interleave with the foundation belongs in the foundational set.

This is initial-sync ordering, not a general pack dependency mechanism. Once both Applications exist, `selfHeal` reconciles them independently, and an operator-added pack has no relationship to either one.

Ordering depends on the producer Application's health. If `llm-serving` becomes Healthy when its Deployment starts while its model is still loading, `chat` can start too early. If the GPU pool has no capacity, `llm-serving` never becomes Healthy and `chat` is never created. Pack authors own the health definition and must make clients tolerate later restarts.

### Remaining constraints

- **Credentials need an explicit handoff.** A spin may reference a Secret but must never write its value. A pod cannot read a Secret in another namespace, and NIC provides no Secret distributor, so cross-pack authentication must be handled by the packs or by a separate cluster mechanism.
- **The example is AWS-only.** Other providers need an equivalent accelerator mapping and a lifecycle for the GPU operator. Providers with externally managed capacity need an explicit precondition rather than an implied guarantee.
