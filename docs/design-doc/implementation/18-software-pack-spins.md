# Software Pack Spins: Implementation Design

NIC provides a foundational layer plus operator-selected packs. A spin is a curated, install-time composition of packs and any infrastructure they require. After generation, it becomes an ordinary NIC deployment: operators manage the resulting packs and infrastructure individually, and NIC does not reapply the spin. A spin is a default answer to the composition question, not a new lifecycle; it stops existing once generation finishes.

A durable `spin:` config field is rejected. Pack Applications use `prune: true` and `selfHeal: true`; changing spin membership could remove an existing Application. Generated Applications are user-owned once written, so changing or removing a pack requires an explicit operator edit. A spin version must not change installed software automatically.

## Boundaries

- **Spin and pack.** A spin selects and wires packs; it is not itself a pack and does not install as one. Every pack a spin installs is an ordinary pack afterward, upgraded and removed individually.
- **Spin and infrastructure.** A spin may emit the first-deployment infrastructure its packs require, as declared node groups in generated config. It does not own that infrastructure afterward: the config is operator-owned and subsequent scaling is an ordinary `nic deploy`.
- **Spin and `nic`.** `nic` owns the foundational layer, the config schema, and the generation path. A spin is input data resolved from a repository; `nic` materializes it and then forgets it. No `nic` code path reads a spin at reconcile time.
- **Spin and GitOps.** After generation, desired state for packs is the files under `user-apps/`. The spin is never consulted to interpret them.
- **`nebi` is adjacent but out of scope here.** It may eventually manage NIC cluster state, including a deployment's environment and package set. This document defines a spin as the install-time starting composition; whether a `nebi` spec owns or references a spin is a separate question.

## Lifecycle and generated artifacts

- A spin is a one-time input, not a runtime object, lifecycle mechanism, or pack inventory. It generates ordinary GitOps desired-state files; after generation, GitOps owns packs and NIC config drives infrastructure. Operators manage the generated outputs individually.
- A spin may include first-deployment infrastructure its packs require—for example, a GPU node group for an LLM-serving workload—while allowing the operator to review and edit the generated config before deployment.
- Spins support one-step generation-and-deployment and a generation-only review flow. Both produce the same bundle; a later deploy consumes reviewed GitOps artifacts by default, and regeneration is explicit so operator edits are preserved.
- Generation also needs deployment context, such as the cluster domain and GitOps repository settings. Remote repositories additionally need auth environment-variable names. A spin name alone is not a deployable config; this context can come from flags or a partial config.
- For each selected pack, a spin writes an ordinary Argo CD `Application` to `<git-repository>/<git-path>/user-apps/<id>.yaml`. It includes the pack's namespace, Helm or Git source, pinned version or revision, `project: nebari-apps`, and generated values or secret references. No separate spin descriptor is used. Operators can also place their own Applications in `user-apps/`.
- `user-apps/` is a top-level sibling of NIC's `apps/` directory. Everything under it is user-owned after it is written. Foundational generation and `--regen-apps` must not overwrite or delete it; enforce this in the deletion path, as with the user-owned overlay seam described in [#499](https://github.com/nebari-dev/nebari-infrastructure-core/pull/499), rather than by convention.
- Generation never overwrites an existing file under `user-apps/`; it fails and names the path.
- Generation reuses `git.Client`: initialize the repository, write through its work directory, commit and push, then clean up. Remote repositories use a temporary clone; local `file://` repositories use their configured path. No new Git client operation is needed.
- NIC owns `apps/user-apps-root.yaml`, an app-of-apps that discovers Applications under `user-apps/`. Those child Applications use the `nebari-apps` project and remain operator-owned.
- Foundational apps use sync waves 1–6 under `apps/root.yaml`. `apps/user-apps-root.yaml` takes a later wave in that same sequence, and that is what puts every pack after the CRDs, gateways, and operators it needs. Waves on the pack Applications themselves are a separate sequence, compared only against each other when `user-apps-root` syncs `user-apps/`; they are not in the foundational numbering space.
- A spin is a versioned, standalone manifest resolved from a Git repository or local path—not a NIC config key, embedded binary, or live GitOps object. NIC materializes it into editable provider config and ordinary GitOps artifacts.
- The spin schema is separate from the deployment config: it defines pack composition, cross-pack value wiring, and infrastructure requirements. Existing examples remain hand-authored deployment-config examples, not spin inputs or another source of truth.
- Spin provenance is recorded in `.bootstrapped`—the spin name, immutable version or digest, and generation timestamp—not in live config or per-pack comments. The recorded version is provenance only; no code path reads it as desired state or uses it to reconcile packs. Only deploy writes this marker: a generation-only run must not create it because its presence makes deploy skip foundational manifest generation. The marker writer must merge existing fields so regeneration preserves provenance.
- A spin gives CI a versioned pack composition to validate. Reusing the same spin from staging to production gives the same composition and wiring when the spin, pack references, and provider mappings are immutable; provider configuration, credentials, and other environment-specific inputs may still differ.
- Spins pin the pack versions CI validated together, and there is deliberately no spin upgrade. Once generated, pack versions live in GitOps and are upgraded there per pack. A new spin release is a known-good set to compare and apply deliberately, not a mechanism that reasserts itself on existing clusters.
- Every spin release requires re-validating its pinned set. The roster should grow only as fast as someone can own that curation.

## Provider translation

- A spin states its packs' infrastructure requirements as declared node-group definitions under `cluster.<provider>.node_groups` in the generated config. Spins emit explicitly named pools.
- Spins use fixed node groups. Cluster autoscaling and node auto-provisioning are deferred.
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
- A spin is not privileged. Its infrastructure requirements and packs go through the same checks as individual installs: catalog membership, source policy where configured, pinned versions or digests, namespace, values schema, and no inline secrets. A spin is a preset batch of ordinary install requests, not a path around those checks.
- Cross-pack integration means value wiring, not ordering. There is no pack dependency mechanism ([#428](https://github.com/nebari-dev/nebari-infrastructure-core/issues/428)); pack Applications are independent, and manifest order does not order them. CI validates each spin as a unit, reducing operator burden but not resolving #428.
- Singleton capabilities are declared in the spin and validated at generation. For example, only one pack may own the foundational OpenTelemetry Collector override ([ADR-0008](../../adr/0008-otel-collector-software-pack-override-point.md)). Generation rejects duplicate claims within a spin; conflicts with packs already in the target repository remain part of per-pack validation.
- Spin manifests start under `spins/` in this repo and are resolved as versioned data, not compiled into the `nic` binary. A separate `nebari-spins` repo is a later decision about where the first-party set lives, not a precondition.
- A spin source is an ordinary Git repository, and anyone can author one. `--spin data-science` resolves against the default first-party source; `--spin` also accepts an explicit repository reference, including a private one. Generation runs on the operator's machine, so the source repository uses credentials available there.
- Third-party spins are not privileged by being resolvable. The same pack validation applies to first- and third-party spins: catalog membership, source policy where configured, version or digest pinning, namespace, values schema, and no inline secrets. A first-party spin adds curation and CI validation of the pinned set, not extra access.
- Remote sources must use immutable refs, and hardened mode requires digest pinning for fetched deployment inputs ([ADR-0010](../../adr/0010-high-security-mode.md) M-08). Fetching at generation time is also what an air-gapped install cannot do, so `--spin` must accept a local path or an internally mirrored repository.

## Worked example: an `llm-chat` spin on AWS

This example is intentionally small. It shows the contract between a spin, two packs, and the AWS provider; it is not a final pack schema or a copy-paste deployment.

Read every identifier below as a placeholder: pack ids, chart coordinates and versions, capability and tier names, and in particular every Helm value path. The real packs' value paths differ, and the spin schema itself does not exist yet. What is meant to be load-bearing is the shape of the input and of the generated artifacts, and the NIC and Argo CD behavior they depend on—node groups and the `gpu: true` cascade, the `nebari-apps` project, sync-wave ordering under an app-of-apps, and `.bootstrapped`—all of which are existing behavior.

The spin combines two ordinary packs:

- `llm-serving` requires an NVIDIA accelerator pool and declares outputs for an internal endpoint and default model.
- `chat` runs on a general-purpose pool and consumes those outputs.

The point is to generate the infrastructure and cross-pack values together. The packs still own their charts, runtime behavior, and pack-specific validation.

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
      llm.baseURL: "{{ .Outputs.llmServing.internalAPIURL }}"
      llm.model: "{{ .Outputs.llmServing.defaultModel }}"
```

The `requires` entry is provider-neutral; the `pool` name makes the relationship between the generated node group and the pack's scheduling values explicit. The wiring sets values at generation time. It does not create a runtime dependency between the two Applications.

#### What the consumer has to publish

The values that must cross between these two packs are not a design choice. An OpenAI-compatible endpoint requires a base URL, a model name, and a credential, so those three are the wiring, whatever they end up being called.

The producer side is well defined: the serving pack documents its endpoints and its models are named by their own resources, so a spin can compute both without guessing. The consumer side is where this stalls. The chat pack's chart exposes a single free-form object for the backend's own configuration and validates nothing inside it, so the chart schema names no endpoint key at all, and the backend's configuration schema is not published. There is currently no path a spin could write to.

That makes a stable, documented values path for an upstream endpoint a precondition for this spin, not a detail to settle during implementation. A spin can only wire into a surface a pack has committed to; a free-form passthrough is not one, because nothing tells the operator when the key beneath it is renamed. The general form of the requirement: a pack that expects to be wired must publish the values path, and treat it as part of its interface.

### Provider translation

With `--provider aws --size small`, a data-only mapping resolves the requirements without cloud API calls:

| Requirement | Generated AWS output |
|---|---|
| `accelerator.nvidia`, `small` | A named `llm-serving-gpu` node group with `gpu: true` and the validated instance, disk, and size limits for that tier |
| `general`, `small` | A general-purpose node group selected by the same tier |

The generator validates that every requirement has a mapping for the selected provider and size. Unsupported provider/capability combinations fail before deployment; they do not produce a config that quietly leaves workloads pending.

### Generation and outputs

The proposed generation-only command and the one-step path produce the same artifacts:

```text
nic deploy --spin llm-chat --provider aws --size small

# Or, for review first:
nic config init --spin llm-chat --provider aws --size small
# review and edit config.yaml and the generated GitOps files
nic deploy -f config.yaml
```

Deployment context such as the domain, GitOps repository, and credentials is omitted here; it comes from flags or a partial config as described above.

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

Each file under `user-apps/` is an ordinary Argo CD `Application` with a pinned source revision, destination namespace, `project: nebari-apps`, and rendered values. `user-apps-root.yaml` is the only NIC-owned object in this seam; it discovers those child Applications. There is no spin controller or runtime spin object.

The generated provider config contains explicit node groups and no `spin:` key. Once written, it is an ordinary operator-owned NIC config: changing a generated node group or scaling a pool is an ordinary config edit followed by `nic deploy`.

### Ownership boundary

| Concern | Owner after generation |
|---|---|
| Node groups and provider-specific infrastructure | Operator-owned NIC config, applied by NIC |
| Pack selection, versions, namespaces, and cross-pack values | GitOps files under `user-apps/` |
| Routes, certificates, gateway security policies, and Keycloak clients | `nebari-operator`, from the packs' `NebariApp` resources |
| Pack-specific in-process authentication settings | The pack, with values substituted by the spin when needed |
| Spin provenance | `.bootstrapped`, written by deploy only |

The two authentication concerns are different. A pack may need an issuer URL to validate tokens inside its own process, so the spin may pass that ordinary value through its chart values. The spin must not create shared routes, certificates, gateway policies, or Keycloak clients; those remain the operator's responsibility.

Deploy records the spin name, immutable version or digest, and generation timestamp in `.bootstrapped`. A generation-only run must not write the marker, and updating it must preserve existing fields.

### Ordering the packs a spin generates

Because the spin writes both Applications, it can order them with the mechanism the foundational layer already uses: sync waves, which order resources within one Application's sync operation and wait for each wave to be Synced and Healthy before applying the next. An `Application` is itself a resource with a health assessment, so a parent app-of-apps waiting on a wave of child Applications is waiting on those children's own health. That is what orders foundational services today.

Ordering here happens at two levels, and conflating them is the easy mistake:

- **`nebari-root` syncing `apps/`** places `user-apps-root` at a wave after the foundational 1–6, so no pack is created until the gateway, cert-manager, Keycloak, and `nebari-operator` are healthy.
- **`user-apps-root` syncing `user-apps/`** then orders the packs among themselves. This is a separate numbering space: `llm-serving` at wave 1 and `chat` at wave 2 is sufficient, and `user-apps-root` will not create `chat` until `llm-serving` reports Healthy. Large numbers chosen to look "after foundational" buy nothing, because these waves are never compared against the foundational ones.

A consequence worth stating: a pack cannot be ordered *between* two foundational waves. Everything under `user-apps/` lands after all of them. Software that genuinely must interleave belongs in the foundational set, not in a spin.

Two alternatives were considered and are not worth their cost. A PreSync hook Job on `chat` that polls the serving endpoint gives the same guarantee but adds an image to maintain, a timeout policy to choose, and a Job that reruns on every sync. A custom Lua health check in `argocd-cm` is the wrong tool for ordering; it describes the health of a resource kind Argo CD cannot assess on its own.

The caveats matter more than the mechanism:

- **Wave ordering is only as good as the health assessment it waits on.** If the serving pack's Application reports Healthy once its Deployment is up, while a model is still pulling weights onto a fresh PVC, `chat` starts against an endpoint that does not serve yet. Making Healthy mean "a model is actually serving" requires a health definition for the pack's own CRs, which belongs to the pack.
- **Ordering applies to the parent's sync operation, not to the cluster afterward.** Once both Applications exist, `selfHeal` reconciles them independently; waves do not re-impose order when the serving pack is later upgraded or briefly unhealthy. Packs must tolerate their dependency restarting.
- **A blocked dependency blocks the consumer entirely.** If the GPU pool has no capacity, `llm-serving` never goes Healthy and `chat` is never created. That is usually the behavior an operator wants, but it should be a documented consequence rather than a surprise.
- **This does not resolve [#428](https://github.com/nebari-dev/nebari-infrastructure-core/issues/428).** It orders the Applications one spin writes at generation time. It is not a pack dependency mechanism: a pack an operator adds to `user-apps/` by hand later gets no ordering, because nothing at runtime knows the relationship.

### Remaining constraints

- **Most wiring is plain values; a credential is the exception.** Endpoints, hostnames, storage classes, and group names are ordinary strings, and this example needs a credential only because the serving pack gates access per model. Where one is needed, a spin emits a reference and never a value: deploy can create the Secret in-cluster, as NIC already does for its own Argo CD and Longhorn client secrets. It cannot have one pack read another's Secret, since a pod may only reference Secrets in its own namespace and NIC ships no distributor. How two packs authenticate to each other is otherwise the packs' design, not the spin's.
- **The example is AWS-only.** Other providers need an equivalent accelerator mapping and a lifecycle for the GPU operator. Providers with externally managed capacity need an explicit precondition rather than an implied guarantee.

