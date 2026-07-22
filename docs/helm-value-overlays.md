# Overriding foundational Helm values

Foundational Helm apps (envoy-gateway, keycloak, opentelemetry-collector, cert-manager, cloudnative-pg, postgresql, metallb, trust-manager, nebari-landingpage) read their Helm values from the GitOps repo:

```
values/<app>/base.yaml          # NIC-owned; --regen-apps rewrites it
values/<app>/overlays/*.yaml    # yours; NIC never touches it
```

To override a value, commit a file under the app's `overlays/` directory (create the directory if it does not exist yet), for example `values/envoy-gateway/overlays/30-llm.yaml` with a map-shaped override. ArgoCD picks up new files at sync time; you do not need to edit the Application manifest.

Note that git does not track empty directories, so `overlays/` only exists in the repo once it contains at least one file. There is no need for a placeholder file such as `.gitkeep`; just commit your first overlay and the directory comes along with it.

## Contract

1. **Ordering.** Overlay files apply after `base.yaml`, in lexical filename order, and the last file wins on any key collision. Prefix your files (`30-llm.yaml`) so ordering relative to other packs is explicit and visible from the filename alone.
2. **Merge semantics.** Helm merges maps but REPLACES lists. You cannot append to a list-valued field from an overlay. If the value you need to change is a list, this mechanism cannot help you; see [issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409) for how the OTel Collector works around the same limitation with named pipelines instead of list entries.
3. **Collision avoidance.** When your overlay contributes entries to a map that other packs might also write into, namespace your keys as `<kind>/<packname>`, for example `otlp/langfuse`, so two packs' entries do not overwrite each other.
4. **Never edit `base.yaml` or `apps/*.yaml`.** Both are rewritten by `--regen-apps`. Anything you write there is lost on the next regeneration.

## Migration from hand-edited manifests

If you previously edited `apps/<app>.yaml` directly to change Helm values, diff your copy against the regenerated version and move your changes into `values/<app>/overlays/<NN>-<name>.yaml` instead.

Non-values edits, such as changes to sync policy or destination, are not covered by this mechanism. If you need one of those, open an issue rather than hand-editing the generated manifest.
