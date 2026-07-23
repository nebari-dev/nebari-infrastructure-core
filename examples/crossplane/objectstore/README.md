# ObjectStore: platform capability + consumer example

`XObjectStore` is a **platform capability**: one namespaced claim yields an S3
bucket plus least-privilege, keyless access (EKS Pod Identity) for a named
ServiceAccount. Application packs (e.g. `nebari-mlflow`) *consume* it — they
don't define it.

- `platform/` — the capability itself: XRD + Composition + composition
  functions. In a finished system this lives NIC-side, GitOps-managed next to
  the Crossplane providers. **It is not there yet** (see the call-out below).
- `claim.yaml` + `probe-pod.yaml` — the **consumer** side: how a pack requests
  a bucket and how its workload uses it.

## ⚠️ platform/ is PoC-only (hardcoded)

`platform/composition.yaml` hardcodes account `390259467264`, region
`us-east-1`, cluster `nebari-xp-poc`, and prefix `nebari-xp-poc-apps`, so it
works only against the phase-0 PoC cluster. Making it a real, portable
NIC-managed contract needs parameterization (EnvironmentConfig) plus a
verbatim-copy writer change — its `{{ }}` go-templating collides with NIC's own
`text/template` pass. Tracked as the "parameterize + wire into NIC GitOps"
follow-up; hand-applied until then.

## Prerequisites

An NIC-deployed EKS cluster with `crossplane_capabilities: [s3]`, so the
provisioner IAM (provider roles, boundaries, Pod Identity associations) exists,
plus Crossplane core and the `provider-aws-{s3,iam,eks}` providers and their
`aws-{s3,iam,eks}` ClusterProviderConfigs (`source: PodIdentity`).

## Apply

```sh
# Platform capability (once per cluster)
kubectl apply -f platform/functions.yaml
kubectl apply -f platform/definition.yaml    # XRD; wait for Established
kubectl apply -f platform/composition.yaml

# Consumer: request a bucket + prove read/write
kubectl apply -f claim.yaml                            # SA + XObjectStore
kubectl get xobjectstore smokestore -n pack-test -w    # wait READY=True
kubectl apply -f probe-pod.yaml
kubectl logs -f s3-probe -n pack-test                  # expect "SMOKE-OK"
```

`probe-pod.yaml` stands in for a pack's workload: it assumes the workload role
via Pod Identity, writes+reads an object, and confirms a *different* bucket is
denied — proving access is scoped to just this claim's bucket.
