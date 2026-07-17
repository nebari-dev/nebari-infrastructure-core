# ArgoCD project scoping

NIC installs three ArgoCD AppProjects:

- `foundational`: NIC's own platform apps. Its `sourceRepos` and `destinations`
  are derived automatically from NIC's app templates, so it accepts content only
  from the platform's known repos and deploys only into the platform namespaces.
- `nebari-apps`: the home for software packs. Lightly scoped (in-cluster server,
  the pack Helm repository plus the GitOps repo), with namespaces open so packs
  can create their own.
- `default`: locked down to deny-all. Do not use it.

## Deploying software packs

Software pack Applications must set `project: nebari-apps`. Do not add pack
Applications to the foundational apps path, and do not use `project: default`.

### Migrating a pack that currently uses `foundational`

```bash
kubectl patch application <pack> -n argocd --type merge \
  -p '{"spec":{"project":"nebari-apps"}}'
```

A pack left on `foundational` will fail to sync (its repo or namespace is not
permitted there).

## Security model (what this does and does not do)

This scoping is defense-in-depth and default-hardening. It stops content from
unapproved repos, confines foundational apps to known namespaces, and closes the
`default`-project escape hatch. It does NOT stop malicious content committed to an
approved repo, because the resource-kind whitelists remain open: a
ClusterRoleBinding or privileged pod committed to an approved source still
applies. Blocking dangerous resource kinds is the admission-controller work
([#480](https://github.com/nebari-dev/nebari-infrastructure-core/issues/480)).
Treat write access to the GitOps repo as cluster-admin-equivalent.

## Known limitation

ArgoCD's `sourceRepos` governs only the top-level Application source, not content
a Helm chart or remote kustomize base pulls in transitively.
