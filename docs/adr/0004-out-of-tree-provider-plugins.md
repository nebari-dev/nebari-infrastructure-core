# ADR-0004: Out-of-Tree Provider Plugin Architecture

## Status

Proposed

## Date

2026-04-15

## Context

NIC's architecture already establishes a clear abstraction boundary: CLI commands depend only on the `provider.Provider` interface, never on concrete implementations. Today, all providers (AWS, GCP, Azure, local) live in-tree under `pkg/provider/`, and every NIC binary ships code for every supported provider.

As NIC's scope grows, this model has limits:

1. **Providers are not only cluster providers.** NIC needs (or will need) pluggable behavior for multiple categories:
   - Cluster infrastructure (AWS, GCP, Azure, Hetzner, local, bare-metal)
   - DNS management (Cloudflare, Route53, ASCOT, etc.)
   - Certificate issuance (Let's Encrypt, internal CA, etc.)
   - Git hosting (GitHub, GitLab, Gitea)
   - Software installers (ArgoCD, Flux, or direct Helm)
2. **Not every provider uses OpenTofu.** The Hetzner cluster provider is implemented directly against the Hetzner API without tofu at all. The `Provider` interface, not Terraform modules, is the real contract.
3. **Organizations have legitimate private integrations.** OpenTeams operates an internal DNS system called ASCOT that manages DNS records via pull requests to an infrastructure repo. An "ASCOT DNS provider" that accepts a fine-grained GitHub token and opens/merges PRs automatically is useful internally but has no business living in mainline NIC.
4. **Binary bloat and coupling.** A user deploying only to AWS carries GCP, Azure, Hetzner, and every DNS/cert integration in their binary. Release coordination gets harder as provider count grows.

The Terraform provider ecosystem is the obvious precedent: a small core, a stable plugin protocol, and providers distributed independently.

## Decision Drivers

- **Honor the existing abstraction boundary.** This ADR enforces, rather than invents, the principle already stated in `CLAUDE.md` that CLI should depend only on interfaces.
- **Enable private / org-specific providers.** ASCOT-class integrations must have a supported path that is not "fork NIC."
- **Right-size the binary.** Ship only what a given deployment needs.
- **Support multiple provider categories.** Not a single plugin interface, but several narrow ones.
- **Keep the happy path simple.** A first-time user on AWS should not have to think about plugins.
- **Protocol stability.** Once external plugins exist, the plugin contract becomes a public API and must be versioned deliberately.

## Considered Options

1. **Status quo**: keep all providers in-tree.
2. **Config-driven Terraform module registration**: external providers are just OpenTofu module references declared in config.
3. **Out-of-tree plugin binaries over gRPC** (this proposal): providers are separate binaries discovered and launched by NIC, speaking a versioned gRPC protocol via HashiCorp's `go-plugin`.

## Decision Outcome

Chosen option: **Option 3, out-of-tree plugin binaries over gRPC**, scoped as a proposal pending validation.

Rationale:

- Option 1 does not solve the ASCOT-class problem and does not scale across provider categories.
- Option 2 is disqualified by the Hetzner case: the `Provider` contract is the interface, not tofu modules. Forcing every provider through a tofu intermediary would exclude legitimate non-tofu implementations.
- Option 3 matches the shape of the problem: multiple small stable interfaces, independent release cadence per provider, clean story for private integrations.

### Consequences

**Good:**

- Core NIC binary stays small and focused on orchestration.
- Private providers (ASCOT, org-specific clouds) have a supported path.
- Enforces the abstraction boundary in `CLAUDE.md` at the release-artifact level, not just source code.
- Smaller per-kind interfaces are easier to evolve than one god-interface.
- Mix-and-match composition: swapping Cloudflare for ASCOT is a config change, not a fork.

**Bad:**

- Plugin protocol becomes a public API with all the stability obligations that implies.
- Security surface: NIC now executes arbitrary downloaded binaries; checksums and ideally signing become mandatory.
- Operational complexity: plugin install/upgrade/uninstall, offline installs, version pinning, two-tier (official vs community) trust model.
- Testing story is harder: cannot test every combination of cluster x dns x cert; need conformance suites per plugin kind.
- Debugging crosses a process boundary; telemetry must propagate across it.
- `nic validate` may not be able to fully validate provider-specific config without the plugin installed.

## Options Detail

### Option 1: Status quo (all providers in-tree)

Every provider lives under `pkg/provider/<name>/` and is compiled into the NIC binary. New providers require a PR to nebari-infrastructure-core.

**Pros:**

- Simplest possible implementation: nothing to build.
- One binary, one release, one test matrix under our control.
- No plugin protocol to version.

**Cons:**

- ASCOT and similar private integrations have no path except fork.
- Every user carries code for every provider.
- Release coordination gets worse as provider count grows.
- Does not scale across provider categories (cluster + dns + cert + git + software).

### Option 2: Config-driven Terraform module registration

External providers are declared in `config.yaml` as references to OpenTofu modules (git URLs, registry references). NIC templates them into the root module.

**Pros:**

- No Go code to load; no plugin protocol.
- Leverages existing OpenTofu ecosystem.

**Cons:**

- **Disqualifying**: assumes every provider is expressible as a tofu module. Hetzner already is not. ASCOT as a DNS provider (GitHub PR workflow) clearly is not.
- No place for provider-specific Go logic: credential validation, readiness checks, kubeconfig retrieval, structured outputs.
- Conflates "infrastructure as code" with "provider interface"; these are different concerns.

### Option 3: Out-of-tree plugin binaries over gRPC

Plugins are separate binaries (e.g. `nic-provider-<name>`) distributed independently. NIC discovers installed plugins, execs them, and speaks a versioned gRPC protocol via `hashicorp/go-plugin`.

**Shape:**

- **Multiple plugin kinds, each a narrow interface**: `ClusterProvider`, `DNSProvider`, `CertProvider`, `GitProvider`, `SoftwareProvider`. A deploy is an assembly of plugins, one per kind.
- **Provider SDK in its own repo** (`nebari-dev/nic-provider-sdk`), exporting gRPC stubs and a `Serve(Provider)` helper so writing a new provider is minimal boilerplate.
- **Discovery**: NIC looks in `$NIC_PLUGIN_DIR` (default `~/.nic/providers/`) for binaries named `nic-provider-<kind>-<name>`.
- **Known-provider manifest** (JSON, hosted by nebari-dev) maps blessed names like `cluster:aws` to repo + per-platform binary + checksum. NIC auto-installs blessed providers on first use.
- **Unknown providers**: `nic provider install <kind> <name> <url>` downloads, verifies checksum, drops binary in the plugin dir. Offline install via local path is supported.
- **Config pins version**: optional `provider_version` field per plugin for reproducibility.
- **Lifecycle commands**: `nic provider list / install / upgrade / uninstall`.
- **Protocol versioning**: explicit `ProtocolVersion` field; NIC refuses plugins speaking an incompatible protocol.
- **Telemetry**: plugins receive trace context over gRPC and emit spans back through NIC's exporter.
- **Config schema**: each plugin exposes a `Schema()` RPC returning its required/optional fields so `nic validate` can check provider-specific config.

**Orchestration:**

- Deploys are a DAG across plugin kinds (cluster provides LB endpoint, DNS consumes it, cert requires DNS working, software install requires cluster ready). This overlaps conceptually with Nebari's existing "stages" model and should be reconciled rather than reinvented.
- Structured, typed handoff between plugin kinds: cluster provider emits a defined output shape that DNS providers consume, not a free-form blob.

**Pros:**

- Supports private / org-specific providers (ASCOT) cleanly.
- Smaller core binary, independent release cadence per provider.
- Smaller per-kind interfaces age better than one monolithic contract.
- Well-trodden precedent: Terraform, kubectl plugins, Helm plugins.
- Enforces the existing abstraction boundary at the binary boundary.

**Cons:**

- Plugin protocol is a public API; breaking changes require protocol-version bumps and coordinated ecosystem rollout.
- Security: arbitrary binary execution; need checksums minimum, signing (cosign/sigstore) ideally.
- Testing combinatorics: per-kind conformance suites instead of end-to-end matrix.
- Two-tier ecosystem (official vs community) with its own social/UX problems.
- Extra operational surface: install, upgrade, offline, airgapped flows.

## Open Questions

1. **Scope of plugin kinds.** Where is the line? `ClusterProvider` is clearly a plugin kind; "install this specific Helm chart" probably isn't, it's config. A `SoftwareProvider` abstraction risks hollowing out the core. This needs deliberate limits before shipping.
2. **Relationship to Nebari stages.** The DAG orchestration across plugin kinds overlaps with Nebari's existing stage model. Unify or stay independent?
3. **Config schema per kind.** Does every plugin kind get its own top-level config block, or a generic `plugins:` map the plugin parses via its declared schema?
4. **Credential model.** How do plugins declare which env vars / secrets they need, and how does NIC surface missing credentials at validate time without loading plugin code it does not yet have?
5. **Validation without install.** Can `nic validate` meaningfully run before plugins are installed, or is auto-install-during-validate acceptable?
6. **Trust and signing.** Do we require signed plugins from day one, or start with checksums and add signing later? Either answer has a migration cost.
7. **Migration path.** Do existing in-tree providers (AWS, GCP, Azure, local) move out-of-tree too, or remain bundled as "first-party" while the plugin system serves only external providers?

## Links

- [ADR-0001: Git Provider for GitOps Bootstrap](0001-git-provider-for-gitops-bootstrap.md)
- [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)
- [Terraform Provider Protocol](https://developer.hashicorp.com/terraform/plugin/terraform-plugin-protocol)
- CLAUDE.md abstraction-boundaries guidance (in-repo)
