# Overriding foundational Helm values

The foundational Helm apps, that is, those with a `values/<app>/` directory in your gitops repo (currently: envoy-gateway, keycloak, opentelemetry-collector, cert-manager, cloudnative-pg, postgresql, metallb, trust-manager, nebari-landingpage), read their Helm values from the GitOps repo:

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
5. **Requires ArgoCD 3.4 or later.** Glob expansion of `helm.valueFiles` was added in ArgoCD 3.4. On 3.3.x and earlier the `overlays/*.yaml` entry is silently discarded: your overlay has no effect, the Application still reports Synced/Healthy, and there is no diagnostic at the repo-server's default `info` log level. NIC installs a chart that ships 3.4 or later, so this only bites a cluster whose ArgoCD predates that. Check the repo-server specifically, not `argocd version`, because glob expansion happens only there:

   ```bash
   kubectl -n argocd get pods -l app.kubernetes.io/name=argocd-repo-server \
     -o jsonpath='{range .items[*]}{.spec.containers[0].image}{"\n"}{end}'
   ```

### If you upgrade ArgoCD without running `--regen-apps`

Already-committed overlays can stay inert for up to 24 hours. ArgoCD's manifest cache key does not include the ArgoCD version, so upgrading alone does not invalidate cached manifests, and `--repo-cache-expiration` defaults to 24h. The symptom is identical to the version-floor problem above and reads as "the seam is broken" rather than "the cache is stale".

Force a hard refresh on the affected Applications after an upgrade, or overlays will not take effect until the repo cache expires. Following the documented `--regen-apps` path avoids this entirely, because the regeneration commit changes the revision and therefore the cache key.

## Migration from hand-edited manifests

If you previously edited `apps/<app>.yaml` directly to change Helm values, diff your copy against the regenerated version and move your changes into `values/<app>/overlays/<NN>-<name>.yaml` instead.

Non-values edits, such as changes to sync policy or destination, are not covered by this mechanism. If you need one of those, open an issue rather than hand-editing the generated manifest.
