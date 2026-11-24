# ADR-0021: Workload Sandboxing for Software Packs

## Status

Proposed. Depends on the pack platform contract ([ADR-0023, #781](https://github.com/nebari-dev/nebari-infrastructure-core/pull/781)), also proposed: this ADR proposes sandboxing as promises in that contract, delivered through its P5 platform values.

## Date

2026-10-12

## Context

Software packs increasingly run code nobody has reviewed line by line, such as commands from a coding agent, notebook extensions that execute tool calls, or build steps. Pack authors want to limit what that code can do: which files it can touch, whether it can reach the network, and whether it can read the pod's credentials.

**Threat model.**

- **In scope:** an agent's own commands misbehaving, meaning code it runs because of a prompt injection, a compromised dependency or a mistake. Unconfined, that code can reach whatever credentials, files and network the pod can. Many agent-sandbox failures are this kind of policy leakage, not escape.
- **Out of scope:** an adversary determined to escape to the host. What this ADR delivers shares the host kernel and does not stop kernel exploits. Stronger isolation such as gVisor or Kata raises the cost of escape, but NIC does not claim it contains such an adversary either: even VMs may not reliably contain cyber-capable agents ([Trail of Bits, 2026-08-26](https://blog.trailofbits.com/2026/08/26/vms-wont-contain-cyber-capable-agents/)).
- **Also not covered:** misuse of what the agent is allowed to do. If an agent is given a credential for its job, such as a token to push code, a prompt injection can still trick it into misusing that credential. A sandbox limits what the agent and its commands can reach, not what the agent chooses to do with access it legitimately has.

**Why NIC is involved.** Whether a pack can do this depends on the cluster, for example the node's kernel or which container runtimes are installed, and a pack can't find that out for itself: under Argo CD, Helm renders without cluster access. Confining processes inside the pod with Landlock, a Linux kernel feature, needs no change to pod security, but needs node kernels with it enabled at a sufficient ABI, which packs cannot choose. Clusters mix node groups (architectures, images, GPU nodes), so support can differ within one cluster. Stronger isolation needs runtimes (such as gVisor or Kata) and node preparation only the cluster can provide, and network guarantees need enforcement in the cluster's network plugin.

**The pack platform contract.** ADR-0023 (#781) proposes how NIC states what packs may rely on: numbered promises, each with a page, a level (stable or experimental), entry rules and a conformance test that runs on every target. Platform facts reach packs as Helm values under `global.nebari` (P5), which NIC writes into the GitOps repository and the pack's Argo CD Application loads. An earlier revision of this ADR proposed its own versioned contract, with boundary levels and a mapping in `nic outputs`. That would have been a second, parallel contract, so this revision fits sandboxing into ADR-0023 instead.

## Decision Drivers

- **One contract for packs.** Sandboxing follows ADR-0023's promises, levels, entry rules and conformance tests, and does not define its own versioning.
- **Measure, don't predict.** What NIC tells packs is what it measured on the cluster's own nodes, not what a table says a node image should support.
- **Packs get plain values.** A pack never queries the cluster; everything it needs arrives as Helm values.
- **Grow in steps.** Start with what works today; add stronger isolation later as separate promises, without changing what packs already use.
- **Provider-neutral.** It works the same way on every cluster provider: capability flows through `InfraSettings` and provider interfaces, with no provider-name switches outside `pkg/providers/cluster/`.
- **Don't weaken pod security.** Everything runs under Pod Security `restricted`.
- **Keep NIC's part small.** Measure, report and label; NIC does not enforce the sandbox.
- **Fit high-security mode** ([ADR-0010](0010-high-security-mode.md)). Nothing here needs a privileged pod. A later option to fail the deploy on an unmet requirement would match `hardened`'s fail-closed deploys.

## Considered Options

1. Do less: document which node setups work, maintain an example launcher, and let operators tell packs what to use.
2. Predict support per node group from a NIC-maintained table of node images.
3. Detect the capability per node with Node Feature Discovery.
4. Measure at deploy and publish the result through the pack platform contract, as one promise per sandboxing capability.
5. As 4, but as one sandboxing promise that grows to cover every capability.
6. A separate sandboxing contract with its own versioned mapping and boundary levels (the earlier revision of this ADR).
7. Bake in one sandboxing tool (e.g. bubblewrap).
8. Go straight to gVisor or Kata as the only option.
9. Adopt a full sandbox platform (e.g. Agent Substrate).

## Decision Outcome

Chosen option: "Option 4". Sandboxing enters the pack platform contract as separate experimental promises, one per capability, each with its own page, entry-rule check, conformance test and graduation clock. This ADR proposes the first:

> **P19 (number provisional): In-pod self-restriction reported.** A pack is told, per node group, whether a process in its pod can restrict itself with Landlock, and at what Landlock ABI, as NIC observed it on the cluster's default runtime at deploy time, and whether NIC can steer a pod onto the groups where it was observed.

P19 promises the report, not Landlock: Landlock availability itself belongs on ADR-0023's "provided, not promised" list. The report is a Nebari-owned value about a Linux kernel interface; a Landlock ABI is a fact about that interface, not a portable isolation rating.

**Consumers.** [Javo](https://github.com/nebari-dev/javo-pack) runs commands from coding agents in user pods and already confines them with Landlock; it needs to know at install whether confinement can be established, and where to schedule the pods that use it. The near-term need is agents that run inside a user's own pod, such as a notebook extension or an agent in a hub terminal, whose commands sit next to the user's files and tokens ([review of #749](https://github.com/nebari-dev/nebari-infrastructure-core/pull/749#issuecomment-7051137389)). The same review suggests kagent as an agent platform; its sandboxing would use a NIC-provided runtime, so it is a consumer for a later runtime promise, not for P19.

**How it is accepted.** Under ADR-0023 a new promise is decided by Core through a platform RFD; this ADR is the design that RFD would link to. P19 would enter as experimental and planned.

Later capabilities, such as a user-space-kernel runtime (gVisor), a VM-isolated runtime (Kata) or a sandbox profile (for example no service-account token), each enter as their own promise through the same process. They are named by the property they provide, not the product, as ADR-0023's neutral-interface rule requires.

NIC does not enforce the sandbox; the pack does, by confining its own commands and refusing sandboxed work when it can't. NIC doesn't stop `nic deploy` on a cluster without Landlock; it reports it.

### What NIC does on `nic deploy`

1. **Name the node groups.** At provisioning, NIC sets a reserved label naming each node's group, `nebari.dev/node-group=<name>`, in every node-group definition it manages (Kind workers already carry it). It goes in the definition, not on nodes, because the cluster autoscaler builds new nodes, including scale-from-zero, from the definition. The label never changes with measurements, so measuring never changes infrastructure.
2. **Measure.** After the cluster is up, NIC runs the sandbox probe as one short-lived pod per node on the cluster's default runtime, under Pod Security `restricted`. Each checks that a non-root process can confine itself and its children with Landlock, and records the ABI (about 7 seconds in the prototype).
3. **Report per node group.** Every node counts, Ready or not. A group is `qualified` if every node was measured and passed, at the lowest ABI measured in it; `unsupported` if every node was measured and lacks Landlock at ABI 3 or newer; and `unknown` otherwise, including empty groups, unmeasured nodes, mixed results and groups that changed during measurement. The cluster is `qualified` if any group is, with `landlockAbi` the lowest observed among qualified groups.
4. **Publish values.** NIC writes the result into the P5 platform values (below) and shows a per-group summary at the end of `nic deploy` and in `nic outputs`. Before publishing placement, it checks the group label is in each qualified group's definition and on its live nodes.

The report is a snapshot: a group keeps its place in the placement until the next deploy, even if its nodes are replaced, and a group empty at the next deploy drops out until it is measured again. A changed placement re-renders the pack, which may restart or remove sandboxed workloads.

### What packs get

Under the P5 platform values:

```yaml
global:
  nebari:
    sandbox:
      landlock:
        state: qualified          # qualified / unknown / unsupported
        landlockAbi: 4            # lowest ABI observed among qualified groups
        groups:
          general: {state: qualified, landlockAbi: 6}
          ubuntu:  {state: qualified, landlockAbi: 4}
          legacy:  {state: unsupported}
          gpu:     {state: unknown}
        placement:
          available: true         # or false, with a reason
          groupLabel: nebari.dev/node-group
          affinity:               # selects every qualified group
            nodeAffinity:
              requiredDuringSchedulingIgnoredDuringExecution:
                nodeSelectorTerms:
                  - matchExpressions:
                      - {key: nebari.dev/node-group, operator: In, values: [general, ubuntu]}
```

A pack that needs a higher ABI than the cluster's `landlockAbi` picks the qualified groups at its minimum from `groups` and selects them by `groupLabel`. In this example a pack needing ABI 6 selects `general`.

`placement.available` means NIC can restrict pods to these groups, not that a pod will fit there (taints, resources and storage still apply). It is true only where the provider sets the group label in its node-group definitions and steering has been verified on that provider. Elsewhere, or when no group is qualified, it is `{available: false, reason: ...}`, and a pack that needs steering refuses the sandboxed work rather than scheduling anywhere.

**Other sandbox promises.** Every sandbox promise publishes under `global.nebari.sandbox.<capability>` with `state` and, where they apply, `groups` and `placement`, plus fields of its own (a later runtime promise would add `runtimeClassName`). P19's page sets this shape, and later sandbox promises follow it. Capabilities protect against different things (confining a pod's own commands, a VM boundary, keeping the service-account token out), so one never implies or substitutes for another, and nothing ranks them. A helper in the nebari-app library chart may later choose among alternatives a pack lists in its own order; no helper is needed for this ADR.

**Relationship to P5.** P5, as proposed in #781, tells packs not to rely on any `global.nebari` key outside its own table. This ADR asks that P5 also allow keys defined by other promise pages, carrying that promise's level, so that P19 and later promises can add keys without changing P5 each time.

### The P19 promise page (summary)

- **Packs may rely on:** a report on every target, true at deploy time for the nodes then in each `qualified` or `unsupported` group; when `placement.available` is true, placement selecting only `qualified` groups.
- **Packs must not rely on:** Landlock being available, any minimum ABI, `unknown` groups behaving either way, or placement as a security guarantee (nodes added after a deploy were not measured).
- **Per-target notes:** placement is unavailable on `existing`, Hetzner and Kind until steering is verified there; on `existing`, NIC reports what it measured without managing node groups.
- **Conformance test:** under `tests/user_journeys/journeys/pack_contract/`, it checks the report against an independent probe of every node, and that a reference pack either schedules onto a qualified group and confines a command, or reports why it can't and refuses, on every target without skipping.

### Rules

- **Fail closed.** When a pack's policy requires confinement and the report says it can't be provided (no group qualified at the ABI it needs, or placement unavailable), the pack refuses to launch the confined work: the agent, or each command. It never responds by turning its lock off and continuing unconfined. At runtime, the pack's launcher, in the workload's own container and security context, establishes every restriction the pack needs before running each untrusted command, and refuses to run it if any fails. An init container's restrictions don't carry over to the application container, so a check there is not enough.
- **Prove before promising.** A promise's conformance test checks every claim on every target.
- **State a trust model.** Every later sandbox promise says who is trusted, what hostile code may do, what NIC enforces, what the pack must enforce, and what is not covered.

### Landlock, and what it covers

This all concerns the cluster's default runtime: gVisor and Kata run their own kernel, and gVisor has no Landlock.

A non-root process, with no extra privileges, restricts itself and everything it starts: which files they can read or write (ABI 3 is the minimum here because it adds truncation, without which a write restriction can be sidestepped), which TCP ports they can bind or connect to (ABI 4 or newer; ports, not destination addresses), and, from ABI 6, connecting to abstract UNIX sockets and signalling processes outside the sandbox, if the launcher asks for those scopes. Below ABI 6, a confined command can still connect to abstract UNIX sockets shared by every container in the pod, and signal other processes as normal OS permissions allow; the probe demonstrates both on an ABI 4 kernel (Ubuntu 24.04, kernel 6.8, the Hetzner kernel line). The ABIs measured so far (up to 6) don't cover UDP. Restricting itself doesn't take rights away from file descriptors a process already holds, or credentials already in its environment, so a pack still keeps the service-account token out (`automountServiceAccountToken: false`) and blocks other network access itself, for example with a seccomp filter.

**Evidence so far.** NIC does not use this table; it measures on every deploy:

| Configuration | Result |
|---|---|
| AWS EKS 1.34, AL2023 x86_64 and arm64, Bottlerocket 1.70, kernel 6.12 | Measured: ABI 6 |
| Hetzner, Ubuntu 24.04 x86_64, kernel 6.8 | Measured: ABI 4 |
| AWS below 1.33; Azure below 1.35 | Expected none: the default kernel lines predate a usable Landlock (not measured) |

### Before ADR-0023 is accepted

These don't depend on ADR-0023 or P5 and can be built now: the probe image, measurement and the per-group report in `nic deploy` and `nic outputs` (for operators), the group label in each provider's node-group definitions, provider experiments with steering, and test fixtures that exercise the proposed value shape privately. The pack-facing `global.nebari.sandbox` values and any claim of conformance wait until P5 and P19 are agreed, so that no pack comes to depend on an interface before it is accepted.

### What a pack author does

Follow the rules above. Put the placement on every pod that runs untrusted commands, including pods the pack creates at runtime for users. Keep the service-account token and other credentials out of those pods. A pack that needs the ABI 6 scopes requires them rather than applying them best effort. Treat files those commands write as untrusted, since tools that run later outside the sandbox may execute them.

### Out of scope

- Installing gVisor or Kata, and defining profiles; each is a later promise. Kata belongs on ADR-0023's "provided, not promised" list until more than one target offers it, and profiles that restrict network traffic wait for NetworkPolicy enforcement (P16, [#691](https://github.com/nebari-dev/nebari-infrastructure-core/issues/691)).
- Creating sandbox pods: that's the pack's job, or that of a tool such as [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/).
- Choosing a runtime automatically.

### Security

It relaxes nothing: the probe and packs run under Pod Security `restricted`. It shares the host kernel, so it doesn't protect against kernel exploits. Group labels are set by NIC through each provider's node-group configuration, not by anything running in the cluster; ordinary node labels are still a scheduling hint, not proof of who set them.

### Consequences

**Good:**

- What packs are told is measured on the cluster's own nodes on every deploy, and a conformance test checks it.
- Placement works with the cluster autoscaler, including scale-from-zero for groups measured at the last deploy, and a measurement never changes infrastructure.
- One contract for packs; later sandboxing capabilities are added, graduated or removed one at a time without affecting the others.

**Bad:**

- `nic deploy` gains a step and NIC must maintain a multi-architecture probe image (mirrored for air-gapped installs).
- Empty groups stay `unknown` until a node is started in them and measured.
- Each provider must set the group label in its node-group definitions and have steering verified; so far only label propagation has been demonstrated, on EKS.
- Placement can go stale between deploys, so packs depend on their launcher refusing to run, which shows as a failed command or pod rather than a silent gap.

## Options Detail

### Option 1: Do less

NIC does most of this anyway, but every pack then has to work out separately what each cluster supports.

### Option 2: Predict from an image table

A table to maintain, wrong whenever vendors backport features or ship Landlock built in but switched off, and the prototype found it calling configurations `unknown` that measure as qualified.

### Option 3: Node Feature Discovery

An extra component on every node, and it labels nodes directly, which new nodes from the autoscaler don't inherit.

### Option 4: Separate promises per capability (chosen)

Each capability passes ADR-0023's entry rules on its own and graduates on its own clock. The cost is consistency between promises, handled by the shared shape and by combination promises owning combined tests.

### Option 5: One sandboxing promise

One page and one combined test, but later capabilities would enter as changes to an experimental page without the entry-rule check, the whole promise graduates or expires together, and a pack declaring it says little about what it actually uses.

### Option 6: Separate sandboxing contract

A second contract alongside ADR-0023, with its own versioning and vocabulary (its "levels" clash with ADR-0023's), and reserved names for things nothing yet uses.

### Option 7: One sandboxing tool

No route to stronger isolation.

### Option 8: gVisor or Kata only

Needs installing per provider, adds overhead and breaks some workloads; each can come later as its own promise.

### Option 9: Full sandbox platform

Runs its own runtimes from privileged pods, and would be a separate system to trust rather than something NIC provides.

The earlier revision's assessment of options 2, 3, 8 and 9 is in its design doc ([§20, background only](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/docs/design-doc/implementation/20-workload-sandboxing.md#option-2-node-group-labels-from-an-image-table)).

## Open Questions

- In which order should providers gain the group label and a verified steering journey (AKS, Hetzner, Kind)?
- Should NIC's AKS module expose an `os_sku` setting, so Azure clusters below Kubernetes 1.35 can use Ubuntu 24.04?
- Should an option fail `nic deploy` when no group is qualified, for `hardened` installs?

## Links

The sandbox probe and its reference launcher come into this repository with P19's conformance test, under `tests/user_journeys/journeys/pack_contract/`, so that they are reviewed and updated here; until then they are on the author's fork at a fixed commit. The design doc on the fork describes the earlier revision of this ADR and is background only; where they differ, this ADR applies.

- [ADR-0023: Pack Platform Contract (#781)](https://github.com/nebari-dev/nebari-infrastructure-core/pull/781)
- [Design doc for the earlier revision, background only (fork, fixed commit)](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/docs/design-doc/implementation/20-workload-sandboxing.md)
- [Sandbox probe and reference launcher (fork, fixed commit)](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/tests/sandbox-probe/README.md)
- [ADR-0010: High-Security Mode](0010-high-security-mode.md)
- [Trail of Bits: VMs won't contain cyber-capable agents](https://blog.trailofbits.com/2026/08/26/vms-wont-contain-cyber-capable-agents/)
- [Landlock](https://docs.kernel.org/security/landlock.html)
- [go-landlock](https://github.com/landlock-lsm/go-landlock)
- [Cluster Autoscaler: node group labels and scale-from-zero](https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/FAQ.md)
- [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/)
