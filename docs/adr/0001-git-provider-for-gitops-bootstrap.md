# ADR-0001: Git Provider for GitOps Bootstrap

## Status

Proposed

## Date

2025-01-21

## Context

NIC delegates management of foundational software (cert-manager, ingress-nginx, JupyterHub, etc.) to ArgoCD following GitOps principles. ArgoCD requires a Git repository containing Application manifests that define what software to deploy and how to configure it.

This creates a bootstrapping requirement: NIC must generate ArgoCD Application manifests and push them to a Git repository that ArgoCD can watch. The question is how NIC should interact with Git repositories and what level of automation to provide.

Key considerations:
- NIC's role is to **bootstrap** the GitOps repository, not manage it ongoing
- ArgoCD becomes the source of truth for foundational software after bootstrap
- Users may have existing Git repositories they want to use
- Different organizations use different Git providers (GitHub, GitLab, Bitbucket, self-hosted)
- Both NIC (for pushing) and ArgoCD (for watching) need repository access

## Decision Drivers

- **Simplicity**: MVP should be implementable quickly with minimal complexity
- **Flexibility**: Support various Git hosting providers without provider lock-in
- **GitOps purity**: ArgoCD should own software configuration after initial bootstrap
- **Security**: Credentials must be handled securely for both NIC and ArgoCD
- **Idempotency**: Subsequent `nic deploy` runs should not disrupt existing configurations

## Considered Options

1. **Generic Git Operator only** - Use git CLI for all operations, require existing repo
2. **Provider-specific implementations** - Build API integrations for each Git provider
3. **Layered approach** - Generic Git Operator as base, optional provider extensions

## Decision Outcome

Chosen option: **Layered approach with MVP focusing on Generic Git Operator**

The implementation will be split into two phases:

**MVP (Phase 1)**: Implement `GitOperator` interface that uses git CLI operations. Users must provide an existing repository URL and authentication credentials via environment variables.

**Future (Phase 2)**: Add provider-specific implementations (GitHub, GitLab, etc.) that can create repositories via API, then delegate to `GitOperator` for actual git operations.

### Consequences

**Good:**
- MVP is simple and works with any Git host
- No provider-specific code needed initially
- Clean separation between repo setup (providers) and git operations (operator)
- Users retain full control over their Git repository

**Bad:**
- MVP requires manual repository creation
- No automated deploy key setup in MVP
- Users must configure ArgoCD repository credentials separately (via NIC-created secret)

## Detailed Design

### GitOperator Interface (MVP)

```go
// NOTE: Illustrative code - not production ready
package git

type GitOperator interface {
    // Init clones the repo if not present, or opens and pulls if it exists
    Init(ctx context.Context) error

    // WorkDir returns the local working directory path
    WorkDir() string

    // CommitAndPush stages all changes, commits, and pushes to remote
    CommitAndPush(ctx context.Context, message string) error

    // HasChanges returns true if there are uncommitted changes
    HasChanges(ctx context.Context) (bool, error)
}
```

### Configuration

```yaml
git_repository:
  url: "git@github.com:my-org/my-gitops.git"
  branch: main
  path: "clusters/my-nebari/"  # optional subdirectory
  auth:
    ssh_key_env: GIT_SSH_PRIVATE_KEY
    # or: token_env: GIT_TOKEN (for HTTPS)
```

### Bootstrap Flow

```
First `nic deploy`:
  1. Clone repository
  2. Check for .bootstrapped marker file
  3. If not found:
     a. Generate ArgoCD Application manifests
     b. Write .bootstrapped marker
     c. Commit and push
  4. Create Kubernetes Secret for ArgoCD repo access
  5. Continue with OpenTofu (infra + ArgoCD pointing at repo)

Subsequent `nic deploy`:
  1. Clone/pull repository
  2. Find .bootstrapped marker
  3. Skip manifest generation (log: "GitOps repo already initialized")
  4. Continue with OpenTofu

`nic deploy --regen-apps`:
  1. Clone/pull repository
  2. Ignore .bootstrapped marker
  3. Regenerate all manifests
  4. Commit and push
  5. Continue with OpenTofu
```

### ArgoCD Repository Access

NIC creates a Kubernetes Secret with the Git credentials for ArgoCD:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gitops-repo-creds
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repository
type: Opaque
stringData:
  url: git@github.com:my-org/my-gitops.git
  sshPrivateKey: |
    -----BEGIN OPENSSH PRIVATE KEY-----
    ...
```

The same credentials provided by the user (via environment variable) are used for both:
- NIC pushing manifests during bootstrap
- ArgoCD watching and pulling from the repository

### Future: Provider Extensions

```go
// NOTE: Illustrative code - not production ready
type GitProvider interface {
    GitOperator  // embeds base operations

    // CreateRepository creates a new repository
    CreateRepository(ctx context.Context, org, name string, private bool) (url string, err error)

    // CreateDeployKey adds a deploy key to the repository
    CreateDeployKey(ctx context.Context, repo, publicKey string, readOnly bool) error
}
```

Providers (GitHub, GitLab, etc.) would:
1. Create repository via API if needed
2. Optionally create deploy keys
3. Delegate all git operations to the embedded `GitOperator`

## Options Detail

### Option 1: Generic Git Operator Only

Use git CLI for clone, commit, push operations. User must provide existing repository.

**Pros:**
- Simple implementation
- Works with any Git host
- No API dependencies

**Cons:**
- Cannot create repositories programmatically
- Cannot automate deploy key setup
- More manual setup required

### Option 2: Provider-Specific Implementations

Build separate implementations for GitHub, GitLab, Bitbucket using their APIs.

**Pros:**
- Full automation (repo creation, deploy keys, webhooks)
- Better user experience

**Cons:**
- Significant implementation effort
- Must maintain multiple provider integrations
- API differences between providers
- Self-hosted instances may have compatibility issues

### Option 3: Layered Approach (Chosen)

Generic Git Operator as foundation, optional provider APIs for enhanced automation.

**Pros:**
- MVP ships quickly with universal Git support
- Providers can be added incrementally
- Clean separation of concerns
- Providers reuse Git Operator for actual operations

**Cons:**
- Initial MVP has less automation
- Two-phase implementation

## Links

- [ArgoCD Private Repositories](https://argo-cd.readthedocs.io/en/stable/user-guide/private-repositories/)
- [MADR Format](https://adr.github.io/madr/)
