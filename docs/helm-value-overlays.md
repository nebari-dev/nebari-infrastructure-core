# Overriding foundational Helm values

The foundational Helm apps, that is, those with a `values/<app>/` directory in your gitops repo (currently: envoy-gateway, keycloak, opentelemetry-collector, cert-manager, cloudnative-pg, trust-manager, nebari-landingpage), read their Helm values from the GitOps repo:

```
values/<app>/base.yaml          # NIC-owned; --regen-apps rewrites it
values/<app>/overlays/*.yaml    # yours; NIC never touches it
```

To override a value, commit a file under the app's `overlays/` directory (create the directory if it does not exist yet), for example `values/envoy-gateway/overlays/30-llm.yaml` with a map-shaped override. The file contains bare chart values, not a Kubernetes resource:

```yaml
# values/envoy-gateway/overlays/30-llm.yaml
deployment:
  envoyGateway:
    resources:
      limits:
        memory: 1Gi
```

ArgoCD picks up new files at sync time; you do not need to edit the Application manifest.

To find out what keys are available and what NIC currently sets, read `values/<app>/base.yaml` in your gitops repo. Any key you don't set there, or in an overlay, falls through to the chart's own defaults.

Note that git does not track empty directories, so `overlays/` only exists in the repo once it contains at least one file. There is no need for a placeholder file such as `.gitkeep`; just commit your first overlay and the directory comes along with it.

## Contract

1. **Ordering.** Overlay files apply after `base.yaml`, in lexical filename order, and the last file wins on any key collision. Prefix your files (`30-llm.yaml`) so ordering relative to other packs is explicit and visible from the filename alone.
2. **Merge semantics.** Helm merges maps but REPLACES lists. You cannot append to a list-valued field from an overlay. If the value you need to change is a list, this mechanism cannot help you; see [issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409) for how the OTel Collector works around the same limitation with named pipelines instead of list entries.
3. **Collision avoidance.** When your overlay contributes entries to a map that other packs might also write into, namespace your keys as `<kind>/<packname>`, for example `otlp/langfuse`, so two packs' entries do not overwrite each other.
4. **Never edit `base.yaml` or `apps/*.yaml`.** Both are rewritten by `--regen-apps`. Anything you write there is lost on the next regeneration.
5. **Requires ArgoCD 3.4 or later.** Glob expansion of `helm.valueFiles` was added in ArgoCD 3.4. On 3.3.x and earlier the `overlays/*.yaml` entry is silently discarded: your overlay has no effect, the Application still reports Synced/Healthy, and there is no diagnostic at the repo-server's default `info` log level. NIC installs a chart that ships 3.4 or later, so this only bites a cluster whose ArgoCD predates that.

   Check the repo-server, never `argocd version` or the UI, which report the API server. Glob expansion happens only in the repo-server, so a chart upgrade that did not roll that Deployment reads as 3.4.x while running 3.3.0.

   The best check reads what the binary does rather than what a tag says:

   ```bash
   kubectl -n argocd logs deploy/argocd-repo-server -c repo-server | grep -c "resolved value files"
   ```

   That log statement is `log.Infof` at `reposerver/repository/repository.go:1488` in v3.4.4 and does not exist in v3.3.0 at all, so a **non-zero count proves 3.4+**. A count of zero is inconclusive, not proof of pre-3.4: the line is emitted only when the repo-server actually renders a Helm app with `valueFiles`, and on a manifest-cache hit nothing is rendered at all. As the cache section below explains, serving from cache is the common state, not a corner case. A zero also follows a pod restart or reschedule (`kubectl logs` sees only the current container) or, with multiple repo-server replicas, reading a pod that did not serve the render. To disambiguate a zero, force a render with a hard refresh or a trivial gitops commit and re-run the count; if it is still zero after a forced render, fall back to the image-tag check below as the tiebreaker.

   A non-zero count is also the diagnostic for the next question, because the line lists the files that were actually passed to Helm:

   ```bash
   kubectl -n argocd logs deploy/argocd-repo-server -c repo-server \
     | grep "resolved value files" | grep <app> | tail -1
   ```

   The line should list two paths, `base.yaml` then your overlay; that means expansion worked. Only `base.yaml` on a 3.4+ repo-server means the glob matched nothing, which an image tag cannot tell you. The image tag is the tiebreaker for the zero-count case above, and also reveals pods from two different ReplicaSets serving at once:

   ```bash
   kubectl -n argocd get pods -l app.kubernetes.io/name=argocd-repo-server \
     -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].image}{"\n"}{end}'
   ```

### If you upgrade ArgoCD without running `--regen-apps`

Already-committed overlays can stay inert for up to 24 hours. ArgoCD's manifest cache key does not include the ArgoCD version, so upgrading alone does not invalidate cached manifests, and `--repo-cache-expiration` defaults to 24h. The symptom is identical to the version-floor problem above and reads as "the seam is broken" rather than "the cache is stale".

Force a hard refresh on the affected Applications after an upgrade, or overlays will not take effect until the repo cache expires. Following the documented `--regen-apps` path avoids this entirely, because the regeneration commit changes the revision and therefore the cache key.

## Verifying the seam end to end

Use this when you suspect overlays are not being applied, or when validating the seam on a new provider. Every step has a pass condition, so a failure localises rather than leaving you guessing. Executed against Hetzner/k3s with a remote authenticated GitOps repo and a `repository.existing.path` prefix, on both an OCI chart source and an HTTPS Helm repo source.

**1. Confirm the repo-server is 3.4+.** Use the two checks in contract item 5 above. Do this first: everything below reports a false negative on a pre-3.4 repo-server.

**2. Confirm the layout and the rendered Application.**

```bash
kubectl -n argocd get app <app> -o jsonpath='{.spec.sources[0].helm.valueFiles}{"\n"}'
kubectl -n argocd get app <app> -o jsonpath='{.spec.sources[?(@.ref=="values")]}{"\n"}'
```

Expect both `valueFiles` entries carrying your `repository.existing.path` prefix, and a source with `ref: values` pointing at your GitOps repo. The chart source is always `sources[0]` (the tests pin that), but the `ref: values` source is selected by its `ref` field rather than by position, so the filter form keeps working if an app carries additional sources. Gated-off apps (trust-manager on most providers) should have no `values/<app>/` directory in the repo at all.

**3. Confirm `base.yaml` applies before testing overlays.** Assert specific values, not just that the app is Healthy. Because `ignoreMissingValueFiles: true` also covers the `base.yaml` entry, a typo in `repository.existing.path` silently falls back to the chart's own defaults instead of erroring. If you see chart defaults here, fix that before going further; step 4 would be meaningless.

**4. Drop an overlay, with no `nic deploy`.** This is the property the seam exists to provide.

```bash
mkdir -p <path>/values/envoy-gateway/overlays
printf 'deployment:\n  replicas: 2\n' > <path>/values/envoy-gateway/overlays/30-test.yaml
git add -A && git commit -m "test: overlay" && git push
```

The commit is mandatory. ArgoCD reads committed content only, so an uncommitted file has no effect, including for local `file://` repos where it is tempting to assume the file is read from disk.

Pass condition: the live Deployment picks up the override with no edit to the Application and no `nic deploy` run. Observed within about a minute on a plain commit; a hard refresh (`kubectl -n argocd patch app <app> --type merge -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}'`) makes it immediate.

**5. Confirm at the resolution layer** with the `resolved value files` command in contract item 5. Two paths means expansion worked.

**6. Confirm overlays survive regeneration.** Record the overlay's checksum, run `nic deploy -f <config>.yaml --regen-apps` twice, then re-check. Expect an identical checksum, `base.yaml` regenerated unchanged, the override still live, and gated apps still absent.

**7. Confirm removal reverts.** Delete the overlay, commit, push. The value should return to what `base.yaml` specifies.

**If step 5 shows only `base.yaml` on a confirmed 3.4+ repo-server**, look at what the repo-server actually has on disk, which is the one thing the checks above cannot see:

```bash
kubectl -n argocd exec deploy/argocd-repo-server -c repo-server -- \
  sh -c 'ls -la /tmp/_argocd-repo/*/<path>/values/<app>/overlays/ 2>&1'
```

An absent or empty `overlays/` alongside a present `base.yaml` is a stale checkout rather than a glob problem; delete the repo-server pod and hard refresh. If the overlay is present at the right size and the glob still resolves to one file, that is an upstream bug worth reporting, and the repo-server's debug log will give the skip reason. The knob is `reposerver.log.level` in `argocd-cmd-params-cm` (chart value `configs.params."reposerver.log.level"`), which the chart wires to `ARGOCD_REPO_SERVER_LOGLEVEL`; do not use `ARGOCD_LOG_LEVEL`, which the repo-server overwrites at startup rather than reads, so setting it silently does nothing:

```bash
kubectl -n argocd patch cm argocd-cmd-params-cm --type merge -p '{"data":{"reposerver.log.level":"debug"}}'
kubectl -n argocd rollout restart deploy/argocd-repo-server
```

The restart is required either way: the flag default is read at process start.

## Migration from hand-edited manifests

If you previously edited `apps/<app>.yaml` directly to change Helm values, diff your copy against the regenerated version and move your changes into `values/<app>/overlays/<NN>-<name>.yaml` instead.

Non-values edits, such as changes to sync policy or destination, are not covered by this mechanism. If you need one of those, open an issue rather than hand-editing the generated manifest.
