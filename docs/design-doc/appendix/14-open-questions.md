# Open Questions

Numbering note: this file is the chapter immediately following the operations section. Anchor links in older docs may reference these by "13.x" - that's stale; the file's section numbers below are the canonical ones.

## 14.1 Technical Questions

1. **Resolved**: OpenTofu via `pkg/tofu` (`terraform-exec` wrapper) is used by the AWS provider. Other providers do not use tofu (Hetzner uses `hetzner-k3s` directly; local uses Kind; existing is a no-op). See [ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md) for the proposed out-of-tree plugin direction that formalizes this.
2. **Multi-Cluster**: How to manage multiple clusters from one NIC invocation? Today: one cluster per `nic deploy` invocation. Still open.
3. **Custom Kubernetes Distributions**: Support for k0s, k3d, RKE2? Today: Kind for local, k3s for Hetzner, EKS for AWS. RKE2/k0s remain open.
4. **Helm Chart Storage**: Foundational charts live in `pkg/argocd/templates/apps/` as ArgoCD `Application` manifests that reference upstream Helm repositories. OCI mirroring for offline installs is still open.
5. **Operator HA**: Should the Nebari Operator run HA with leader election? Owned upstream at [`nebari-dev/nebari-operator`](https://github.com/nebari-dev/nebari-operator).

## 14.2 Configuration Questions

6. **Config Validation**: Today: custom Go validation in `pkg/config/config.go` (`NebariConfig.Validate`). JSON Schema export for IDE support remains open.
7. **Config Inheritance** (`extends`): Not implemented. See [`15-future-enhancements.md`](15-future-enhancements.md).
8. **Secrets Management**: **Resolved for MVP**: env vars via `.env` (loaded by `godotenv` in `cmd/nic/main.go`). Git auth uses env-var indirection (`ssh_key_env` / `token_env`). External Secrets Operator / Sealed Secrets / cloud secrets managers remain open as longer-term options.

## 14.3 Deployment Questions

9. **Rollback Strategy**: Should `nic rollback` exist? Still open. Today: re-apply a previous config.
10. **Blue/Green Cluster Deployments**: Future.
11. **Canary Deployments for foundational software updates**: Future (depends on ArgoCD's own progressive sync features).

## 14.4 Integration Questions

12. **CI/CD Templates**: Should NIC ship GitHub Actions / GitLab CI templates? Still open; the `git_repository:` consumption side is shipped, but template generation is not.
13. **Phone-Home Telemetry**: Should NIC emit opt-in usage telemetry? Still open.
14. **Marketplace Integration**: AWS/GCP Marketplace listings? Future.

## 14.5 Platform Automation Questions

15. **Git Repository Provisioning**: NIC **consumes** an existing GitOps repo today (`pkg/git`, `git_repository:` config). The **provisioning** side (auto-create the repo on GitHub/GitLab/Gitea, configure protections, etc.) is still open. See `15-future-enhancements.md` §2.

16. **CI/CD Workflow Generation**: Auto-generate validation/deploy/drift workflows. Still open.

## 14.6 Application Stack Questions

17. **Software Stack Specification**: Declarative specs for full platform stacks (databases, queues, apps). Still open. Today: user packs install themselves via ArgoCD using `NebariApp` CRs from the upstream operator.
18. **Full Stack in One Repo**: Still open. The GitOps repo layout is owned by NIC for the foundational set today; users overlay their own applications.
19. **Stack Templates & Marketplace**: Still open. The "Software Pack" concept exists in the broader Nebari ecosystem; a curated marketplace is future work.

## 14.7 Provider Plugin Architecture

20. **Out-of-Tree Provider Plugins** ([ADR-0004](../../adr/0004-out-of-tree-provider-plugins.md), Proposed): Open questions from the ADR include scope of plugin kinds, relationship to Nebari stages, credential model, validation without install, trust/signing, and migration of existing in-tree providers. These are tracked in the ADR rather than duplicated here.
