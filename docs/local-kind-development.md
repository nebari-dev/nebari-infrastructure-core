# Local Development with Kind

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- Go 1.26+

## Quick Start

```bash
make localkind-up
```

This single command:
1. Builds the `nic` binary
2. Creates a `kind` Docker network (`192.168.1.0/24`) for MetalLB
3. Creates a Kind cluster named `nebari-local` with appropriate volume mounts
4. Deploys Nebari (ArgoCD, cert-manager, envoy-gateway, MetalLB, Keycloak, etc.)

## GitOps Repository Modes

The Makefile reads `examples/local-config.yaml` and automatically handles three scenarios:

| Config | What happens |
|--------|-------------|
| No `git_repository` section | Auto-creates `~/.nic/gitops/{project_name}` and mounts it into the cluster |
| `url: "file:///path/to/repo"` | Uses the matching `cluster.local.kind.extra_mounts` entry supplied by the user |
| `url: "git@github.com:..."` | No mount — ArgoCD pulls from the remote repo directly |

For local `file://` repos, the path is mounted into both the Kind node and the ArgoCD repo-server pod. ArgoCD reads commits and refs from `.git` and creates its own checkout; it does not consume the source working-tree files directly.

When initializing or committing to any local `file://` repo, NIC makes the repository root and Git-serving data under `.git` group/other-readable and traversable so the non-root ArgoCD repo-server can read committed content. This applies whether the repo is auto-generated or user-supplied. NIC preserves existing and special permission bits, and does not touch working-tree files, hooks, reflogs, the Git index, or unrelated `extra_mounts`.

If an existing Kind cluster was created with a different local GitOps path, recreate it with `make localkind-down` followed by `make localkind-up`; Kind mounts are fixed at cluster creation time.

> **Note:** `file://` repos only work when the cluster nodes can access the local path (Kind, k3s, bare metal). For cloud providers (AWS, GCP, Azure), use a remote git repository since Kubernetes nodes don't have access to your local filesystem.

## Using a Custom Config

```bash
make localkind-up LOCAL_CONFIG=./my-config.yaml
```

## Teardown and Rebuild

```bash
make localkind-down     # Delete cluster and Docker network
make localkind-rebuild  # Full teardown + rebuild in one step
```

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

The Makefile creates a Docker network with subnet `192.168.1.0/24`. MetalLB allocates LoadBalancer IPs from `192.168.1.100-192.168.1.110` within this range, making services reachable from your host machine.

## Troubleshooting

**Check pod status:**
```bash
kubectl get pods -A
```

**Check ArgoCD application sync:**
```bash
kubectl get applications -n argocd
```
