# ADR-0021: Workload Sandboxing Capability Contract for Software Packs

## Status

Proposed.

## Date

2026-10-12

## Context

Software packs increasingly run code nobody has reviewed line by line, such as commands from a coding agent, notebook extensions that execute tool calls, or build steps. Pack authors want to limit what that code can do: which files it can touch, whether it can reach the network, and whether it can read the pod's credentials.

**Threat model.**

- **In scope:** an agent's own commands misbehaving, meaning code it runs because of a prompt injection, a compromised dependency or a mistake. Unconfined, that code can reach whatever credentials, files and network the pod can. Many agent-sandbox failures are this kind of policy leakage, not escape.
- **Out of scope:** an adversary determined to escape to the host. What this ADR delivers shares the host kernel and does not stop kernel exploits. Stronger levels such as gVisor or Kata raise the cost of escape, but NIC does not claim they contain such an adversary either: even VMs may not reliably contain cyber-capable agents ([Trail of Bits, 2026-08-26](https://blog.trailofbits.com/2026/08/26/vms-wont-contain-cyber-capable-agents/)).
- **Also not covered:** misuse of what the agent is allowed to do. If an agent is given a credential for its job, such as a token to push code, a prompt injection can still trick it into misusing that credential. A sandbox limits what the agent and its commands can reach, not what the agent chooses to do with access it legitimately has.

**Why NIC is involved.** Whether a pack can do this depends on the cluster, for example the node's kernel or which container runtimes are installed, and a pack can't find that out for itself. Confining processes inside the pod with Landlock, a Linux kernel feature, needs no change to pod security, but needs node kernels with it enabled at a sufficient ABI, which packs cannot choose. Stronger isolation needs runtimes (such as gVisor or Kata) and node preparation only the cluster can provide, and network guarantees need enforcement in the cluster's network plugin.

## Decision Drivers

- **Grow in steps.** Start with what works today; add stronger options later without breaking packs that already use the contract.
- **Packs get plain values.** A pack never queries the cluster; everything it needs arrives as Helm values.
- **Name the protection, not the tool, but test it with a real tool.** Packs ask for a kind of protection. Every promise is backed by a test with a concrete consumer.
- **Provider-neutral.** It works the same way on every cluster provider: capability flows through `InfraSettings`, with no provider-name switches outside `pkg/providers/cluster/`.
- **Don't weaken pod security.** Prefer settings that pass Pod Security `restricted`.
- **Keep NIC's part small.** Report what NIC knows; add configuration or components only when a real pack needs them.
- **Fit high-security mode** ([ADR-0010](0010-high-security-mode.md)). Nothing here needs a privileged pod, so it works under `hardened`'s admission rules. A later option to fail the deploy on an unmet requirement would match `hardened`'s fail-closed deploys.

## Considered Options

Listed from least to most work for NIC.

1. Do less: document which node setups work, maintain an example launcher, let operators give packs the exact runtime name (a `RuntimeClass`) as a Helm value, treat NetworkPolicy as general infrastructure work, and leave sandbox lifecycle to Agent Sandbox.
2. Label qualifying node groups from a NIC-maintained table of node images.
3. Detect the capability per node with Node Feature Discovery.
4. A tool-neutral contract with a published mapping of concrete values, starting with a tested description of Landlock on the normal runtime.
5. Bake in one sandboxing tool (e.g. bubblewrap).
6. Go straight to gVisor or Kata as the only option.
7. Adopt a full sandbox platform (e.g. Agent Substrate).

## Decision Outcome

Chosen option: "Option 4". This ADR sets out the contract below. For now it delivers only one thing: a tested description of where Landlock works on the cluster's normal runtime, which `nic outputs` reports for each cluster. NIC does not enforce the sandbox; the pack does, by confining its own commands and refusing sandboxed work when it can't. NIC also doesn't stop `nic deploy` on a cluster that can't support it; it only reports it. A setting to make the deploy fail in that case can be added later without changing the contract.

The contract gives NIC a shared vocabulary for "what sandboxing this cluster supports", and ready-made settings for packs. Packs then ask for a kind of protection, not a specific tool, and the cluster's options can grow (for example adding gVisor or Kata) without packs having to change.

### The contract

The contract describes a cluster's sandboxing support in three independent parts:

| Axis | Values | In this ADR |
|---|---|---|
| **Boundary level** between pod and host | `shared-kernel`, `mediated-kernel`, `hardware-virtualised` | `shared-kernel` (the default runtime); the other two reserved |
| **In-pod capability** a workload uses inside its pod | `process-self-restriction`, `seccomp-user-notification`, `in-pod-namespaces` | `process-self-restriction` described; the other two reserved |
| **Profile** of guarantees enforced around the pod | Compositions of atomic guarantees | Reserved; none defined |

**Qualified** means a NIC maintainer has tested a configuration (provider, node image, architecture, kernel line and container runtime version) with a working consumer, such as the sandbox probe, and recorded the result in the qualification record. It is a tested claim about that one configuration, not an ongoing promise of support.

**Boundary levels.** At `shared-kernel`, code calls the host kernel directly; at `mediated-kernel`, it never makes system calls directly to the host kernel (for example gVisor); at `hardware-virtualised`, a hardware virtualisation boundary and a guest kernel separate it from the host (for example Kata).

Levels are an acceptance policy: an operator who accepts "at least `mediated-kernel`" accepts any qualified implementation at that level or above. This is a NIC policy convention, not an ecosystem standard or a security ranking: GKE, AKS and `RuntimeClass` expose concrete runtimes, and a user-space kernel and a VM protect in different ways. A higher level never promises that a pack runs unchanged (gVisor has no Landlock), so capabilities are never implied by a level, and NIC never substitutes one implementation for another on its own.

**In-pod capabilities.** An in-pod capability is something a workload does to itself inside its pod, such as confining its own commands with Landlock (`process-self-restriction`, described under Decision Outcome). Capabilities are separate from levels because a level never implies them.

**Profiles.** A profile is a set of guarantees the cluster enforces around a pod and the pod cannot change, composed of atomic guarantees that can each be enforced and checked from outside the workload, such as `no-service-account-token`, `no-secret-references`, `metadata-unreachable` or `default-deny-off-pod-egress`. Profiles are independent of the boundary level and form part of a requirement. NIC makes no blanket "no credentials" or "no network" claim: admission cannot see credentials in images, literal values or data, and "network" must say whether it covers off-pod, loopback and sidecar, own-node and request-layer traffic. Later ADRs define named profiles.

**The published mapping.** To be sandboxed, a pod needs specific settings: today its security context, and later things like which container runtime to use and which nodes to run on. Some of these will differ between clusters, and a pack can't look them up itself: under ArgoCD, Helm can't read the cluster, and a runtime has to be named literally. So NIC publishes them, and they reach the pack as ordinary Helm values, like the domain or storage class.

NIC shows the mapping in `nic outputs`. It lists named requirements, each with concrete values for a pod spec. A pack that creates sandbox pods through [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/) (a Kubernetes SIG project for creating sandbox pods on demand) puts the same values in its `SandboxTemplate` instead. Today there is one entry, `default`, the cluster's normal runtime:

```yaml
version: 1
requirements:
  default:
    level: shared-kernel
    state: qualified          # or unknown / unsupported, derived from the node groups
    capabilities: [process-self-restriction]   # listed only when qualified
    podSecurityContext:       # together these meet Pod Security "restricted"
      runAsNonRoot: true
      seccompProfile: {type: RuntimeDefault}
    containerSecurityContext:
      allowPrivilegeEscalation: false
      capabilities: {drop: [ALL]}
```

**How values reach a pack.** Today's `default` security settings are the same on every cluster, so a pack author ships them as chart defaults and nobody copies anything. The only part that varies by cluster today is `state`. The operator, who reads `nic outputs`, uses it to decide whether to turn on a pack's sandboxed features. The pack's startup check still refuses sandboxed work on any node that can't enforce it. Once entries carry values that do vary by cluster, such as a `runtimeClassName` or `placement`, the operator sets them when installing a pack, as they already do for the domain and storage class. With pack codegen ([ADR-0003](0003-software-pack-codegen.md)), NIC would fill them in automatically for packs whose `values.yaml` has a `sandbox` key, the same convention ADR-0003 proposes for `sharedStorage.storageClass`.

Later entries add fields such as `runtimeClassName`, `placement`, `workspaceStorageClass`, `profile` and `implementation`. These names are reserved now so that adding them breaks nothing. Consumers ignore fields they don't recognise, and an incompatible change bumps `version`.

A later entry might name a `RuntimeClass` that NIC installs (named after the implementation, such as `nebari-gvisor`), a provider-managed one (such as AKS's `kata-vm-isolation`), or the operator's own on an `existing` cluster, together with storage known to work with it. NIC never selects a class automatically in this ADR. Labels on runtime classes are inventory only, and nothing selects a class by label. If automatic selection is added later, finding no match or more than one must be treated as a failure.

**What NIC's report means.** It is an expectation, not a guarantee. `state: qualified` means a matching configuration passed the probe and is in the qualification record. `unknown` means nobody has tested it, and NIC never upgrades it to `qualified` by comparing kernel versions. `nic outputs` also shows each node's kernel version and the Landlock version that kernel should provide (the *baseline*), but only as a hint: vendors backport features, and Landlock can be built in but not switched on. Whether a sandbox actually holds is decided only by the pack's startup check, on each node, every time. All of this concerns the cluster's normal runtime: gVisor and Kata run their own kernel, so later entries cover them.

**Rules.**

- **Fail closed.** If a pack cannot establish its sandbox, it refuses the work; it never falls back to weaker settings.
- **Prove before promising.** Nothing is promised for a configuration until it is qualified.
- **State a trust model.** Every later level, capability or profile says who is trusted, what hostile code may do, what NIC enforces, what the pack must enforce, and what is not covered.

### Delivered now

**`process-self-restriction`.** A non-root process, with no extra privileges, restricts itself and everything it starts using Landlock: which files they can read or write (Landlock ABI 3 or newer), and which TCP ports they can connect to or listen on (ABI 4 or newer). Landlock doesn't cover UDP, files already open, or credentials already in the environment. So a pack still keeps the service-account token out (`automountServiceAccountToken: false`) and blocks other network access itself, for example with a seccomp filter. The other in-pod capabilities stay reserved.

**Where it is qualified today:**

| Configuration | Expectation |
|---|---|
| AWS, AL2023 x86_64, kernel 6.12 (Kubernetes 1.33–1.35) | Qualified, Landlock ABI 6 |
| Hetzner, Ubuntu 24.04 x86_64, kernel 6.8 | Qualified, ABI 4 |
| AWS below 1.33; Azure below 1.35 | Unsupported (kernel too old) |
| Everything else (ARM, GPU or Bottlerocket images, AKS from 1.35, `existing`, kind) | Unknown until probed |

**Reporting.** Each cluster provider tells NIC, through `InfraSettings`, what it expects for each node group. `nic outputs`, and the end of `nic deploy`, show that, plus each node's kernel version as a hint. It never fails a deploy, labels nodes or adds configuration.

The details are in [§20](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/docs/design-doc/implementation/20-workload-sandboxing.md): the probe, the example launcher, the full qualification record and how to redo it, the report format, and the test runs of gVisor, Kata and bubblewrap that shaped the contract.

### What a pack author does

**Today.** Ship the `default` security settings as chart defaults. Keep the service-account token and other credentials out of pods that run agent commands. At startup, check the Landlock version and refuse sandboxed work if it's too old. Confine each command (the example launcher shows how). Treat files those commands write as untrusted, since tools that run later outside the sandbox may execute them.

**Later.** A pod that runs untrusted code uses a mapping entry naming a level and a profile, and the pack writes no isolation code.

### Out of scope

- Installing gVisor or Kata, and defining profiles (which first need NetworkPolicy enforcement, [#691](https://github.com/nebari-dev/nebari-infrastructure-core/issues/691)).
- Creating sandbox pods: that's the pack's job, or that of a tool such as Agent Sandbox.
- Choosing a runtime automatically.

### Security

It relaxes nothing and runs under Pod Security `restricted`. It shares the host kernel, so it doesn't protect against kernel exploits.

### Consequences

**Good:**

- A tested description of what the cluster's normal runtime offers, with no new components or configuration.
- Packs get plain values, and later levels, capabilities and profiles can be added without breaking them.

**Bad:**

- Most configurations are unknown until someone probes them, and older kernels don't qualify.
- NIC doesn't block a deploy or steer pods away from unsuitable nodes; packs rely on their startup check.
- Maintainers must keep the qualification record up to date.
- The mapping is versioned before it has a second entry, and wiring values into packs is manual until ADR-0003.

## Options Detail

### Option 1: Do less

NIC does all of this anyway, but without a shared description every pack has to work out separately what each cluster supports.

### Option 2: Label node groups from an image table

A table to maintain for steering that helps little, and it doesn't confirm Landlock is switched on.

### Option 3: Node Feature Discovery

An extra component with the same limits as 2.

### Option 4: Tool-neutral contract (chosen)

Usable now, cheap, nothing relaxed, not tied to one tool.

### Option 5: One sandboxing tool

No route to stronger isolation.

### Option 6: gVisor or Kata only

Needs installing per provider, adds overhead and breaks some workloads; kept as the reserved levels.

### Option 7: Full sandbox platform

Pre-1.0, runs its own runtimes from privileged pods, and would be a separate system to trust rather than a level NIC provides.

The full assessment of options 2, 3, 6 and 7 is in [§20](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/docs/design-doc/implementation/20-workload-sandboxing.md#option-2-node-group-labels-from-an-image-table).

## Open Questions

- Should NIC's AKS module expose an `os_sku` setting, so Azure clusters below Kubernetes 1.35 can use Ubuntu 24.04?
- Should the Pack Specification's security baseline point at this contract?
- Is it worth automating the probe as short-lived pods that `nic outputs` runs on each node group, at the cost of maintaining a multi-architecture image?

## Links

The supporting design doc and the sandbox probe below are linked at a fixed commit on the author's fork; they are proposed for this repository together with the implementation, not with this ADR.

- [Design doc: Workload Sandboxing (fork, fixed commit)](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/docs/design-doc/implementation/20-workload-sandboxing.md)
- [Sandbox probe and reference launcher (fork, fixed commit)](https://github.com/nenb/nebari-infrastructure-core/blob/bca13609252691012ad1dc495f90db47d38d7a69/tests/sandbox-probe/README.md)
- [ADR-0003: Software Pack Codegen](0003-software-pack-codegen.md)
- [ADR-0010: High-Security Mode](0010-high-security-mode.md)
- [Trail of Bits: VMs won't contain cyber-capable agents](https://blog.trailofbits.com/2026/08/26/vms-wont-contain-cyber-capable-agents/)
- [Landlock](https://docs.kernel.org/security/landlock.html)
- [go-landlock](https://github.com/landlock-lsm/go-landlock)
- [Kubernetes NodeRestriction admission plugin](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#noderestriction)
- [Agent Sandbox](https://agent-sandbox.sigs.k8s.io/docs/)
