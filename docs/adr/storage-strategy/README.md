# Nebari storage strategy

Longhorn holds several roles in a Nebari cluster at once — sole default StorageClass, RWX implementation, backup implementation. This works through which of those it should keep, on which providers, and what replaces the rest. It started as the narrower question of ReadWriteMany (RWX) storage on AWS, and kept running into the others.

Nothing here proposes a decision. It records what has been argued, which claims are settled, which are open, and what evidence would close them, so that whatever gets proposed later starts from a position anyone can inspect and dispute. Some of it will end up amending [ADR-0002](../0002-longhorn-distributed-block-storage-for-aws.md), which adopted Longhorn to solve cross-AZ EBS attachment and named RWX and backups as later benefits.

The source of truth is [`storage-strategy.argdown`](storage-strategy.argdown). Rendering is `make argdown`, which writes every SVG below from that one file; the sections in the source are the sections on this page. The source also carries two things this page does not: the acceptance gates each topic defers to (under *AWS decision gate*), and the running list of assumptions the argument leaves behind.

The whole argument in one picture is [`overview.svg`](overview.svg) — useful as a reference, unreadable as an introduction. The maps below are the same argument split by *topic*: each one takes the central fork and hangs a single line of attack off it.

## How to read the maps

![Legend](legend.svg)

## Does the platform need RWX at all?

Everything downstream depends on this. The claim is that a production Nebari with the Data Science Pack must provide RWX, because the pack's collaboration contract (`/shared/group-name`, written concurrently by users whose pods must stay independently schedulable) cannot be served by a ReadWriteOnce volume.

Three ways out are argued and all three fail: pinning every collaborating pod to one node, replacing the shared directory with object storage, and dropping group collaboration from scope.

**Homes are the second reason to want it, and the larger one.** `/shared` is what makes RWX unavoidable, so it is what this section argues from — but nothing about the requirement stops there. Nothing in the pack's contract requires RWX for a *home* volume; what recommends it is that RWX homes dissolve the per-user node pin, the cross-AZ constraint, and Longhorn's presence on AWS in one move, rather than mitigating each separately. That is a supporting reason for the capability, not a second requirement, and it is why homes get [their own topic](#topic-home-volume-access-mode) — including the performance evidence running against them. Cross-namespace sharing between packs is a third candidate, and the weakest: no requirement is established there at all.

That third candidate has moved on twice. The 2026-08-13 standup narrowed it: large datasets go to object storage, so the Ray-to-Jupyter case that originally motivated cross-pack RWX is answered by a bucket, not a volume, leaving small-file sharing between packs. And the user-story gate is now met — the work is two tracked spikes, [#597](https://github.com/nebari-dev/nebari-infrastructure-core/issues/597) (object-storage access) and [#598](https://github.com/nebari-dev/nebari-infrastructure-core/issues/598) (cross-namespace volume sharing). What has *not* been established is that the remainder needs RWX at all. The map now carries the working preference that it probably does not: reach for object storage wherever it fits and treat a shared filesystem as the fallback for what genuinely needs POSIX. That preference is not free either, and its costs are the open ones — no per-user AWS identity to key access on (a data catalog is the leading proposal for the permission layer), unproven small-file and metadata performance, and nothing in-cluster short of running MinIO, which trades a managed service for another storage system to operate.

![Is RWX required?](rwx-required.svg)

## One implementation, or one per provider?

This is the fork every topic below hangs off. Nebari can standardize the RWX *capability* and let each provider supply the implementation (managed filesystem on AWS, Longhorn on Hetzner), or it can run Longhorn everywhere and buy uniformity.

Only the per-pack principle is decided (2026-08-12): each pack requests the access mode it needs, with no cluster-wide policy. The fork itself is not — the rest of the page is the case against "Longhorn everywhere", topic by topic, plus what each topic has to measure before it counts.

![Capability versus implementation](provider-strategy.svg)

## Topic: operator burden

Longhorn's cost is not only the project's integration work — it is a standing burden on whoever runs each cluster, and the runbooks already in this repo are the evidence for its size: node drains need a five-step replica-eviction procedure, failure diagnosis needs Longhorn-specific concepts, and some capacity knobs are still tuned by hand per cluster.

That cost scales with clusters and on-call operators, not with providers, which is where the uniformity argument prices it. Priced under the operations gate.

![Operator burden](operations.svg)

## Topic: the shared data path

Longhorn does not serve RWX from its replicated block layer directly — it puts an NFS server pod in front, and the pack requests one shared PVC for all groups, so a single pod is the data path for every group's shared storage at once. On a stock deploy today that pod is the pack's own transitional NFS server exporting a Longhorn RWO volume, with Longhorn's share-manager not in the path at all. Either way, every operation across it pays a network round trip, which is what makes metadata-heavy work slow.

Note what that does *not* cover: the metadata-heavy workloads usually cited — environment builds, `git status`, autosave — live on the home volume, not on `/shared`, so they price the home gate below; the two comparisons must not be run together. The counter is that the whole-cluster *scope* of the path is the pack's layout choice, fixable without changing storage backend. What survives is per-group and smaller: one pod's network connection as the bandwidth ceiling, and the still-unanswered question of whether Longhorn synchronises concurrent cross-namespace writers at all.

One open idea would cut across all of it: data-intensive jobs may not need to run *from* shared storage. Stage the inputs onto the node's local NVMe, run there, copy results back, and the network filesystem is paid at the edges instead of per operation. Unexplored — it needs a staging mechanism, a cache-invalidation story, and instance types with enough local disk — but if it works it narrows what the storage layer has to be fast at, which changes what the performance gate is measuring.

![Shared data path](data-path.svg)

## Topic: Longhorn as the default StorageClass

Longhorn holds several roles in NIC at once — sole default StorageClass, RWX implementation, backup implementation — and it was adopted for none of them: ADR-0002 adopted it to solve cross-AZ EBS attachment, with RWX and backups named as benefits that came later. Those roles arrived separately and are separable. NIC installs the chart with `persistence.defaultClass: true` and then, on install and on upgrade, walks every other StorageClass and patches it to non-default. Only the replica count is configurable; nothing gates the default or the demotion. On AWS, Longhorn is on unless you opt out. So "Longhorn everywhere" in practice means "Longhorn under everything", which is a larger commitment than the RWX requirement asks for.

The workload that makes this concrete is a database. Keycloak's Postgres runs on CloudNativePG, which replicates across instances at the application layer; synchronously replicated block storage underneath it stores a second copy of a guarantee the database already provides, adds a replication round trip to every fsync, and puts a shared storage control plane in the failure path of the component that gates every login. It needs neither RWX nor cross-node attach. None of that depends on the AWS question — even under "Longhorn everywhere", Longhorn can be the RWX class while single-pod volumes use native block storage. It is the cluster-level counterpart of the per-pack principle already decided: if each pack should request what it needs, no cluster-wide default should be answering that question for every unclassed PVC.

Two things stand between that and a config change, and they are why this is scoped as its own topic rather than assumed done.

**There is no seam to hang it on.** Dropping the default annotation would not actually move the database. NIC computes one cluster-wide storage class and renders it as a literal into the only PVC-bearing manifest it owns — the Keycloak CNPG cluster — so with Longhorn enabled that volume is pinned to `longhorn` explicitly, not by default-class inheritance. What is missing is a per-workload class field. The only precedent for a second class is the opt-in EFS provisioner mode, which creates an `efs-sc` class without annotating it default and has nothing routed onto it — it shows the gap rather than filling it. Same missing injection seam ADR-0003 designs for pack values. Worth noting too that the fallback class is assumed rather than provisioned: NIC names `gp2` when Longhorn is off, but nothing in Terraform creates it — that is EKS's built-in in-tree class, and gp2 versus gp3 is itself unmeasured.

**Backups set the order.** Backup enrolment is keyed to the default `longhorn` class, and `nic` refuses a backups config outright when the effective storage class is not `longhorn` — so this is a validation error the operator meets immediately, not a silent gap. The Keycloak database is exactly the volume the argument wants moved, and its only data protection today is Longhorn's volume backups: barman-to-S3 is unbuilt and ADR-0007 makes backup an explicit non-goal. Durability answer first, then de-defaulting; the other order trades an unnecessary storage layer for an unprotected database.

![Longhorn as the default StorageClass](default-class.svg)

## Topic: backup and restore

Native Longhorn backups shipped in July 2026 and are hard-gated on Longhorn being the default StorageClass — `nic validate` and `nic deploy` both refuse otherwise. There is no storage-agnostic backup path in the project, so any AWS shape that moves volumes off Longhorn replaces working, tested machinery rather than merely avoiding work it would have had to build.

The offsetting point is that the implementation is coupled to per-provider Terraform internals and carries its own upkeep. Backup is priced on its own gate, not folded into availability or cost.

![Backup and restore](backup.svg)

## Topic: the compute model

Keeping Longhorn on AWS also decides the compute model. Longhorn needs a node-level iSCSI prerequisite that has no install path on EKS Auto Mode's managed immutable nodes, and Auto Mode's forced node rotation is hostile to node-local replicas — so the two are one decision, not two.

Two things soften it: a hybrid fleet (Auto Mode plus a classic storage node group) is technically available, though it keeps every code path Auto Mode was meant to remove; and whether Auto Mode is adoptable at all is separately open.

![Compute model](compute.svg)

## Topic: cross-AZ attachment

The strongest objection to removing Longhorn: as the sole default StorageClass it also hides that EBS volumes are AZ-bound, which is the failure ADR-0002 was originally adopted to fix.

The response is that this is a node-group topology gap, not a storage-layer one. Worth spelling out, because "per-AZ node groups fix it" is doing a lot of work in one clause.

The volume never moves, either way. An EBS volume lives in one AZ for its whole life, and a dynamically provisioned PV carries a zone requirement the scheduler treats as a hard constraint — so a pod is never placed somewhere it cannot attach. The failure is not a wrong-AZ mount attempt; it is a pod stuck `Pending` because nothing can give it a node in the right AZ. NIC ships one autoscaling group spanning every AZ, and Cluster Autoscaler cannot ask a multi-AZ ASG for capacity in a *specific* zone, so it may well add a node in the wrong one and leave the pod pending anyway. One node group per AZ (or Karpenter, which reads the PV's zone requirement natively) is what makes "give me a node in us-east-1b" an answerable request. So the pod comes back as soon as that AZ has capacity — and if the AZ is what failed, it comes back when the AZ does, instead of sitting in an unexplained pending state that looks like a storage bug.

That is a real cost, not a free fix. Per-AZ groups multiply the node-group config surface by the number of AZs — scaling limits, instance types, taints, labels, and GPU variants replicated per zone — and each group scales independently, so headroom is per-AZ too and utilisation drops. Karpenter avoids the fan-out but replaces cluster-autoscaler, which is another migration with its own surface. Neither is exotic; both are more configuration than the single ASG NIC ships today, and that difference belongs in the comparison rather than in a footnote.

The honest residual is availability: if the AZ itself is gone, EBS-backed volumes are stranded until it returns, where Longhorn would usually reattach from a surviving replica.

It is worth sizing that before treating it as decisive. Full-AZ outages are infrequent and usually measured in hours; recovery needs no operator action, since the volumes are intact and the pods reschedule when capacity comes back; and the workload is interactive analysis, not a customer-facing service, so the tolerable answer may simply be that affected users cannot work until the AZ returns. Two caveats keep that from being a finding: Nebari has no stated availability objective to check it against, so "acceptable" is a preference until someone writes the number down; and the things that genuinely should survive a lost AZ are databases, which belong to CNPG replication rather than to the storage layer. Longhorn's own survival is also softer than assumed — NIC sets replica zone anti-affinity to *soft* with two replicas, so both can be sitting in the AZ that just failed.

Either way the residual disappears in the RWX-homes shape, where a Multi-AZ filesystem has no AZ to be pinned to.

![Cross-AZ attachment](cross-az.svg)

## Topic: the other clouds

The uniformity argument is being made on two providers. NIC wires Longhorn on AWS and Hetzner; GCP, Azure, and local hardcode `LonghornEnabled: false` with a "not yet wired" comment. The clouds that would actually test "Longhorn everywhere" are the ones it has never run on — and on the evidence available, they are where the case for it is weakest.

Both ship RWX as a managed service rather than something to install: GKE has the Filestore CSI driver as a cluster addon (working on Autopilot as well as Standard), AKS ships the Azure Files CSI driver in-box with RWX storage classes. There is no ZFS-based service outside AWS, but the closest analogue exists on both — Google Cloud NetApp Volumes and Azure NetApp Files, ONTAP-based managed NAS sold on the same low-latency, provisioned-throughput property that makes FSx for OpenZFS a candidate here, and sitting above the commodity tier much as FSx sits above EFS. Whether that tier clears the home gate is the same open question on all three clouds, and the answer will probably travel.

The cross-AZ problem barely exists there. Multi-zone AKS on 1.29+ defaults its built-in StorageClasses to zone-redundant disks and fails stateful pods over to a healthy zone; GKE offers regional persistent disks and Hyperdisk Balanced High Availability as synchronously replicated cross-zone block storage. ADR-0002's founding motivation is an AWS property, not a cloud property. The same goes for the compute argument: GKE Autopilot forbids privileged pods and hostPath outright, which is stricter than EKS Auto Mode, so keeping Longhorn everywhere declines the managed-node model everywhere.

The counter is straightforward and unresolved: four providers with four managed filesystems means four CSI integrations, four Terraform surfaces, and four backup stories where Longhorn is one of each — and Hetzner keeps Longhorn in the project under every shape. That is what the portability cost gate prices.

All of this is desk research from vendor documentation, dated 2026-08-14, unverified against any Nebari deployment, and subject to version floors (GKE 1.33+ for Hyperdisk Balanced HA, AKS 1.29+ and multi-zone for the ZRS defaults). It is scoping for when those providers are wired, not evidence about them.

![The other clouds](other-clouds.svg)

## Topic: home volume access mode

Scoped separately because it decides how ambitious the AWS change is. Two shapes are live on AWS: the **split shape** moves only single-pod system volumes to gp3 and keeps homes and RWX on Longhorn; the **RWX-homes shape** puts homes on a managed RWX filesystem too, which removes Longhorn from AWS entirely. Homes are RWO today by deliberate pack design, on a performance rationale written against Longhorn's and EFS's data paths and silent about FSx for OpenZFS.

If FSx clears the home performance gate, moving homes onto it removes the per-user node pin, the cross-AZ objection, and Longhorn from AWS together. If it does not, the RWX-homes shape is dead and the split shape stands. Measurement decides, not argument.

### The first numbers

Environment creation measures **~3s on an RWO EBS volume, ~60s on EFS, ~56s on FSx for OpenZFS** (first pass, 2026-08-14). The two managed filesystems land in the same place, so this is the gap between local block storage and network file storage in general — not a gap a different appliance is likely to close. And it recurs: changing kernels forces a rebuild, which one developer can hit several times a day, so the ~20x is paid per rebuild in the middle of an interactive loop, not once per volume.

That is adverse, and it is not yet a verdict. A minute may be a perfectly good price for free scheduling, no AZ pin, and one less storage system to operate — but nobody has written down what rebuild latency is acceptable, and a measurement without a stated bar cannot settle anything. Two things to fix before more benchmarking: state the ceiling, and re-baseline against gp3, since NIC provisions gp2 today and the 3-second figure is what the managed filesystems are being judged against.

### Two pack-side prerequisites

Both are easy to miss because neither is storage-layer work, and neither is a blocker — they are listed so the shape is not proposed without them.

The per-user affinity term is unconditional, so RWX homes would stay pinned pointlessly until it is made conditional. That is small work on a pack the same maintainers own, not an external dependency.

Ownership is the subtler one. On a block volume the kubelet sets it: `fsGroup: 100` makes the volume root group-owned so the container's non-root user can write. NFS-backed CSI drivers report no fsGroup support, so the kubelet skips that step and a freshly provisioned export arrives owned by root, with the user unable to write to their own home. A dedicated per-user volume does not fix this by itself — nothing chowns it. The fix is to have the appliance do the job instead: FSx for OpenZFS takes root uid/gid and permissions as volume-creation parameters and controls root-squash, and EFS uses access points that force a POSIX user. So it is solvable per user; the point is that it moves from the pod spec to StorageClass parameters, and therefore has to be verified rather than assumed to carry over.

![Home volume access mode](homes.svg)
