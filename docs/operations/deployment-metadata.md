# Deployment metadata: which NIC produced this cluster?

`nic deploy` records the identity of the NIC binary that ran it into the cluster
itself, so the question can be answered with `kubectl` alone — no SSH to a
bastion, no access to the OpenTofu state, no guessing whether someone rebuilt
the binary since the last deploy.

## Reading it

```console
$ kubectl get configmap nic-deployment-info -n nebari-system -o yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nic-deployment-info
  namespace: nebari-system
  labels:
    app.kubernetes.io/managed-by: nebari-infrastructure-core
data:
  nic-version: v0.13.0
  nic-commit: e6d4ae9
  nic-build-date: "2026-06-04T13:30:00Z"
  cluster-provider: aws
  project-name: my-nebari-aws
  last-deploy-timestamp: "2026-06-10T18:43:22Z"
```

The version and commit alone, for pasting into an issue:

```bash
kubectl get cm nic-deployment-info -n nebari-system \
  -o jsonpath='{.data.nic-version}@{.data.nic-commit}'
```

## Fields

| Key | Meaning |
| --- | --- |
| `nic-version` | The `nic version` string of the binary that deployed. `dev` for a build made without version injection |
| `nic-commit` | Short commit of that build. `none` when not injected |
| `nic-build-date` | When that binary was built. `unknown` when not injected |
| `cluster-provider` | The cluster provider that provisioned the cluster, e.g. `aws` |
| `project-name` | The deployment's `project_name`, which ties the cluster back to its config and provider state |
| `last-deploy-timestamp` | RFC 3339 UTC time of the deploy that wrote this record |

Keys are only ever added, never renamed: runbooks and support scripts outside
this repository read them.

## Semantics

- **Last write wins.** The ConfigMap records the build that *most recently*
  deployed the cluster, not a history. Every `nic deploy` upserts the same
  object, so there is never more than one.
- **Written before the software stack.** The record is stamped as soon as the
  cluster provider finishes, ahead of Argo CD and the foundational apps. A
  deploy that dies during bootstrap still leaves behind which build died.
- **Never fatal.** If the write fails (RBAC, an unreachable API server), the
  deploy warns and continues — the cluster is fine, only its provenance record
  is missing. The warning names the ConfigMap so the gap is visible rather than
  silent.
- **Skipped in dry-run**, which has no cluster to write to.
- **Lives in `nebari-system`**, the namespace NIC owns and declares in the
  foundational Argo CD AppProject, rather than `kube-system`, which is
  deliberately outside that scope. Because the record is written before the
  foundational install, NIC creates the namespace if it is not there yet - the
  same namespace that install would have created.
- `dev` / `none` / `unknown` values mean the binary was built without
  `-ldflags` version injection (a plain `go build`). `make build` and the release
  pipeline both inject; see [packaging](packaging.md).

## Limitations

Cloud-resource tags do **not** carry the NIC version yet, so this record is
reachable only with cluster access. AWS and Azure resources carry the existing
`managed-by` markers but no version. That is tracked as a follow-up.
