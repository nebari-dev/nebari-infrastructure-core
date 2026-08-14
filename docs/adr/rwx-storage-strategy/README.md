# RWX storage as a Nebari platform capability

This is an argument map, not an ADR. It records what the team has argued about ReadWriteMany (RWX) storage on AWS — which claims are settled, which are open, and what evidence would close them — so the eventual ADR is written from a position everyone can inspect.

The source of truth is [`rwx-storage-strategy.argdown`](rwx-storage-strategy.argdown). Rendering is `make argdown`, which writes every SVG below from that one file; the sections in the source are the sections on this page. The source also carries two things this page does not: the acceptance gates each topic defers to (under *AWS decision gate*), and the running list of assumptions the argument leaves behind.

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

The response is that this is a node-group topology gap, not a storage-layer one — a rescheduled pod needs a node in its volume's AZ, which per-AZ node groups or Karpenter supply and NIC's single multi-AZ ASG does not. The residual is AZ-outage availability for EBS-backed volumes, to be priced rather than argued away.

![Cross-AZ attachment](cross-az.svg)

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
