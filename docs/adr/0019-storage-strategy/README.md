# ADR-0019: Nebari Storage Strategy

## Status

Proposed

The design this ADR selects, and the storage contract software packs rely on, are described in the [storage design doc](../../design-doc/architecture/storage.md). This directory also holds the ADR's argument map: the [Argdown source](storage-strategy.argdown) and the rendered maps embedded below.

## Date

2026-09-22

## Context

Nebari Infrastructure Core (NIC) provisions a Kubernetes cluster and its foundational services, and software packs add workloads on top. The Data Science Pack (DSP) is the main storage consumer. It runs JupyterHub, which starts each user's JupyterLab server and jhub-apps applications (dashboards and apps launched from the hub) as pods. Those pods need four kinds of persistent data:

- a private **home** directory (`/home/jovyan`) for notebooks, source code, configuration, and user state;
- a **workspace** volume for materialized software environments, managed by Nebi, Nebari's pixi-based environment manager;
- **shared** POSIX directories (`/shared/{group}`) for collaboration within groups; and
- **object storage** for large datasets and other data that does not need filesystem semantics.

These have different access, performance, durability, and scheduling needs. Serving them all from one implementation keeps the platform simple to configure, but forces every workload onto that implementation's compromises.

Two Kubernetes access modes drive most of what follows. A **ReadWriteOnce (RWO)** volume attaches to one node at a time, so every pod that mounts it must run on that node. A **ReadWriteMany (RWX)** volume is served over the network, usually as NFS, to any number of nodes, at the cost of slower metadata operations such as `stat`, `create`, and `rename`.

### Why Nebari uses Longhorn

AWS EBS volumes are bound to one Availability Zone (AZ). NIC runs one autoscaling group across several AZs, and Cluster Autoscaler cannot ask that group for capacity in a specific AZ, so a pod whose EBS-backed home sits in one AZ can stay `Pending` while the cluster adds nodes in another. [ADR-0002](../0002-longhorn-distributed-block-storage-for-aws.md) selects Longhorn, an in-cluster replicated block-storage system, to mask that problem: it replicates each volume across nodes, so a volume can follow its pod to another node or AZ. It also gives Nebari one storage implementation across AWS, Hetzner, and on-premises-style deployments where no managed shared filesystem is available.

Longhorn now fills three roles, and none of them requires the others:

1. It is the default StorageClass, including for single-pod RWO volumes.
2. It provides RWX volumes by exporting a replicated volume over NFS from a share-manager pod.
3. It provides NIC's only implemented backup and restore path.

The storage decision is therefore broader than choosing an RWX backend: it determines which workloads use Longhorn, how pods are scheduled, what happens during an AZ outage, how backups work, and how much storage infrastructure operators own.

### What Longhorn costs

- **Every user is pinned to one node.** Home and workspace are RWO, and each of a user's lab and app pods mounts both, so DSP adds required pod affinity that places all of them on the same node. That node's spare capacity becomes the user's ceiling: a GPU lab cannot start while one of the user's apps holds them on a CPU node, and a node cannot scale down while anything is pinned to it ([DSP #221](https://github.com/nebari-dev/data-science-pack/issues/221)).
- **Storage-aware operations.** Draining a node means disabling Longhorn scheduling, evicting its replicas, and waiting for them to move first, and diagnosis needs Longhorn's own vocabulary of engines, replicas, and faulted volumes. The cost model estimates ~9.3 operator hours per cluster per month, against ~2.3 for a managed filesystem.
- **Infrastructure cost.** Two replicas plus a 25% disk reserve, a dedicated storage node group, and roughly 12% of each user node's CPU reserved for Longhorn's `instance-manager` make Longhorn 2.5-2.7x the cost of the managed-shared shape, D.
- **Compute model.** Longhorn needs iSCSI, privileged pods, and host access on every node, which rules out EKS Auto Mode and GKE Autopilot.

## Decision Drivers

These are the settled requirements every option has to satisfy. They come from current DSP workloads, observed failures, and the provider architecture, set out under [DSP Storage Needs](#dsp-storage-needs) and [The User Node Pin](#the-user-node-pin); they do not select an implementation. Everything still open appears as an open decision in the argument map.

- **RWX is a required capability, not a cluster-wide default.** Group members must read and write one POSIX path while their pods stay independently schedulable, but each pack requests the access mode its own workload needs. NIC therefore needs a per-workload StorageClass surface, because DSP's home and workspace PVCs currently inherit the cluster default.
- **Distributed workers must reach the user's environment and files across nodes.** Either the home satisfies a multi-node contract (an RWX home plus a workspace that no longer requires single-node attachment), or explicit alternatives cover each direction: environments as prebuilt bundles or images, inputs broadcast to workers, results copied back to the user's volume after the job. Group-level independence is settled by the RWX requirement above; releasing the *per-user* node pin is the open home-access-mode trade, not a settled requirement.
- **POSIX semantics only where the contract requires them.** Large datasets and immutable artifacts go to object storage; homes, outputs, environments, and shared small-file work go on volumes. Cross-pack sharing defaults to object storage for the same reason.
- **Volume storage is not the bulk data path for distributed compute.** High-concurrency Ray and Dask read from object storage or a parallel filesystem, so one workload cannot make unrelated homes unresponsive. Per-user volumes keep a saturating job inside one user's failure domain.
- **Cross-namespace sharing requires real RWX.** Two PVCs on one RWO volume is an undeclared single-node pin that Longhorn under-reports, so drain checks get an incomplete answer. Layout counts too: `/shared` must be split per group or subpath-scoped before another namespace mounts it.
- **The contract describes observable behavior, not one product.** Access mode, POSIX semantics, durability, availability, and performance are defined at the pack boundary ([storage contract](../../design-doc/architecture/storage.md#storage-contract)); each provider supplies an implementation. Longhorn stays supported for Hetzner regardless of the AWS choice.
- **Every volume needs durability coverage under whatever backend holds it.** Longhorn is NIC's only implemented backup path, so anything moving off it needs replacement backup, retention, and restore-into-a-new-cluster procedures. Keycloak's single CNPG instance needs multi-instance replication, verified zone placement, and a backup path before its database moves to gp3.
- **AZ-bound volumes require compute in their AZ.** Removing Longhorn does not make EBS portable, so per-AZ node groups or a topology-aware provisioner such as Karpenter is a prerequisite, not an optimization.

## Considered Options

Seven AWS storage shapes, each assigning every volume to a backend. [Options Detail](#options-detail) compares them side by side.

1. **A. Incumbent:** Longhorn for every volume.
2. **B. System split:** Keycloak's database on gp3; user volumes and `/shared` stay on Longhorn.
3. **C. Longhorn only for RWX:** homes, workspaces, and the database on gp3; `/shared` on Longhorn.
4. **D. Managed shared:** `/shared` on cloud-managed RWX; homes and workspaces on gp3.
5. **E. Managed homes:** homes and `/shared` on cloud-managed RWX; workspaces ephemeral.
6. **F. Published apps:** gp3 homes mounted only by the lab; app pods pull published bundles.
7. **G. Managed homes, published environments:** homes and `/shared` on cloud-managed RWX and mounted by every user pod; environments rebuilt from lockfiles on node-local disk.

## Decision Outcome

Chosen option: **G. Managed homes, published environments**, on every cluster whose provider offers a cloud-managed RWX filesystem: a network filesystem the cloud operates, which every node in the region can mount read-write at the same time.

> **Files cross between pods on a shared filesystem. Environments cross as a lockfile and are rebuilt on node-local disk.**

- Each user's home and each group's `/shared` directory live on cloud-managed RWX and mount into every pod the user runs, including app pods. No user pod mounts a block volume, so the per-user node pin goes away.
- The per-user workspace volume goes away. Environment specs (`pixi.toml`, `pixi.lock`) stay in the home and are pushed to Nebi server; each pod builds its environment on node-local disk from the lockfile, keeping the metadata-heavy install off the network filesystem.
- Ray workers can mount the user's home through an authorized claim in their own namespace, and anything that fans out across workers starts from a prebuilt image.
- Datasets, models, and job results live in object storage.
- Platform databases (Keycloak, Nebi server) run on CloudNativePG (CNPG) over block storage, with HA and backup at the database layer.
- Single-writer datastores that packs bring without an operator (FiftyOne's MongoDB is the first) run on a CSI block StorageClass and are backed up by scheduled CSI volume snapshots with a `Delete` deletion policy, so expired snapshots are removed with their objects; see [Block volumes outside CNPG](../../design-doc/architecture/storage.md#block-volumes-outside-cnpg).
- Longhorn remains the RWX implementation where no managed filesystem exists, as on Hetzner and on-premises clusters.

This ADR does not select a managed RWX service. On AWS, EFS and FSx for OpenZFS both satisfy the design, and [Choosing a managed RWX service on AWS](#choosing-a-managed-rwx-service-on-aws) records how they compare. How the design works, what persists and what does not, the storage contract, and migration are in the [storage design doc](../../design-doc/architecture/storage.md).

### Consequences

**Good:**

- **The node pin goes away.** A user's lab and apps schedule independently. With a regional or multi-AZ filesystem, no user volume is bound to an AZ, so user pods need no per-AZ capacity either.
- **Apps see live files.** Edit a file in the lab, reload the app, and the change is there, with no publish step for code.
- **Environment builds stay at block-storage speed**, because they never touch the network filesystem.
- **Longhorn leaves AWS.** No storage node group, no storage-aware drains, and EKS Auto Mode becomes possible. Estimated operator time drops from ~9.3 to ~2.3 hours per cluster per month.
- **Ray can reach the user's home.** Unlike any block-backed shape, managed RWX lets a Ray worker in another namespace mount the user's home, although the per-user claim that does so is not built yet.

**Bad:**

- **Slower home file operations.** On the services tested, metadata-heavy work on the home (untar, clone, checkout) ran 26-57x slower than gp3 on FSx for OpenZFS Multi-AZ, and slower still on EFS. `git checkout` goes from 0.32 s to about 18 s. Notebook autosave stays short.
- **No infrastructure saving.** The cost model, which prices this shape on FSx for OpenZFS Multi-AZ, puts it at about the incumbent's cost: $231 / $1,083 / $6,307 per month at 10 / 50 / 200 users, against $231 / $1,045 / $6,235. The saving is operator time and a smaller failure surface.
- **The workspace path becomes disposable.** `/var/lib/nebi/workspaces` persists today and is rebuilt on every start under this design. Anything a user saves there that is not part of the environment is lost on the next cull, restart, or node drain, so it has to move to the home or object storage first, and users have to be told.
- **Nebi server becomes a runtime dependency.** Nebi local mode keeps a SQLite database in the home in WAL (write-ahead log) mode, which requires every client to run on one host and cannot work on a network filesystem. Server mode resolves that, but Nebi is alpha and not recommended for production.
- **New work NIC owes.** A controller that creates a per-user directory and claim on the shared filesystem; a replacement backup and restore path for homes and `/shared`; a CSI block StorageClass, the snapshot CRDs and controller, a `VolumeSnapshotClass`, and a snapshot scheduler for block volumes outside CNPG; multi-instance CNPG for Keycloak; and a tested per-user migration of every existing home.

### Why not the other options

Shapes A through C keep Longhorn on AWS, and with it the node pin, the storage-aware operations, and the exclusion from EKS Auto Mode. Shape D (managed `/shared`, gp3 homes) is the cheapest measured shape and keeps homes fast, but keeps the pin and adds per-AZ capacity work. Shape E (managed homes) removes the pin but leaves environment builds on the network filesystem, where the RWX penalty is worst. Shape F (gp3 homes mounted only by the lab, apps built from published bundles) keeps block-speed homes, but apps see the last publish rather than live files, and it depends on a write-back contract and a user publish path that do not exist. G takes E's backends and F's environment delivery; [its detail](#g-managed-homes-published-environments) has the full comparison.

## Open Questions

- **Can DSP depend on Nebi?** The design makes Nebi server the durable authority for workspace specs. If packs must stay independently deployable, the design needs another durable home for them. This is the first thing to settle; the [design doc](../../design-doc/architecture/storage.md#can-data-science-pack-depend-on-nebi) sets out both branches.
- **Which managed service each cloud uses.** On AWS, EFS is integrated in NIC today; FSx for OpenZFS was faster on home metadata operations (checkout in about 18 s on Multi-AZ against 32 s on EFS) and is cheaper at scale in the model, but NIC would own its integration. Azure and GCP have not been evaluated.
- **Ray tenancy.** A shared RayCluster serves many users, while homes and workspaces belong to one. The launch path must carry the user's identity, authorized home, and environment into the job, or Nebari runs per-user or per-job Ray clusters.
- **Performance thresholds.** There is no agreed latency threshold for home operations, and `/shared` has not been tested beyond four concurrent writers.

## Acceptance Gates

The decision is tested against the drivers by closing these questions explicitly for each cloud's implementation:

- **POSIX behavior:** concurrent reads and writes, locking, atomic rename, ownership, permissions, setgid propagation, `subPath`, and failure recovery.
- **Performance:** an agreed latency threshold for home operations, concurrency testing beyond four writers for `/shared`, and a load-isolation test showing that distributed compute cannot make homes or collaboration storage unavailable.
- **Availability:** node failure, AZ failure, endpoint failover, remount behavior, lock recovery, and stated recovery objectives.
- **Backup and restore:** coverage for every volume, retention, credentials, and restore into a new cluster.
- **Operations:** installation, upgrades, observability, capacity changes, incident response, and security patching.
- **Cost:** realistic customer sizes, shared-storage I/O, request charges, cross-AZ traffic, backups, compute overhead, and operator time.
- **Migration:** a tested path for every user volume that changes backend or access mode.
- **Portability:** the permanent cost of Longhorn plus each provider-native implementation.

## How to Read the Diagrams

In the shape diagrams, orange outlines mark Longhorn components, green outlines mark managed services, blue outlines mark artifact registries, and red dashed outlines mark work the shape still owes. The Ray data-flow diagram uses its own key: green for the mounted home volume, blue for artifact registries, orange for bulk data stores. The Argdown maps use this legend:

[![Legend](legend.svg)](legend.svg)

[`overview.svg`](overview.svg) renders the complete argument map from [`storage-strategy.argdown`](storage-strategy.argdown); the topic maps embedded below render smaller sections of the same argument.

## DSP Storage Needs

### Volumes today

The current DSP storage layout has four volume shapes:

| Volume | Access mode | Contents | Consumers |
| --- | --- | --- | --- |
| `claim-{user}` | RWO | `/home/jovyan`, including Nebi local mode's SQLite database at `~/.local/share/nebi/nebi.db`, opened in WAL mode | every pod run by that user; Ray workers once the planned interim mount lands |
| `nebi-workspaces-{user}` | RWO | materialized environments; 20 GiB by default | mounted into every pod run by that user, though the jhub-apps path re-materializes into an `emptyDir` rather than reading it |
| `/shared/{group}` | RWX | POSIX files shared by group members | pods belonging to multiple users |
| Keycloak CNPG | RWO | identity database | one database instance per volume |

### Group directories require RWX

The group directory creates one hard RWX requirement. Multiple users must be able to read and write the same POSIX path while their pods remain independently schedulable. RWO can serve several pods only when they all run on the same node; applying that constraint to a group creates a hard capacity ceiling and composes badly when users belong to several groups.

### Object storage and volumes

Object storage complements the filesystem rather than replacing it. It is the appropriate default for large datasets and immutable artifacts, but it does not provide the POSIX rename, locking, directory, and permission behavior that existing notebooks and tools expect from `/shared`. Nebari also lacks a per-user AWS identity and permission layer for mapping Keycloak users and groups to object-storage access. The preferred boundary is therefore:

- use object storage for large datasets and other object-native workflows;
- use volume storage for homes, user outputs, environments, and shared small-file workflows that require POSIX behavior.

That boundary is also a scale boundary, and it rests on observed failures rather than caution: the saturation cases under [Shared-data path](#shared-data-path) took down both an in-cluster NFS share and a managed EFS whose performance tier had already been raised. Volume storage is a convenience path for exploratory, single-node, and small-scale work; high-concurrency distributed compute reads bulk data from object storage or a parallel filesystem. Per-user volumes narrow the failure domain: a job that saturates its own home harms one user, while one shared filesystem behind every home lets it affect the whole deployment.

### Ray and cross-pack sharing

Ray is the second live consumer. The agreed interim plan mounts a user's home into Ray workers so exploratory jobs can reuse local files without publishing everything to object storage first; [rayserve-pack #30](https://github.com/nebari-dev/rayserve-pack/issues/30) tracks it and is blocked on making that mount work across namespaces. A single-node Ray job can run on the current RWO home. A distributed cluster places workers in another namespace and across several nodes, so a production design needs multi-node home access or an explicit alternative for each direction: environments delivered as prebuilt bundles or images, inputs broadcast or copied to workers, and results copied back to the user's volume after the job. The requirement is settled; the design is not.

Pointing PVCs in two namespaces at one Longhorn RWO volume was tested and rejected. It works while the pods share a node, which is the problem: RWO attaches per node, so the arrangement is an undeclared single-node pin spanning two packs, and it takes a cluster-admin static `PersistentVolume` to build rather than anything a tenant reaches by accident. Longhorn's `Volume` CR also records only one of the two consumers, so a drain check gets an incomplete answer. Cross-namespace access needs a real RWX volume. The full test and its findings are in [this comment on NIC #597](https://github.com/nebari-dev/nebari-infrastructure-core/issues/597#issuecomment-5356169820).

Cross-pack sharing is a live dependency. [NIC #597](https://github.com/nebari-dev/nebari-infrastructure-core/issues/597) covers cross-namespace volume access and [NIC #598](https://github.com/nebari-dev/nebari-infrastructure-core/issues/598) object-storage access; [rayserve-pack #30](https://github.com/nebari-dev/rayserve-pack/issues/30), which would expose head and worker `volumes` and `volumeMounts` in the chart, is blocked on the first. The real requirement is narrower than that chart change: not arbitrary volumes, but the requesting user's home plus the group directories that user is entitled to, resolved per user at cluster launch. No static `values.yaml` field can express that, so it needs a launch-time layer that resolves each user's entitlements, a gateway that does not exist yet. It also constrains layout, not just backend: `/shared` is one RWX PVC with group directories beneath it, so mounting it from another namespace exposes every group unless the volume is split per group or the claim is subpath-scoped.

Ray environment delivery is a separate open problem. Ray's own mechanism, `runtime_env`, has each worker resolve and install dependencies again, and pip can resolve differently there than it did locally. Nebi already publishes workspace bundles through any OCI registry (Artifact Keeper is just another configured registry); each content-addressed bundle carries `pixi.toml`, `pixi.lock`, and optional assets, so every consumer still runs `pixi install`. Nebi does not yet support `[tool.pixi.*]` tables in `pyproject.toml`. An environment-baked container image remains a proposal: bundles pay install time per pod, baked images pay pull time per node, and neither profile is measured.

```mermaid
flowchart LR
  subgraph dsp["DSP namespace"]
    jp["Jupyter pod"]
    hp["user home PVC"]
    jp <-->|"notebooks, scripts, config"| hp
  end

  subgraph ray["Ray namespace"]
    rp["Ray head and workers (n)"]
  end

  hp -->|"RWX or equivalent cross-namespace mount<br/>small mutable user files"| rp
  oci["any OCI registry<br/>(including configured Artifact Keeper)"] -->|"Nebi spec bundle<br/>then pixi install per pod"| rp
  img["container registry"] -->|"proposed baked image<br/>pull and run per node"| rp
  jp <-->|"object-native datasets and outputs"| os["object storage or parallel filesystem"]
  os <-->|"bulk distributed data"| rp

  classDef mounted stroke:#5c9e6f,stroke-width:2px
  classDef artifact stroke:#2b6cb0,stroke-width:2px
  classDef data stroke:#b3762f,stroke-width:2px
  class hp mounted
  class oci,img artifact
  class os data
```

[![Is RWX required?](rwx-required.svg)](rwx-required.svg)

## The User Node Pin

DSP adds required pod affinity so every pod belonging to one user runs on the same node. This lets all of those pods mount the same RWO home and workspace PVCs, but it makes that node's remaining capacity the maximum capacity available to the user.

The pin reaches fewer pods than "every pod the user causes to exist". It is set through `KubeSpawner.extra_pod_config` and selects on a label DSP stamps on the pods it spawns, so it covers the JupyterLab pod and jhub-apps app pods and nothing else. Pods that another pack's operator creates, such as KubeRay workers from a RayService spec in their own namespace, never carried the label and were never co-located. Distributed Ray has therefore never reached a user's home or environment on any shape: today it is either local inside the lab pod, where it has both because it is that pod, or the shared cluster, which has neither and receives code through `runtime_env`. Releasing the pin is about the lab and app pods; Ray is a separate problem that no access mode resolves on its own.

[DSP #221](https://github.com/nebari-dev/data-science-pack/issues/221) records the resulting scheduling failures:

- a GPU JupyterLab cannot start while one of the user's applications is pinned to a CPU node;
- an application pinned to a GPU node prevents that node from scaling down after the lab stops; and
- all concurrent workloads for one user must fit on one node even when the cluster has free capacity elsewhere.

Changing the home to RWX does not remove the pin by itself. The workspace PVC is also RWO and is mounted into every user pod. Releasing the pin therefore requires both an RWX home and a workspace that no longer requires single-node attachment, such as an ephemeral node-local volume. Shape F reaches the same end from the other side: leave home on block storage and remove every other consumer, delivering published bundles to app pods instead. Shape G satisfies both halves at once, with an RWX home that every pod mounts and an environment path that never touches it.

## Options Detail

Seven AWS shapes capture the meaningful choices, and shape G is the chosen option. Shapes B through E each change a further workload rather than varying one design; shape F keeps D's backends and changes what app pods consume instead. Shape G crosses E and F: E's managed RWX backends with F's per-pod environment delivery, keeping the home mount that F gives up.

| | **A. Incumbent** | **B. System split** | **C. Longhorn RWX only** | **D. Managed shared** | **E. Managed homes** | **F. Published apps** | **G. Managed homes, published environments (chosen)** |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Home | Longhorn RWO | Longhorn RWO | gp3 RWO | gp3 RWO | managed RWX | gp3 RWO, lab pod only | managed RWX, every user pod |
| Workspace | Longhorn RWO | Longhorn RWO | gp3 RWO | gp3 RWO | ephemeral | gp3 RWO, lab pod only | ephemeral, node-local by configuration |
| `/shared` | Longhorn RWX | Longhorn RWX | Longhorn RWX | managed RWX | managed RWX | managed RWX | managed RWX |
| Keycloak CNPG | Longhorn RWO | gp3 RWO | gp3 RWO | gp3 RWO | gp3 RWO | gp3 RWO | gp3 RWO |
| Longhorn on AWS | all volumes | homes, workspaces, and `/shared` | `/shared` only | no | no | no | no |
| Cost model, using FSx Single-AZ in D and F, Multi-AZ in E and G: 10 / 50 / 200 users | $231 / $1,045 / $6,235 | approximately A | est. $182 / $589 / $3,032 | $86 / $423 / $2,466 | $231 / $1,083 / $6,307 | approximately D | approximately E |
| Operator time per month | ~9.3 h | ~9.3 h | ~9.3 h | ~2.3 h | ~2.3 h | ~2.3 h | ~2.3 h |
| Home write latency vs gp3 | approximately block baseline | approximately block baseline | block baseline | block baseline | 26-57x block baseline | block baseline | 26-57x for home file operations; environment builds at block baseline |
| `/shared` with FSx, one writer vs Longhorn RWX | Longhorn baseline | Longhorn baseline | Longhorn baseline | within benchmark noise | within benchmark noise | within benchmark noise | within benchmark noise |
| `/shared` with FSx, four writers vs Longhorn RWX | Longhorn baseline | Longhorn baseline | Longhorn baseline | 1.2-1.4x slower | 1.2-1.4x slower | 1.2-1.4x slower | 1.2-1.4x slower |
| User node pin | yes | yes | yes | yes | no | no, except concurrent named servers | no |
| AZ and database HA work | no | CNPG multi-instance with zone placement | per-AZ groups or Karpenter, plus CNPG multi-instance | per-AZ groups or Karpenter, plus CNPG multi-instance | CNPG multi-instance with zone placement | per-AZ groups or Karpenter, plus CNPG multi-instance | CNPG multi-instance with zone placement |
| Backup coverage | all volumes | excludes system volumes | excludes homes, workspace, and system volumes | new implementation required | new implementation required | new implementation required | new implementation required |
| User-data migration | no | no | yes | yes | yes | yes | yes |
| EKS Auto Mode possible | no | no | no | yes | yes | yes | yes |
| Managed RWX integration required | no | no | no | yes | yes | yes | yes |
| Nebi workspace coordination | unchanged: local mode | unchanged: local mode | unchanged: local mode | unchanged: local mode | server mode required; Nebi is alpha | local mode; apps pull published bundles | server mode required; apps pull environments into a per-pod `emptyDir` |

The C estimate is derived from the model's unit prices rather than from a modeled scenario. It assumes Longhorn's `instance-manager` CPU reservation disappears from user nodes when they no longer attach Longhorn volumes. The model also excludes the 20 GiB workspace PVC from every shape; including it increases each gp3 total slightly and each Longhorn total by the 2.67x replica-and-reserve multiplier.

### A. Incumbent: Longhorn for every volume

The incumbent keeps the current AWS behavior. It avoids migrations and preserves one backup path, while retaining Longhorn's node-drain procedure, capacity tuning, dedicated storage nodes, and cluster-wide failure vocabulary. Because Longhorn is the default StorageClass, new unclassified PVCs also inherit it whether or not they need replication or RWX.

```mermaid
flowchart LR
  subgraph n["user node · pinned by two RWO mounts"]
    pod["jupyter pod"]
  end
  pod -->|/home/jovyan| h["home PVC · RWO"]
  pod -->|workspaces| w["workspace PVC · RWO"]
  pod -->|/shared/group| sh["shared PVC · RWX"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  h --> LH
  w --> LH
  sh --> sm["share-manager NFS pod"] --> LH
  sys --> LH
  LH["Longhorn · default StorageClass"] --> sn["storage node group · 2 replicas per volume"]
  LH --> bk["Longhorn backups to S3"]
  classDef lh stroke:#b3762f,stroke-width:2px
  class LH,sm,sn,bk lh
```

### B. System split: native block for system volumes

This shape moves the Keycloak database to gp3 while leaving both user volumes and `/shared` on Longhorn. It avoids user-data migration and keeps the current scheduling behavior for users. Keycloak currently runs as one CNPG instance, so this shape must also configure multiple instances with zone-aware placement and provide a database backup strategy. Replication supplies failover only after that work exists, and it still does not protect against logical corruption or deletion.

The shape reduces little of Longhorn's cost or operational burden. Its architectural value is conditional: once CNPG provides database-level high availability, Longhorn replication beneath it becomes unnecessary for failover. Until then, moving the database to gp3 removes both cross-AZ attachment and the current storage-layer redundancy.

```mermaid
flowchart LR
  subgraph n["user node · pinned by two RWO mounts"]
    pod["jupyter pod"]
  end
  pod -->|/home/jovyan| h["home PVC · RWO"]
  pod -->|workspaces| w["workspace PVC · RWO"]
  pod -->|/shared/group| sh["shared PVC · RWX"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  h --> LH
  w --> LH
  sh --> sm["share-manager NFS pod"] --> LH
  sys --> gp3["EBS gp3 · AZ-bound"]
  LH["Longhorn · still default"] --> sn["storage node group"]
  LH --> bk["Longhorn backups to S3"]
  gp3 -.-> owed["owed: CNPG multi-instance,<br/>zone placement, backup"]
  classDef lh stroke:#b3762f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  class LH,sm,sn,bk lh
  class owed gap
```

### C. Longhorn only for RWX

This shape applies the access-mode boundary consistently. Homes, workspaces, and the Keycloak database use gp3; only `/shared` uses Longhorn.

It avoids using a managed RWX service and preserves Longhorn backup coverage for shared group data. It also moves every home, requires AZ-aware node provisioning, requires multi-instance CNPG for Keycloak availability, and removes homes and system volumes from the existing backup set. Longhorn remains installed, so its drain, upgrade, capacity, and incident-response procedures remain part of every AWS cluster even though they apply to less data.

```mermaid
flowchart LR
  subgraph n["user node · pinned by two RWO mounts"]
    pod["jupyter pod"]
  end
  pod -->|/home/jovyan| h["home PVC · RWO"]
  pod -->|workspaces| w["workspace PVC · RWO"]
  pod -->|/shared/group| sh["shared PVC · RWX"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  h --> gp3["EBS gp3 · AZ-bound"]
  w --> gp3
  sys --> gp3
  sh --> sm["share-manager NFS pod"] --> LH["Longhorn · RWX class only"]
  LH --> sn["storage node group · /shared replicas only"]
  LH --> bk["Longhorn backups · /shared only"]
  gp3 -.-> owed["owed: per-AZ capacity,<br/>CNPG HA, backup for gp3 volumes"]
  classDef lh stroke:#b3762f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  class LH,sm,sn,bk lh
  class owed gap
```

### D. Managed `/shared`, block-backed homes

This shape puts RWO workloads on gp3 and `/shared` on an AWS-managed RWX service. It removes Longhorn from AWS while preserving fast block storage for interactive home workloads. EFS and FSx for OpenZFS are implementation candidates for the same shape.

The benchmark favors the RWO half of this boundary and is neutral to mildly negative on the RWX half; the FSx cost model is what makes it the cheapest measured implementation. It requires a managed-RWX StorageClass and workload routing, a provider-specific backup and restore path, per-AZ node groups or Karpenter, multi-instance CNPG for Keycloak availability, and user-data migration. EFS support already exists in NIC and the upstream EKS module; FSx requires new provisioning, CSI installation, and IAM wiring. The FSx evaluation implementation on the `remove-longhorn-aws` branch demonstrates feasibility but is not a capability shipped on `main`.

```mermaid
flowchart LR
  subgraph n["user node · pinned by two RWO mounts"]
    pod["jupyter pod"]
  end
  pod -->|/home/jovyan| h["home PVC · RWO"]
  pod -->|workspaces| w["workspace PVC · RWO"]
  pod -->|/shared/group| sh["shared PVC · RWX"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  h --> gp3["EBS gp3 · AZ-bound"]
  w --> gp3
  sys --> gp3
  sh --> csi["managed RWX CSI driver"] --> fs["EFS or FSx for OpenZFS"]
  fs -.-> bk["owed: provider backup<br/>and restore path"]
  gp3 -.-> owed["owed: per-AZ capacity,<br/>CNPG HA, backup for gp3 volumes"]
  classDef mg stroke:#5c9e6f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  class csi,fs mg
  class bk,owed gap
```

### E. Managed homes and shared storage

This shape puts homes and `/shared` on a regional or Multi-AZ managed RWX service, keeps the Keycloak database on gp3, and makes workspaces ephemeral. It removes the user node pin, the persistent AZ binding for homes, and Longhorn from AWS. Keycloak still needs multi-instance CNPG with zone-aware placement because its gp3 volumes remain AZ-bound. Nebi's local-mode SQLite database uses WAL, which cannot reside on NFS or be opened by clients on different hosts; the correctness fix is relocating the Nebi data directory to node-local storage, and this shape requires server mode because server mode is what makes that relocation lossless.

With the modeled Multi-AZ FSx implementation, this shape imposes a 26-57x penalty on metadata-write-heavy home operations relative to gp3. `git checkout` takes about 18 seconds rather than 0.32 seconds. It also costs about the same as the Longhorn incumbent at every modeled tier: $231 / $1,083 / $6,307 versus $231 / $1,045 / $6,235. This shape additionally requires DSP to make its affinity conditional, change the workspace lifecycle, deploy Nebi in server mode, and establish POSIX ownership through filesystem-specific StorageClass parameters. Server mode is configuration of a shipped capability rather than a new one, and Keycloak already supplies the OIDC it expects, but Nebi is alpha and explicitly not recommended for production use.

```mermaid
flowchart LR
  subgraph n["any node · pin released"]
    pod["jupyter pod"]
  end
  pod -->|/home/jovyan| h["home PVC · RWX"]
  pod -->|workspaces| w["emptyDir · node-local"]
  pod -->|/shared/group| sh["shared PVC · RWX"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  sys --> gp3["EBS gp3 · AZ-bound"]
  h --> csi["managed RWX CSI driver"]
  sh --> csi
  csi --> fs["regional or Multi-AZ managed RWX"]
  pod --> nebi["Nebi server · alpha"]
  nebi --> ndb["server database on its own volume<br/>SQLite by default, or CNPG PostgreSQL"]
  fs -.-> bk["owed: provider backup<br/>and restore path"]
  gp3 -.-> owed["owed: CNPG HA,<br/>zone placement, backup"]
  nebi -.-> nw["owed: server-mode configuration<br/>and production validation"]
  classDef mg stroke:#5c9e6f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  class csi,fs mg
  class bk,owed,nw gap
```

### F. Single-mount homes, apps from published bundles

This shape keeps homes and workspaces on gp3 and removes the pin by removing the reason it exists: the requirement that several of a user's pods share the same RWO volumes. Only the JupyterLab pod mounts the home and workspace PVCs. App pods mount neither — they pull the user's published workspace bundle, environment and source layers together, into a per-pod `emptyDir`, extending the `nebi-pull` mechanism DSP already runs for environments. With one consumer per volume, required affinity has no purpose, and EBS single-attach stops being a scheduling constraint. There is no read-only middle ground this competes with: two pods on different nodes cannot attach one gp3 volume even read-only.

It keeps block latency for interactive work, keeps Nebi local mode (the home-resident database now has exactly one client, which is what WAL requires), removes Longhorn from AWS, and shares D's cost and prerequisites: per-AZ capacity or Karpenter, multi-instance CNPG, a new backup path, and the managed-RWX integration for `/shared`.

Its gates are the publish contract rather than storage:

- **Write-back.** Bundles are one-directional. App pods must be stateless consumers of published content, with outputs going to `/shared`, object storage, or an explicit pull-back; whether current jhub-apps satisfy that is unverified.
- **Publish path.** Every user needs a registry to publish to — the unresolved Artifact Keeper-in-core question.
- **Delivery performance.** Per-pod install and pull times are unmeasured.
- **Iteration loop.** Apps see the last publish, not a live home; edit, push, respawn is a slower loop than reading files in place.
- **Named servers.** A second concurrent Jupyter server for the same user still needs the RWO home, so those co-locate or fail — a residue of the pin, not its removal.

```mermaid
flowchart LR
  subgraph n["any node · home AZ only"]
    lab["jupyterlab pod"]
  end
  subgraph napp["any node · unpinned"]
    app["app pod"]
  end
  lab -->|/home/jovyan| h["home PVC · RWO · lab only"]
  lab -->|workspaces| w["workspace PVC · RWO · lab only"]
  lab -->|/shared/group| sh["shared PVC · RWX"]
  app -->|/shared/group| sh
  app --> ed["emptyDir · bundle pull per pod"]
  lab -->|nebi push| oci["OCI registry<br/>(including configured Artifact Keeper)"]
  oci -->|"nebi bundle: env + source layers"| ed
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  h --> gp3["EBS gp3 · AZ-bound"]
  w --> gp3
  sys --> gp3
  sh --> csi["managed RWX CSI driver"] --> fs["EFS or FSx for OpenZFS"]
  fs -.-> bk["owed: provider backup<br/>and restore path"]
  gp3 -.-> owed["owed: per-AZ capacity,<br/>CNPG HA, backup for gp3 volumes"]
  oci -.-> gates["owed: app write-back contract,<br/>user publish path, delivery performance"]
  classDef mg stroke:#5c9e6f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  classDef artifact stroke:#2b6cb0,stroke-width:2px
  class csi,fs mg
  class bk,owed,gates gap
  class oci artifact
```

### G. Managed homes, published environments

This is the chosen option; the [storage design doc](../../design-doc/architecture/storage.md) describes it in full.

Shapes E and F each solve half of the multi-consumer problem and give up the other half. E keeps every pod's view of the user's files identical by putting the home on managed RWX, but leaves the metadata-heavy work (environment solves, installs, and the package cache) on that same filesystem, which is where its 26-57x penalty is felt. F moves environments off shared storage entirely and delivers them per pod, but pays for it by unmounting the home from app pods, so an app sees the last publish rather than the user's live files.

Shape G takes E's backends and F's delivery together. Home and `/shared` are managed RWX and mount into every user pod, including app pods. The workspace store is ephemeral and node-local, and so is everything else in the environment path: the built prefix through pixi's `detached-environments`, the package cache through `PIXI_CACHE_DIR`, and Nebi's local database through its data directory. App pods do not mount the workspace store at all. Selecting an environment at launch gives the app pod a per-pod `emptyDir` and an init container running `nebi pull` and `pixi install`, the mechanism DSP already ships.

The separation is the point: **files cross between pods on the filesystem, environments cross as a lockfile, and neither path touches the other.** Two pods that share no environment storage converge on identical environments because both build from one lockfile: they share the environment by digest rather than by mount. The RWX home then carries only what it is good at (authored files, notebooks, data a user edits in place), and the file-count-heavy work it is bad at moves off it by configuration rather than by architecture.

That reframes E's headline cost rather than removing it. The 26-57x figure was measured on metadata-write-heavy operations, and `git checkout` on a home still takes about 18 seconds against 0.32 on gp3. What changes is which operations land there. The benchmark already makes this case: `pixi install` takes 2.83-3.59 s on block storage against 31.97-61.62 s single-pod on every tested RWX backend, and an `emptyDir` sits in the block column. The untested combination flagged under [Home performance](#home-performance), where DSP sets no `PIXI_CACHE_DIR` and so leaves the cache under a home that E puts on NFS, is not an open question here, because pinning both the cache and the prefix to node-local storage is part of the shape's definition.

Against F, the trade runs the other way. G gives up block-latency homes and inherits E's dependence on Nebi server mode, and in return clears the three gates F cannot currently clear. There is no write-back contract to define, because app pods mount the real home and write to it directly; no publish-path requirement for authored files, because only the environment is published; and no edit-push-respawn loop for source changes. F's residual pin on concurrent named servers also disappears, since an RWX home admits any number of mounts.

Its gates:

- **Nebi server mode is required and Nebi is alpha.** Server mode supplies the environment picker that app launches read, and it is what makes moving the local database off the home lossless. This is E's risk unchanged, and it is the largest one in the shape.
- **Per-user isolation on managed RWX is unsolved.** One filesystem serving every home needs a controller that creates access points or PV/PVC pairs per user. This is inherited from E and is not answered by choosing a backend.
- **Ray is a tenancy problem plus a build step, not a mount.** Three layers have to line up, and storage is the easiest of them:
  - *Storage* is tractable. Managed RWX has no single-attach semantics, so a second PV and PVC in the Ray namespace, pointing at the same filesystem and access point, mounts the user's home concurrently. No block-backed shape can do this, which makes it the one place G is strictly better rather than merely equivalent. What is missing is the layer that creates and resolves that pair for the requesting user (the gateway described under [Ray and cross-pack sharing](#ray-and-cross-pack-sharing)) and the chart surface in [rayserve-pack #30](https://github.com/nebari-dev/rayserve-pack/issues/30). An `efs-ap` StorageClass mints a fresh access point per claim, so naive dynamic provisioning yields an empty directory rather than the user's home.
  - *Identity* is unbuilt. The Nebi token comes from DSP's pre-spawn hook, using the spawning user's `auth_state`; a pod that KubeRay creates from a RayService spec has no user and no hook, so it cannot authenticate a pull. The design expects this to be straightforward to supply.
  - *Tenancy* is the part no design answers yet. A RayCluster is one shared, long-lived object while workspaces are per user, so no single environment is correct for it to hold. That points at per-user or per-job clusters, which is idiomatic KubeRay, and makes environment delivery the binding constraint rather than the mount.

  Container images solve the delivery half but not tenancy. A baked image makes parity structural (driver, head, and workers on one digest, with no install step to diverge). It removes the runtime identity problem, since the kubelet pulls with a node-level secret rather than a user token. And it avoids prefix relocation, where a built conda or pixi environment breaks when moved because it embeds absolute paths, since build path and runtime path are identical. But a shared cluster runs one image, so baking one user's environment excludes every other user exactly as installing their bundle would. Images also need a build step the platform does not provide, turn `pixi add` into an image build, and leave one lockfile with two materializations, since the notebook is the Ray driver and runs the JupyterLab image with a pixi environment rather than the Ray image. Nebi already publishes through any OCI registry, so the missing piece is the builder, not the store. Neither profile is measured: bundles pay install time per pod, images pay pull time per node.
- **Interactive propagation still costs a publish and a restart.** The pull runs in an init container at spawn, so `pixi add` followed by `nebi push` does not reach an app that is already running. The pull is by workspace name rather than by pinned tag or digest, so restarting the app picks up the new version without recreating the app, but nothing re-materializes the environment in place. This is the cost of sharing by digest, and it applies to F equally.
- **Durability of the ephemeral workspace.** Editable installs, in-place compiled extensions, and unpublished authored files under the workspace path do not survive the pod. Keeping projects in the home rather than the workspace store is the mitigation, and it is a user-facing convention rather than an enforced one.

```mermaid
flowchart LR
  subgraph ds["user namespace"]
    subgraph n1["node A"]
      lab["jupyterlab pod"]
    end
    subgraph n2["node B · no pin"]
      app["app pod"]
    end
  end
  subgraph rs["ray namespace"]
    ray["ray head and workers"]
  end
  lab -->|/home/jovyan| h["home PVC · RWX"]
  app -->|/home/jovyan| h
  lab -->|/shared/group| sh["shared PVC · RWX"]
  app -->|/shared/group| sh
  h --> csi["managed RWX CSI driver"]
  sh --> csi
  csi --> fs["regional or Multi-AZ managed RWX"]
  lab -->|"detached prefix and package cache"| le["node-local disk"]
  app --> ae["emptyDir · per pod<br/>init: nebi pull, pixi install"]
  lab -->|"nebi push: pixi.toml and pixi.lock"| nebi["Nebi server · alpha"]
  nebi -->|nebi pull| ae
  nebi --> ndb["server database on its own volume"]
  kc["keycloak CNPG · 1 instance"] --> sys["database PVC · RWO"]
  sys --> gp3["EBS gp3 · AZ-bound"]
  ray -. "owed: per-user PV and PVC pair<br/>onto the same access point" .-> h
  nebi -. "owed: bundle delivery to Ray workers" .-> ray
  fs -.-> bk["owed: per-user isolation,<br/>provider backup and restore"]
  gp3 -.-> owed["owed: CNPG HA,<br/>zone placement, backup"]
  nebi -.-> nw["owed: server-mode production validation"]
  classDef mg stroke:#5c9e6f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  classDef artifact stroke:#2b6cb0,stroke-width:2px
  class csi,fs mg
  class bk,owed,nw gap
  class nebi artifact
```

Whether G deserves its own letter is fair to ask: it is E's backends plus configuration that E leaves unspecified, and it could be filed as "E, configured correctly". It is listed separately because that configuration decides whether E's measured penalty lands on the operations users actually wait on, and because E does not say what a second consumer does, which is the whole question the pin exists to answer.

### Choosing a managed RWX service on AWS

The decision does not depend on this choice, and this ADR does not make it. Shapes D, E, F, and G do not require FSx specifically. EFS is the lower-effort integration baseline: NIC already exposes an `efs:` configuration block and `efs-sc` StorageClass, and the upstream EKS module supplies the filesystem and Pod Identity integration. FSx is an optimization candidate that requires NIC to own additional Terraform, CSI-driver installation, IAM, StorageClass, lifecycle, and backup behavior.

The measured and modeled differences remain relevant to that implementation choice. For `/shared`, FSx is 2.1-2.5x faster than EFS at one writer and retains a metadata-read advantage at four writers; write-heavy results converge within benchmark noise at four. EFS Elastic has the flatter concurrency curve. At 50 users, the model prices FSx at $423 per month and EFS at $545-$1,550; at 200 users, FSx is $2,466 and EFS is $3,280-$7,300. That is a recurring difference of $122-$1,127 per month for a medium cluster and $814-$4,834 per month for a large cluster. EFS request volume and lifecycle tiering are not sufficiently measured to treat those ranges as final.

The economic test is symmetric. FSx must save enough recurring cost or improve performance enough to recover its one-time integration and continuing maintenance cost across the expected cluster fleet and lifetime. EFS must not accumulate more recurring cost over that same period than the integration work it avoids. The storage architecture remains valid with either managed RWX provider; choosing between them requires both sides of that comparison.

## Arguments

Each topic below corresponds to a section of the argument map, rendered in the image that closes it.

### Standardize the capability, not the implementation

Nebari should define observable storage behavior at the pack boundary: access mode, POSIX semantics, durability, availability, and performance. Each infrastructure provider can then supply the implementation that best meets those requirements.

This approach fits the provider architecture and the infrastructure available on each cloud. Hetzner and on-premises-style environments need an in-cluster RWX implementation such as Longhorn. AWS can choose between Longhorn and a managed RWX provider, with EFS and FSx for OpenZFS as candidate implementations. GKE and AKS expose their own managed RWX services and cross-zone block-storage options.

The cost is permanent divergence. Longhorn remains in the project for Hetzner, so a managed AWS path adds another implementation rather than replacing the existing one.

| Integration surface | AWS managed RWX | GCP Filestore | Azure Files | Longhorn |
| --- | --- | --- | --- | --- |
| Driver installation | EFS uses existing NIC and upstream-module support; FSx requires NIC-owned Helm, IAM, and Terraform | GKE cluster addon | included with AKS | Helm plus host iSCSI prerequisites |
| Workload identity | EKS Pod Identity and an IAM role | Workload Identity | managed identity | provider-specific backup credentials |
| POSIX ownership | EFS access points or FSx volume-creation parameters | export policies | uid and gid mount options | kubelet `fsGroup` |
| Resize and quota | provider-specific filesystem, access-point, or child-volume rules | service-tier capacity rules | share quotas and premium provisioning | Kubernetes PVC expansion |
| Backup | AWS Backup plus provider-native snapshots where available | Filestore backups and snapshots | Azure Backup for Files | NIC's implemented Longhorn workflow |

The implementation count understates the ongoing work: each provider has a different credential model, ownership mechanism, capacity behavior, failure catalogue, and restore procedure.

[![Capability versus implementation](provider-strategy.svg)](provider-strategy.svg)

### Longhorn uniformity

Using Longhorn everywhere provides one storage model, one operator vocabulary, and one recovery workflow. It also preserves NIC's working cross-provider backup interface.

That uniformity is narrower than it appears. NIC supports Longhorn on AWS and Hetzner, while GCP and Azure do not have it wired. Each cloud still needs nodes that permit iSCSI, privileged pods, and host access; provider-specific credentials and backup resources; Terraform wiring; and compatibility with its managed-node model. GKE Autopilot forbids the required privileged and host access, while EKS Auto Mode provides no path for installing the iSCSI prerequisite. Longhorn therefore supplies one operator model, not one implementation-free cloud integration.

### Shared-data path

Longhorn implements RWX by attaching the replicated volume to one share-manager pod and exporting it over NFS. DSP uses one shared PVC with group directories underneath it, so one server pod handles the shared path for every group. The stock DSP configuration adds another transitional form: its own NFS pod exports a Longhorn RWO volume.

This architecture concentrates bandwidth and failure handling in one pod. On untar, clone, and checkout, Longhorn RWX slows by 2.0-2.4x from one to four concurrent writers. FSx slows by 2.6-2.7x at its 160 MBps entry tier, while EFS Elastic remains about 1.1x. The measured FSx tier is the floor used by the small cost-model tier; the medium and large models buy 320 and 640 MBps, so their concurrency behavior is not measured. The benchmark does not locate the crossover beyond four writers.

Nebari Classic supplies two higher-scale warnings that the four-writer result cannot answer. Dharhas Pothina described a conference tutorial where large numbers of multi-node Dask clusters exhausted shared NFS, and a separate occasion where managed EFS became unresponsive even after its performance tier was raised. These are historical operational examples rather than controlled benchmarks, but they show that a managed NFS service can still become a system-wide bottleneck when distributed workers use it as their data plane.

For operations that `/shared` actually serves, Longhorn and FSx are within noise at one writer; at four writers Longhorn is 1.2-1.4x faster on extraction and checkout. Environment creation is not part of this comparison because environments live on the per-user workspace volume, not `/shared`.

[![Shared data path](data-path.svg)](data-path.svg)

### Home performance

The benchmark covers thirteen configurations: EBS gp2 and gp3, Longhorn RWO and RWX, FSx for OpenZFS Single-AZ and Multi-AZ, EFS Elastic, and DSP's chart NFS server, including four-writer tests for RWX backends.

| Backend | Untar | Clone | Checkout | Environment create | Autosave p95 | Untar x4 | Environment create x4 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| EBS gp2 | 0.39 s | 0.74 s | 0.44 s | 2.83 s | 8 ms | - | - |
| EBS gp3 | 0.39 s | 0.71 s | 0.32 s | 3.01 s | 8 ms | - | - |
| Longhorn RWO | 0.48 s | 0.97 s | 0.42 s | 3.59 s | 10 ms | - | - |
| Longhorn RWX | 14.51 s | 15.31 s | 15.83 s | 31.97 s | 13 ms | 32.35 s | 83.20 s |
| FSx OpenZFS Single-AZ | 16.99 s | 13.32 s | 15.15 s | 56.30 s | 80 ms | 45.90 s | 98.51 s |
| FSx OpenZFS Multi-AZ | 20.65 s | 18.71 s | 18.37 s | 53.41 s | 100 ms | 48.15 s | 108.39 s |
| DSP chart NFS | 29.86 s | 25.59 s | 24.80 s | 59.37 s | 12 ms | 44.35 s | 83.40 s |
| EFS Elastic | 38.86 s | 33.09 s | 31.77 s | 61.62 s | 63 ms | 42.25 s | 72.41 s |

The decisive gap is metadata writes and file creation. Metadata reads can improve through caching, bulk I/O is acceptable across the tested backends, and notebook autosave remains short. The environment-create column measures the workspace volume, not the home volume. Home access-mode decisions should use untar, clone, and checkout instead.

No agreed latency threshold exists for this trade. The product decision is whether slower interactive file operations are worth free scheduling, removal of the AZ pin, and a smaller AWS operations surface.

Nebi local mode creates a separate correctness constraint, not another performance threshold. Its SQLite database lives inside the home PVC and opens in WAL mode, whose shared-memory file requires every process touching the database to run on the same host and is unsupported on network filesystems. The current RWO home and per-user node pin jointly provide that same-host guarantee; shapes E and G replace the home with NFS and release the pin, removing both guarantees. The storage benchmark cannot detect this failure mode because it measures untar, clone, and checkout rather than concurrent database access.

The fix is one environment variable, not a new component: pointing `NEBI_DATA_DIR` at node-local ephemeral storage keeps every client's database off the network filesystem, which DSP already does for jhub-apps pods. Server mode is what makes that relocation lossless — workspace tracking moves behind one server process with its own database on its own volume, so a discarded local database costs a cache miss rather than a user's workspace list. SQLite remains the server default and PostgreSQL on CNPG is a separate availability choice, and DSP already ships the Keycloak OIDC server mode expects. Two things stay open: Nebi is alpha and explicitly not recommended for production, and whether a server-mode client still keeps a local database needs confirmation against Nebi's source — DSP's `nebi-pull` container writes a `nebi.db` even though it only pulls.

Making the workspace ephemeral has two gates. **Durability:** a lockfile reproduces only declared content, so editable installs (`pip install -e`), in-place compiled extensions, and unpublished authored files are specific loss cases. **Sharing** is not a blocker: DSP already snapshots the selected workspace at launch, with a `nebi-pull` init container running `nebi pull` and `pixi install` into a per-pod `emptyDir`, and server mode extends the same push/pull model through `workspaces_dir`. Every spawn still mounts two per-user RWO volumes — home and the workspace store at `/var/lib/nebi/workspaces` — and together those are why DSP pins app pods to the JupyterLab node. Shape F removes that pin from the other direction: bundles carry optional source layers, so app pods can pull authored files along with the environment instead of mounting home at all.

The benchmark prices the ephemeral path favorably. With the package cache primed, `pixi install` takes 2.83-3.59 s on block storage against 31.97-61.62 s single-pod on every tested RWX backend and 68-122 s per pod across four concurrent pods (the benchmark table reports per-backend medians, 72-108 s). An `emptyDir` is node-local disk, so per-spawn materialization sits in the block-storage column: these numbers argue against an RWX environment store, not against materializing per spawn. One combination is untested — the benchmark kept the package cache node-local while DSP sets no `PIXI_CACHE_DIR`, leaving it under the home that shape E puts on NFS. Pinning the cache to node-local storage removes the question in one chart change.

[![Home volume access mode](homes.svg)](homes.svg)

### Cross-AZ behavior

Removing Longhorn from RWO workloads does not make EBS portable across AZs. A dynamically provisioned EBS PV carries a zone requirement, so Kubernetes schedules the pod into that AZ or leaves it `Pending`. NIC must provide capacity in the required zone through one node group per AZ or a provisioner such as Karpenter that understands PV topology.

This solves rescheduling when the AZ is healthy. It does not provide service during an AZ outage: the EBS-backed workload waits for its AZ to return. The acceptability of that behavior depends on a platform availability objective that is not defined. Longhorn's guarantee is also weaker than full multi-AZ durability because NIC uses two replicas with soft zone anti-affinity.

A regional or Multi-AZ managed RWX service removes the home-volume AZ constraint. In the measured FSx implementation, Multi-AZ latency is close to Single-AZ. Its modeled total is about 1.7x the Single-AZ shape at the medium tier and ranges from 1.3x to 2.3x across the modeled tiers. This makes cross-AZ availability primarily a cost decision rather than a performance decision for FSx; EFS is regional by design and has its own request-based cost model.

[![Cross-AZ attachment](cross-az.svg)](cross-az.svg)

### Default StorageClass

Longhorn's RWX role and default-class role are separate. NIC configures Longhorn as the default and demotes every other StorageClass during installation and upgrade. The Keycloak CNPG manifest also receives NIC's cluster-wide StorageClass as an explicit value and currently sets `instances: 1`.

Changing the default therefore requires a per-workload StorageClass surface, not only a chart flag. DSP's home and workspace PVCs inherit the cluster default, while the Keycloak database is explicitly rendered. A safe change must control both paths and state which class each workload uses.

[![Longhorn as the default StorageClass](default-class.svg)](default-class.svg)

### Backup and restore

NIC has a working Longhorn backup path: configuration, recurring snapshots and backups, keyless S3 access on AWS, retained backup buckets, a restore runbook, and an integration test. Backup enrollment follows Longhorn volumes, and NIC rejects backup configuration when Longhorn is not the effective storage implementation.

Every shape that moves a volume off Longhorn must provide a replacement durability story. Keycloak currently has one CNPG instance, so moving it to gp3 requires both multi-instance database replication for node and AZ failover and a separate backup path for bad migrations, deletion, and corruption. Managed filesystems also require provider-specific backup, retention, and restore-into-a-fresh-cluster procedures.

The Longhorn path carries maintenance of its own because retained backup buckets are coupled to provider Terraform state addresses. This is a real implementation benefit with a continuing provider-integration cost, not a decisive argument by itself.

[![Backup and restore](backup.svg)](backup.svg)

### Operator burden

Longhorn makes node maintenance storage-aware. The NIC runbook requires cordoning the node, disabling Longhorn scheduling, requesting replica eviction, waiting for replicas to move, and only then draining and terminating the node. Diagnosis also requires Longhorn-specific CRs and concepts such as engines, replicas, faulted volumes, placement, and replenishment delays.

The cost model estimates Longhorn work at 9.3 operator hours per cluster per month and managed-filesystem work at 2.3 hours, a difference of about seven hours. This is an estimate derived from runbooks and observed incidents rather than measured operational data. It should remain a separate line item instead of being treated as zero.

Longhorn also assumes capacity settings that NIC does not fully validate. A 20 GiB default AWS node disk leaves roughly 2.4 GiB schedulable after system use and Longhorn's 25% reserve, so a 20 GiB RWX volume cannot be provisioned on the default-shaped node. This is a correctable NIC configuration defect, but it illustrates the storage-specific knowledge operators need.

[![Operator burden](operations.svg)](operations.svg)

### Cost

The AWS model uses on-demand `us-east-1` pricing for 10-, 50-, and 200-user clusters. Longhorn costs 2.5-2.7x the managed-shared shape at each tier. The main drivers are:

- two replicas plus a 25% disk reserve, producing a 2.67x capacity multiplier;
- a dedicated storage node group; and
- approximately 12% of each user node's CPU reserved for `instance-manager`.

Using FSx as the managed provider, the model estimates the managed-shared shape at $86, $423, and $2,466 per month, compared with $231, $1,045, and $6,235 for the incumbent. It excludes cross-AZ replication traffic, workspace PVCs, and operator labor from the dollar total. It also uses assumed customer sizes and shared-storage I/O rather than observed production distributions.

That saving does not generalize to managed homes. The Multi-AZ FSx shape that puts both homes and `/shared` on managed RWX costs $231, $1,083, and $6,307: effectively the same as the incumbent while carrying the measured home-latency penalty.

EFS has a different cost risk. Elastic throughput stays flat in the four-writer test but charges per GiB read and written, so the bill has no fixed ceiling. The model places EFS $122-$1,127 per month above FSx at 50 users and $814-$4,834 above it at 200 users, depending on I/O. FSx has a fixed provisioned-throughput ceiling that can be raised by purchasing a higher tier. Actual shared-storage I/O per user and the value of EFS lifecycle tiering are therefore key missing inputs.

Right-sizing home PVC requests is a lower-risk cost intervention under every shape and has 2.67x the effect under Longhorn. It should be priced before migration work is justified solely by storage cost.

[![Cost](cost.svg)](cost.svg)

### Compute model and other clouds

Longhorn requires node-level iSCSI support, privileged access, and host storage. These requirements exclude EKS Auto Mode's immutable nodes and GKE Autopilot. A hybrid EKS fleet can keep traditional storage nodes beside Auto Mode compute, but it retains both compute-management paths.

Provider-native storage changes the trade elsewhere. GKE exposes Filestore and cross-zone block storage; AKS exposes Azure Files and zone-redundant block storage. These observations come from vendor documentation rather than Nebari deployments and define future evaluation scope rather than verified product behavior.

[![Compute model](compute.svg)](compute.svg)

[![The other clouds](other-clouds.svg)](other-clouds.svg)

## Links

- [Storage design doc](../../design-doc/architecture/storage.md): the canonical design and the storage contract
- [ADR-0002](../0002-longhorn-distributed-block-storage-for-aws.md): Longhorn distributed block storage for AWS
- [ADR-0007](../0007-cloudnativepg-managed-databases.md): CloudNativePG as foundational database infrastructure
- [DSP #221](https://github.com/nebari-dev/data-science-pack/issues/221): scheduling failures caused by the per-user node pin
- [NIC #597](https://github.com/nebari-dev/nebari-infrastructure-core/issues/597) and [NIC #598](https://github.com/nebari-dev/nebari-infrastructure-core/issues/598): cross-namespace volume access and object-storage access
- [rayserve-pack #30](https://github.com/nebari-dev/rayserve-pack/issues/30): mounting user volumes into Ray workers
