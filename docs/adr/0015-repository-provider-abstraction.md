# ADR-0015: Repository Provider Abstraction for GitOps Bootstrap

## Status

Accepted · Amended (2026-08): the cluster-compatibility check on `LocalSource` was dropped; see [Update](#update-2026-08-local-repositories-are-not-gated-on-the-cluster-provider).

Supersedes [ADR-0001](0001-git-provider-for-gitops-bootstrap.md).

## Date

2026-07-10

## Context

ADR-0001 introduced GitOps bootstrap through a flat `git_repository:` config block and a monolithic `git.Client` interface whose methods mixed two concerns: acquiring a repository (clone-or-init, auth validation) and operating on a working copy (commit, push, bootstrap markers). That design had several problems as the codebase grew:

- `pkg/config` imported `pkg/git` so the config schema could embed `git.Config`, coupling configuration to a go-git-backed implementation detail.
- The repository configuration was a one-off pattern. Cluster and DNS use a provider block where the provider name is the map key (`cluster: aws:`, `dns: cloudflare:`), each backed by a registry entry, so repository configuration was the odd one out (issue #117, part of the #116 config standardization).
- The "clone if remote, init if local" dispatch lived inside the git client, decided by inspecting the URL for a `file://` prefix, so the client had to understand repository semantics that belong to a higher layer.
- There was no seam for alternative repository strategies (a provider that creates the repository via a forge API, for example) without changing the config format.

## Decision Drivers

- **Consistency**: repository configuration should follow the same provider pattern as `cluster:` and `dns:`.
- **Decoupling**: `pkg/config` must not depend on `pkg/git`; go-git must stay sealed inside `pkg/git`.
- **Extensibility**: adding a forge-backed provider (GitHub, Gitea, ...) later must not change the config format or the consumers.
- **Security**: config files carry only environment-variable names; resolved credentials must live only in memory and never be serialized.

## Considered Options

1. Keep the flat `git_repository:` block and refactor only the git client internals.
2. `git_provider:` + `git_config:` sibling keys, as originally sketched in issue #117.
3. A `repository:` provider block (provider name as map key) with a sealed `Source` contract between providers and consumers.

## Decision Outcome

Chosen option: **a `repository:` provider block with a sealed `Source` contract**, because it makes repository configuration structurally identical to the cluster and DNS blocks and gives every downstream consumer a typed, provider-agnostic description of the repository.

### Configuration

```yaml
repository:
  existing:
    url: "git@github.com:my-org/my-gitops-repo.git"
    branch: main
    path: "clusters/my-nebari"   # optional subdirectory
    auth:
      ssh:
        env: GIT_SSH_PRIVATE_KEY
      # or: token: { env: GIT_TOKEN }
    argocd_auth:                  # optional read-only credentials for ArgoCD
      token:
        env: ARGOCD_GIT_TOKEN

# or, for local/dev clusters (kind):
repository:
  local:
    path: /home/user/my-gitops    # optional; defaults to ~/.nic/gitops/<project_name>
```

The `repository:` block is **required**. The previous implicit modes (auto-created local directory on kind, silently skipping GitOps bootstrap on cloud clusters without a git config) are gone: the skip mode produced a cluster where ArgoCD managed nothing, since every foundational service is synced from the repository via the root App-of-Apps. A config without the block now fails validation with guidance instead of deploying a half-functional cluster.

### Package layout

- **`pkg/providers/repository`** (the contract; imports only `pkg/config`, free of go-git and Kubernetes):
  - `Provider` interface: `Name()`, `Validate(ctx, project, *config.RepositoryConfig)`, `Provision(ctx, project, *config.RepositoryConfig) (Source, error)`.
  - `Source` is a sealed interface with two kinds: `LocalSource{Dir, Branch, Path}` and `RemoteSource{URL, Branch, Path, PushAuth, ReadAuth}`. Shared accessors (`RepoURL()`, `GetBranch()`, `RepoPath()`) serve consumers that do not care about the kind; genuine dispatch points type-switch on the concrete kinds.
  - `Auth` is a sealed interface with two kinds: `TokenAuth` and `SSHKeyAuth`, holding values already resolved from their environment variables. `RemoteSource.ArgoCDAuth()` returns `ReadAuth`, falling back to `PushAuth` when no separate read credential is configured.
- **`pkg/providers/repository/existing`**: resolves a pre-existing remote repository into a `RemoteSource`. Auth config is a tagged union (`auth: token: {env: X}` or `auth: ssh: {env: X}`) validated as exactly-one-of.
- **`pkg/providers/repository/local`**: provisions a directory on disk as a `LocalSource`, defaulting to `~/.nic/gitops/<project_name>` (falling back to a per-project directory under the OS temp dir only when the home directory cannot be resolved). The zero-dependency option for local/dev clusters.
- **`pkg/git`**: rewritten as a concrete `Client` struct with no interface and no upward dependencies. The acquisition split is explicit: `Init(ctx, dir)` opens or initializes a local repository in place; `ValidateAuth`/`Clone` acquire a remote one into a managed temp dir. `CommitAndPush` is split into `Commit` and `Push` so local repositories simply never push.

### Wiring

`pkg/nic` is the only bridge between the contract and the git client: it resolves the configured provider from the registry, calls `Provision` (after cluster deploy, so future providers may target in-cluster forges), enforced cluster compatibility (a `LocalSource` required a cluster provider with `SupportsLocalGitOps`; removed, see the Update below), and type-switches the `Source` to drive the git client. `pkg/argocd` consumes the `Source` plus a working-directory string: a `LocalSource` becomes a hostPath mount into the repo-server, a `RemoteSource` becomes a repository-credentials Secret using `ArgoCDAuth()`.

Credentials are resolved from environment variables inside `Provision` and exist only in the returned `Source`; the config that gets committed back to the GitOps repository carries only the env-var names.

### Consequences

**Good:**
- `repository:` is structurally identical to `cluster:` and `dns:`, registry-backed, and open to new providers without config-format changes.
- `pkg/config` no longer imports `pkg/git`; go-git stays sealed inside `pkg/git`; the contract package is dependency-light for out-of-tree consumers.
- Local-versus-remote behavior is explicit in types rather than inferred from URL prefixes.
- Resolved credentials are never serializable as part of the config.
- The `nic-config.yaml` committed to the GitOps repository now carries the `repository:` block verbatim, including auth env-var names. This deliberately reverses the earlier scrub, which recorded a false empty `auth:` block: the names are identifiers rather than secrets, and committing them keeps the record a single source of truth with the operator's config.

**Bad:**
- The `repository:` block is a breaking config change: `git_repository:` users must migrate, and configs that previously omitted git entirely must now choose a provider.
- A no-GitOps deployment mode no longer exists; if a real use case appears it needs an explicit provider (e.g. `none`) rather than an omission.

## Options Detail

### Option 1: Keep `git_repository:`, refactor internals only

**Pros:**
- No config migration.

**Cons:**
- Keeps the config coupled to `pkg/git` types and the one-off block shape issue #116/#117 set out to remove.
- No seam for alternative repository strategies.

### Option 2: `git_provider:` + `git_config:` sibling keys

**Pros:**
- Matches the original issue #117 sketch.

**Cons:**
- By the time of implementation, cluster and DNS had settled on the provider-name-as-map-key shape, so sibling keys would have introduced a third pattern instead of converging on one.

### Option 3: `repository:` provider block with sealed `Source` (chosen)

**Pros:**
- Consistent with the sibling provider blocks; typed contract; sealed kinds keep dispatch exhaustive.

**Cons:**
- Breaking config change (documented in the PR migration notes).

## Update (2026-08): local repositories are not gated on the cluster provider

As implemented, `pkg/nic` rejected a `LocalSource` on any cluster provider whose
`InfraSettings` did not set `SupportsLocalGitOps`, which in practice meant the
kind-backed `local` provider and nothing else. The gate was stricter than the
behavior it modelled and than what NIC did before this ADR, where an explicit
`git_repository: {url: "file://..."}` was honored on any cluster and
`SupportsLocalGitOps` only decided whether NIC would *auto-create* a default
GitOps directory when none was configured.

What the repo-server actually needs is for the directory to be readable from the
cluster's nodes, and that is a property of how the cluster was built, not of
which provider NIC used to reach it. A k3d or minikube cluster adopted through
`cluster.existing` satisfies it whenever the operator mounted the directory into
the node containers, which is what
[action-nebari-sandbox](https://github.com/nebari-dev/action-nebari-sandbox) does
to run the whole platform stack in CI without a remote GitOps repository. NIC
cannot distinguish that cluster from an EKS one by inspecting config, so it now
declines to guess: the pairing is accepted everywhere and a cluster whose nodes
cannot see the path fails at ArgoCD sync time instead of at validation.

The check (`ensureLocalRepositorySupported`, plus the post-provision source-kind
re-check in `Deploy`) and the now-unused `InfraSettings.SupportsLocalGitOps`
field are removed. Nothing about the provider contract or the sealed `Source`
kinds changes.

## Links

- Supersedes [ADR-0001](0001-git-provider-for-gitops-bootstrap.md)
- [Issue #117](https://github.com/nebari-dev/nebari-infrastructure-core/issues/117) - original refactor proposal
- [PR #439](https://github.com/nebari-dev/nebari-infrastructure-core/pull/439) - implementation
- [go-git](https://github.com/go-git/go-git) - pure Go git implementation
