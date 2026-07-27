# Helm values for foundational apps

Each foundational Helm app reads its values from this directory via ArgoCD
`valueFiles` (multi-source `$values` ref):

- `<app>/base.yaml` - owned by nebari-infrastructure-core. Rewritten on every
  `nic deploy --regen-apps`. Do not edit; your changes will be overwritten.
- `<app>/overlays/*.yaml` - owned by you (or a software pack). NIC never
  writes or deletes files here. Create the directory if it does not exist.

Two staging nuances, both easy to trip over:

- NIC never authors or deletes overlay *content*, but its regeneration commit
  stages the whole working tree (`AddWithOptions{All: true}`), so an overlay
  you have left uncommitted will be swept into NIC's next commit under NIC's
  own commit message. Commit your overlays yourself if you want them attributed
  and described properly.
- ArgoCD reads committed content only. An overlay that exists in the working
  tree but is not committed has no effect at all, including for local `file://`
  gitops repos where it is tempting to assume the file is read from disk.

How overlays merge:

- Files apply in lexical filename order and the last file wins, after
  `base.yaml`. Prefix files to make ordering explicit (e.g. `30-llm.yaml`).
- Helm merges maps but REPLACES lists. Map-shaped overrides merge cleanly;
  you cannot append to a list-valued field from an overlay.
- Missing overlay directories are fine (`ignoreMissingValueFiles: true`).
- Overlays require ArgoCD 3.4 or later, which is where glob expansion of
  `helm.valueFiles` was added. On older ArgoCD the `overlays/*.yaml` entry is
  silently discarded: the app still reports Synced/Healthy and nothing is
  logged at the default level. Check the repo-server image, not
  `argocd version`, since expansion happens only in the repo-server.
- After upgrading ArgoCD in place, force a hard refresh. The manifest cache key
  has no ArgoCD version in it, so committed overlays can stay inert until the
  repo cache expires (24h by default).
- Namespace your keys (`<kind>/<packname>`, e.g. `otlp/langfuse`) when a pack
  contributes to a shared map, so packs do not collide.

Non-Helm foundational apps (raw manifests under `manifests/`) are not
covered by this mechanism.
