# ArgoCD project scoping

NIC installs three ArgoCD AppProjects:

- `foundational`: NIC's own platform apps. Its `sourceRepos` and `destinations`
  are derived automatically from NIC's app templates, so it accepts content only
  from the platform's known repos and deploys only into the platform namespaces.
- `nebari-apps`: the home for software packs. Scoped only to the in-cluster API
  server: `sourceRepos` is `'*'` and namespaces are open, so a pack can be
  installed from any chart source and can create its own namespaces.
- `default`: locked down to deny-all. Do not use it.

## Deploying software packs

Software pack Applications must set `project: nebari-apps`. Do not add pack
Applications to the foundational apps path, and do not use `project: default`.

Any chart source works: a Helm index
(`https://nebari-dev.github.io/helm-repository`), that index's OCI mirror
(`oci://quay.io/nebari/charts/<chart>`), a third-party registry, or a git repo.
The project does not restrict sources, so no NIC change is needed to install a
pack from a new location. A *private* source is a separate matter: the project
permits it, but ArgoCD still needs a repository credential for it, and NIC does
not create those automatically.

### Migrating a pack that currently uses `foundational`

```bash
kubectl patch application <pack> -n argocd --type merge \
  -p '{"spec":{"project":"nebari-apps"}}'
```

A pack left on `foundational` will fail to sync (its repo or namespace is not
permitted there).

## Security model (what this does and does not do)

This scoping is defense-in-depth and default-hardening, and it constrains the
three projects unevenly on purpose:

- `foundational` is the part that restricts sources. NIC's own control plane
  accepts content only from its derived repo list and deploys only into its
  derived namespaces.
- `nebari-apps` does **not** restrict sources. `sourceRepos: '*'` means an
  Application in this project may sync from any repository or registry the
  ArgoCD repo-server can reach. Putting an Application in `nebari-apps` is
  therefore equivalent to granting "install arbitrary charts into this cluster".
- `default` is deny-all, which closes ArgoCD's built-in project as an escape
  hatch.

None of this stops malicious content committed to a permitted source, because
the resource-kind whitelists remain open: a ClusterRoleBinding or privileged pod
from a permitted source still applies. Blocking dangerous resource kinds is the
admission-controller work
([#480](https://github.com/nebari-dev/nebari-infrastructure-core/issues/480)).
Treat write access to the GitOps repo as cluster-admin-equivalent.

Narrowing `nebari-apps` to an operator-declared allow-list of pack sources needs
a config surface that does not exist yet, tracked in
[#530](https://github.com/nebari-dev/nebari-infrastructure-core/issues/530).
[ADR-0010](../adr/0010-high-security-mode.md) records that allow-list as the
`hardened` posture.

## Known limitation

ArgoCD's `sourceRepos` governs only the top-level Application source, not content
a Helm chart or remote kustomize base pulls in transitively.
