# Consuming the ObjectStore platform API

How an application pack (e.g. `nebari-mlflow`) asks the platform for object
storage. `XObjectStore` is a **platform capability** — its XRD + Composition
are provided by the platform, GitOps-managed alongside the Crossplane providers.
A pack doesn't define it; a pack just *consumes* it.

To get a bucket plus least-privilege access, a pack:

1. Creates an `XObjectStore` in its own namespace naming a ServiceAccount
   (`claim.yaml`).
2. Runs its workload as that ServiceAccount — EKS Pod Identity then gives the
   pods scoped read/write to the provisioned bucket, with no static AWS keys.

The Composition provisions, from that one claim: an S3 bucket, a workload IAM
role (under the platform's workload path + permissions boundary, trust bound to
this namespace + ServiceAccount), an inline policy scoped to the bucket, and the
Pod Identity association.

## Prerequisites (platform side)

- An NIC-deployed EKS cluster with `crossplane_capabilities: [s3]`, so the
  provisioner IAM exists.
- The `XObjectStore` XRD + Composition + composition functions installed on the
  cluster (the platform layer under `manifests/crossplane/compositions/…`).

## Apply (consumer side)

```sh
kubectl apply -f claim.yaml                            # SA + XObjectStore
kubectl get xobjectstore smokestore -n pack-test -w    # wait for READY=True
kubectl apply -f probe-pod.yaml                        # workload using the SA
kubectl logs -f s3-probe -n pack-test                  # expect "SMOKE-OK"
```

`probe-pod.yaml` stands in for the pack's own workload: it assumes the workload
role via Pod Identity, writes+reads an object, and confirms a *different* bucket
is denied — proving the access is scoped to just this claim's bucket.
