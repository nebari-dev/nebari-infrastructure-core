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
3. Publishes the gateway on host ports 80/443 of `127.0.0.1` (see Networking below).
4. Bootstraps ArgoCD and the foundational apps (cert-manager, Envoy Gateway, Keycloak, etc.).

There is no `make localkind-up`; the local provider does all of this itself.

## GitOps Repository Modes

NIC reads `examples/local-config.yaml` and handles three scenarios automatically:

| Config | What happens |
|--------|-------------|
| `repository: { local: {} }` | Auto-creates `~/.nic/gitops/{project_name}` (or `$TMPDIR/nebari-gitops-{project_name}` when there is no home directory) and mounts it into the cluster |
| `repository.local.path: /path/to/repo` | Uses the matching `cluster.local.kind.extra_mounts` entry supplied by the user |
| `repository.existing.url: "git@github.com:..."` | No mount - ArgoCD pulls from the remote repo directly |

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
# Password (along with the other platform entry points):
nic outputs --show-secrets
```

**Keycloak UI:**
```bash
kubectl port-forward svc/keycloak -n keycloak 8081:80
# Visit http://localhost:8081
```

## Networking

The gateway is published on host ports of `127.0.0.1`. NIC pins the gateway's Envoy service to fixed NodePorts and maps them to host ports 80 and 443 (or `cluster.local.https_port`) when it creates the Kind cluster, so no LoadBalancer, MetalLB, or extra host tooling (such as docker-mac-net-connect on macOS) is involved. This works the same on macOS, Linux, and Windows because published ports are plain Docker port mappings.

To reach the platform, point the hostnames at loopback in `/etc/hosts` (the deploy output prints this line too):

```
127.0.0.1 nebari.local keycloak.nebari.local argocd.nebari.local
```

`/etc/hosts` has no wildcard support, so services exposed later on other subdomains need their hostname appended to the same line.

One caveat follows from using host ports: ports 80 and 443 must be free on your machine, and only one local cluster can own them at a time. Set `cluster.local.http_port` and `cluster.local.https_port` to run a second cluster, to avoid a conflict with services already using 80/443, or on rootless Docker/Podman, which cannot bind ports below 1024. Kind port mappings are fixed at cluster creation, so changing the ports requires recreating the cluster.

## Troubleshooting

**Check pod status:**
```bash
kubectl get pods -A
```

**Check ArgoCD application sync:**
```bash
kubectl get applications -n argocd
```
