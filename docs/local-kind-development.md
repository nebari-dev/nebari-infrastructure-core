# Local Development with Kind

## Prerequisites

- A container runtime: [Docker](https://docs.docker.com/get-docker/) or Podman
- Go 1.26+ (to build the `nic` binary)

NIC embeds the [kind](https://kind.sigs.k8s.io/) Go library, so the `kind` CLI does **not** need to be installed separately; it drives the detected container runtime directly.

## Quick Start

```bash
make build                                   # build the nic binary
./nic deploy -f examples/local-config.yaml
```

`nic deploy` with a `cluster.local` config:

1. Creates a Kind cluster named after `project_name` (`my-nebari-local` in the example config), reusing it if one already exists.
2. Mounts the default GitOps directory into the node (see below).
3. Installs MetalLB and derives its `IPAddressPool` from the Kind Docker network.
4. Bootstraps ArgoCD and the foundational apps (cert-manager, Envoy Gateway, Keycloak, etc.).

There is no `make localkind-up`; the local provider does all of this itself.

## GitOps Repository Modes

NIC reads `examples/local-config.yaml` and handles three scenarios automatically:

| Config | What happens |
|--------|-------------|
| No `git_repository` section | Auto-creates `~/.nic/gitops/{project_name}` (or `$TMPDIR/nebari-gitops-{project_name}` when there is no home directory) and mounts it into the cluster |
| `url: "file:///path/to/repo"` | Uses the matching `cluster.local.kind.extra_mounts` entry supplied by the user |
| `url: "git@github.com:..."` | No mount - ArgoCD pulls from the remote repo directly |

For local `file://` repos, the path is mounted into both the Kind node and the ArgoCD repo-server pod. ArgoCD reads commits and refs from `.git` and creates its own checkout; it does not consume the source working-tree files directly.

When initializing or committing to any local `file://` repo, NIC makes the repository root and Git-serving data under `.git` group/other-readable and traversable so the non-root ArgoCD repo-server can read committed content. This applies whether the repo is auto-generated or user-supplied. NIC preserves existing and special permission bits, and does not touch working-tree files, hooks, reflogs, the Git index, or unrelated `extra_mounts`.

Kind mounts are fixed at cluster creation time. If an existing cluster was created with a different local GitOps path, recreate it with `nic destroy -f examples/local-config.yaml` followed by `nic deploy -f examples/local-config.yaml`.

> **Note:** `file://` repos only work when the cluster nodes can access the local path (Kind, k3s, bare metal). For cloud providers (AWS, Azure, ...), use a remote git repository since Kubernetes nodes don't have access to your local filesystem.

## Using a Custom Config

```bash
./nic deploy -f ./my-config.yaml
```

## Teardown

```bash
./nic destroy -f examples/local-config.yaml   # deletes the Kind cluster
```

To rebuild from scratch, run `destroy` followed by `deploy`.

## Accessing Services

**ArgoCD UI:**
```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443
# Visit https://localhost:8080
# Username: admin
# Password:
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d
```

**Keycloak UI:**
```bash
kubectl port-forward svc/keycloak -n keycloak 8081:80
# Visit http://localhost:8081
```

## Networking

MetalLB is always enabled on local clusters (Kind has no built-in LoadBalancer). NIC derives MetalLB's `IPAddressPool` from the Kind node's Docker network - for example a `192.168.1.0/24` network yields `192.168.1.100-192.168.1.110`, and the default `172.18.0.0/16` kind network yields `172.18.255.100-172.18.255.110`. To pin the range, set `cluster.local.metallb.address_pool` in the config. Services of type `LoadBalancer` then become reachable from your host machine within that range.

## Troubleshooting

**Check pod status:**
```bash
kubectl get pods -A
```

**Check ArgoCD application sync:**
```bash
kubectl get applications -n argocd
```
