# Nebari storage strategy

## Status and scope

This document is a research and decision-support artifact. It records the current storage requirements, measurements, cost estimates, candidate shapes, and arguments; it does not select an AWS storage architecture. Settled platform requirements are stated as such, while provider selection and the other unresolved choices remain open for the team to evaluate against the decision gates below.

## Background

Nebari began as a bundled data-science platform on Kubernetes. Nebari Infrastructure Core (NIC) now provisions the cluster and its foundational services, while software packs add workload-specific capabilities. The Data Science Pack (DSP) is one of those packs.

DSP runs JupyterHub and schedules each user's Jupyter servers and applications as Kubernetes pods. Those workloads need several kinds of persistent data:

- a private home directory for notebooks, source code, configuration, and user state;
- a workspace for materialized software environments;
- shared POSIX directories for collaboration within groups; and
- object storage for large datasets and other data that does not require filesystem semantics.

These uses have different access, performance, durability, and scheduling requirements. Treating them all as one storage problem makes the platform simpler to configure, but forces every workload onto the compromises of one implementation.

## Why Nebari uses Longhorn

AWS EBS volumes are limited to one Availability Zone (AZ). A user pod can be rescheduled, but its EBS-backed home remains in the AZ where the volume was created. NIC uses one autoscaling group across several AZs, and Cluster Autoscaler cannot request capacity in a specific AZ from that group. The result can be a pod that remains `Pending` because the cluster adds capacity in the wrong AZ.

[ADR-0002](../0002-longhorn-distributed-block-storage-for-aws.md) selects Longhorn to mask that topology problem. Longhorn replicates data across Kubernetes nodes, so a volume can follow a workload to another node or AZ. It also gives Nebari one storage implementation across AWS, Hetzner, and on-premises-style deployments where no managed shared filesystem is available.

Longhorn currently fills three separate roles:

1. It is the default StorageClass, including for single-pod RWO volumes.
2. It provides RWX volumes through an NFS share-manager pod.
3. It provides NIC's only implemented backup and restore path.

Those roles do not have to remain coupled. The storage decision is therefore broader than choosing an RWX backend: it determines which workloads use Longhorn, how pods are scheduled, what happens during an AZ outage, how backups work, and how much storage infrastructure operators own.

## How to read the maps

[![Legend](legend.svg)](legend.svg)

## What DSP storage needs

The current DSP storage layout has four volume shapes:

| Volume | Access mode | Contents | Consumers |
| --- | --- | --- | --- |
| `claim-{user}` | RWO | `/home/jovyan` and the user's Nebi database | every pod run by that user |
| `nebi-workspaces-{user}` | RWO | materialized environments; 20 GiB by default | every pod run by that user |
| `/shared/{group}` | RWX | POSIX files shared by group members | pods belonging to multiple users |
| Keycloak CNPG | RWO | identity database | one database instance per volume |

The group directory creates the hard RWX requirement. Multiple users must be able to read and write the same POSIX path while their pods remain independently schedulable. RWO can serve several pods only when they all run on the same node; applying that constraint to a group creates a hard capacity ceiling and composes badly when users belong to several groups.

Object storage complements this filesystem rather than replacing it. It is the appropriate default for large datasets and immutable artifacts, but it does not provide the POSIX rename, locking, directory, and permission behavior that existing notebooks and tools expect from `/shared`. Nebari also lacks a per-user AWS identity and permission layer for mapping Keycloak users and groups to object-storage access. The preferred boundary is therefore:

- use object storage for large datasets and other object-native workflows;
- use mounted volumes for homes, user outputs, environments, and shared small-file workflows that require POSIX behavior.

Cross-pack sharing remains a separate design question. [NIC #597](https://github.com/nebari-dev/nebari-infrastructure-core/issues/597) covers object-storage access and [NIC #598](https://github.com/nebari-dev/nebari-infrastructure-core/issues/598) covers cross-namespace volume sharing. These use cases should use object storage where it fits and require RWX only when POSIX sharing is part of the contract.

[![Is RWX required?](rwx-required.svg)](rwx-required.svg)

## Current scheduling constraint

DSP adds required pod affinity so every pod belonging to one user runs on the same node. This allows all of those pods to mount the same RWO home and workspace PVCs, but it makes that node's remaining capacity the maximum capacity available to the user.

[DSP #221](https://github.com/nebari-dev/data-science-pack/issues/221) records the resulting scheduling failures:

- a GPU JupyterLab cannot start while one of the user's applications is pinned to a CPU node;
- an application pinned to a GPU node prevents that node from scaling down after the lab stops; and
- all concurrent workloads for one user must fit on one node even when the cluster has free capacity elsewhere.

Changing the home to RWX does not remove this constraint by itself. The workspace PVC is also RWO and is mounted into every user pod. Releasing the pin therefore requires both an RWX home and a workspace that no longer requires single-node attachment, such as an ephemeral node-local volume.

## AWS storage shapes

Five AWS shapes capture the meaningful choices. Each successive shape changes another workload rather than representing a small variation of the same design.

| | **A. Incumbent** | **B. System split** | **C. Longhorn RWX only** | **D. Managed shared** | **E. Managed homes** |
| --- | --- | --- | --- | --- | --- |
| Home | Longhorn RWO | Longhorn RWO | gp3 RWO | gp3 RWO | managed RWX |
| Workspace | Longhorn RWO | Longhorn RWO | gp3 RWO | gp3 RWO | ephemeral |
| `/shared` | Longhorn RWX | Longhorn RWX | Longhorn RWX | managed RWX | managed RWX |
| Keycloak CNPG | Longhorn RWO | gp3 RWO | gp3 RWO | gp3 RWO | gp3 RWO |
| Longhorn on AWS | all volumes | homes, workspaces, and `/shared` | `/shared` only | no | no |
| Cost model, using FSx in D/E: 10 / 50 / 200 users | $231 / $1,045 / $6,235 | approximately A | est. $182 / $589 / $3,032 | $86 / $423 / $2,466 | $231 / $1,083 / $6,307 |
| Operator time per month | ~9.3 h | ~9.3 h | ~9.3 h | ~2.3 h | ~2.3 h |
| Home write latency | baseline | baseline | approximately baseline | approximately baseline | 9-37x baseline |
| `/shared` with FSx, one writer | baseline | baseline | baseline | within benchmark noise | within benchmark noise |
| `/shared` with FSx, four writers | baseline | baseline | baseline | 1.2-1.4x slower | 1.2-1.4x slower |
| User node pin | yes | yes | yes | yes | no |
| AZ and database HA work | no | CNPG multi-instance with zone placement | per-AZ groups or Karpenter, plus CNPG multi-instance | per-AZ groups or Karpenter, plus CNPG multi-instance | CNPG multi-instance with zone placement |
| Backup coverage | all volumes | excludes system volumes | excludes homes, workspace, and system volumes | new implementation required | new implementation required |
| User-data migration | no | no | yes | yes | yes |
| EKS Auto Mode possible | no | no | no | yes | yes |
| Managed RWX integration required | no | no | no | yes | yes |

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
  class LH,sm,sn lh
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
  class LH,sm,sn lh
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
  class LH,sm,sn lh
  class owed gap
```

### D. Managed `/shared`, block-backed homes

This shape puts RWO workloads on gp3 and `/shared` on an AWS-managed RWX service. It removes Longhorn from AWS while preserving fast block storage for interactive home workloads. EFS and FSx for OpenZFS are implementation candidates for the same shape.

The benchmark favors the boundary this shape draws—block storage for RWO workloads and managed RWX for shared data—and the FSx cost model makes it the cheapest measured implementation. It requires a managed-RWX StorageClass and workload routing, a provider-specific backup and restore path, per-AZ node groups or Karpenter, multi-instance CNPG for Keycloak availability, and user-data migration. EFS support already exists in NIC and the upstream EKS module; FSx requires new provisioning, CSI installation, and IAM wiring. The FSx evaluation implementation on the `remove-longhorn-aws` branch demonstrates feasibility but is not a capability shipped on `main`.



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

This shape puts homes and `/shared` on a regional or Multi-AZ managed RWX service, keeps the Keycloak database on gp3, and makes workspaces ephemeral. It removes the user node pin, the persistent AZ binding for homes, and Longhorn from AWS. Keycloak still needs multi-instance CNPG with zone-aware placement because its gp3 volumes remain AZ-bound.

It also imposes a 9-37x penalty on metadata-write-heavy interactive work. `git checkout` takes 15-32 seconds in the benchmark rather than roughly 0.4 seconds on block storage. This shape additionally requires DSP to make its affinity conditional, change the workspace lifecycle, and establish POSIX ownership through filesystem-specific StorageClass parameters.



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
  fs -.-> bk["owed: provider backup<br/>and restore path"]
  gp3 -.-> owed["owed: CNPG HA,<br/>zone placement, backup"]
  classDef mg stroke:#5c9e6f,stroke-width:2px
  classDef gap stroke:#c26060,stroke-width:2px,stroke-dasharray:4 3
  class csi,fs mg
  class bk,owed,w gap
```

### Selecting the AWS managed RWX provider

Shapes D and E do not require FSx specifically. EFS is the lower-effort integration baseline: NIC already exposes an `efs:` configuration block and `efs-sc` StorageClass, and the upstream EKS module supplies the filesystem and Pod Identity integration. FSx is an optimization candidate that requires NIC to own additional Terraform, CSI-driver installation, IAM, StorageClass, lifecycle, and backup behavior.

The measured and modeled differences remain relevant to that implementation choice. For `/shared`, FSx is 2.1-2.5x faster than EFS at one writer and retains a metadata-read advantage at four writers; write-heavy results converge within benchmark noise at four. EFS Elastic has the flatter concurrency curve. At 50 users, the model prices FSx at $423 per month and EFS at $545-$1,550; at 200 users, FSx is $2,466 and EFS is $3,280-$7,300. That is a recurring difference of $122-$1,127 per month for a medium cluster and $814-$4,834 per month for a large cluster. EFS request volume and lifecycle tiering are not sufficiently measured to treat those ranges as final.

The economic test is symmetric. FSx must save enough recurring cost or improve performance enough to recover its one-time integration and continuing maintenance cost across the expected cluster fleet and lifetime. EFS must not accumulate more recurring cost over that same period than the integration work it avoids. The storage architecture remains valid with either managed RWX provider; choosing between them requires both sides of that comparison.

## Arguments

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

This architecture concentrates bandwidth and failure handling in one pod. Longhorn RWX slows by 2.2-2.6x from one to four concurrent writers. FSx slows by 1.7-2.7x at its 160 MBps entry tier, while EFS Elastic remains within 1.1-1.2x. The benchmark does not locate the crossover beyond four writers.

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

No acceptable latency threshold exists for this trade. The product decision is whether slower interactive file operations are worth free scheduling, removal of the AZ pin, and a smaller AWS operations surface.

[![Home volume access mode](homes.svg)](homes.svg)

### Cross-AZ behavior

Removing Longhorn from RWO workloads does not make EBS portable across AZs. A dynamically provisioned EBS PV carries a zone requirement, so Kubernetes schedules the pod into that AZ or leaves it `Pending`. NIC must provide capacity in the required zone through one node group per AZ or a provisioner such as Karpenter that understands PV topology.

This solves rescheduling when the AZ is healthy. It does not provide service during an AZ outage: the EBS-backed workload waits for its AZ to return. The acceptability of that behavior depends on a platform availability objective that is not defined. Longhorn's guarantee is also weaker than full multi-AZ durability because NIC uses two replicas with soft zone anti-affinity.

A regional or Multi-AZ managed RWX service removes the home-volume AZ constraint. In the measured FSx implementation, Multi-AZ latency is close to Single-AZ while modeled cost is 1.65-1.67x higher. This makes cross-AZ availability primarily a cost decision rather than a performance decision for FSx; EFS is regional by design and has its own request-based cost model.

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

EFS has a different cost risk. Elastic throughput stays flat in the four-writer test but charges per GiB read and written, so the bill has no fixed ceiling. The model places EFS $122-$1,127 per month above FSx at 50 users and $814-$4,834 above it at 200 users, depending on I/O. FSx has a fixed provisioned-throughput ceiling that can be raised by purchasing a higher tier. Actual shared-storage I/O per user and the value of EFS lifecycle tiering are therefore key missing inputs.

Right-sizing home PVC requests is a lower-risk cost intervention under every shape and has 2.67x the effect under Longhorn. It should be priced before migration work is justified solely by storage cost.

[![Cost](cost.svg)](cost.svg)

### Compute model and other clouds

Longhorn requires node-level iSCSI support, privileged access, and host storage. These requirements exclude EKS Auto Mode's immutable nodes and GKE Autopilot. A hybrid EKS fleet can keep traditional storage nodes beside Auto Mode compute, but it retains both compute-management paths.

Provider-native storage changes the trade elsewhere. GKE exposes Filestore and cross-zone block storage; AKS exposes Azure Files and zone-redundant block storage. These observations come from vendor documentation rather than Nebari deployments and define future evaluation scope rather than verified product behavior.

[![Compute model](compute.svg)](compute.svg)

[![The other clouds](other-clouds.svg)](other-clouds.svg)

## Decision gates

A storage proposal should close these questions explicitly:

- **POSIX behavior:** concurrent reads and writes, locking, atomic rename, ownership, permissions, setgid propagation, `subPath`, and failure recovery.
- **Performance:** an agreed latency threshold for home operations and concurrency testing beyond four writers for `/shared`.
- **Availability:** node failure, AZ failure, endpoint failover, remount behavior, lock recovery, and stated recovery objectives.
- **Backup and restore:** coverage for every volume, retention, credentials, and restore into a new cluster.
- **Operations:** installation, upgrades, observability, capacity changes, incident response, and security patching.
- **Cost:** realistic customer sizes, shared-storage I/O, request charges, cross-AZ traffic, backups, compute overhead, and operator time.
- **Migration:** a tested path for every user volume that changes backend or access mode.
- **Portability:** the permanent cost of Longhorn plus each provider-native implementation.

The source argument map is [`storage-strategy.argdown`](storage-strategy.argdown). [`overview.svg`](overview.svg) renders the complete graph; the topic maps above render smaller sections of the same argument.
