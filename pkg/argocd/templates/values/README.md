# Helm values for foundational apps

Each foundational Helm app reads its values from this directory via ArgoCD
`valueFiles` (multi-source `$values` ref):

- `<app>/base.yaml` — owned by nebari-infrastructure-core. Rewritten on every
  `nic deploy --regen-apps`. Do not edit; your changes will be overwritten.
- `<app>/overlays/*.yaml` — owned by you (or a software pack). NIC never
  writes or deletes files here. Create the directory if it does not exist.

How overlays merge:

- Files apply in lexical filename order and the last file wins, after
  `base.yaml`. Prefix files to make ordering explicit (e.g. `30-llm.yaml`).
- Helm merges maps but REPLACES lists. Map-shaped overrides merge cleanly;
  you cannot append to a list-valued field from an overlay.
- Missing overlay directories are fine (`ignoreMissingValueFiles: true`).
- Namespace your keys (`<kind>/<packname>`, e.g. `otlp/langfuse`) when a pack
  contributes to a shared map, so packs do not collide.

Non-Helm foundational apps (raw manifests under `manifests/`) are not
covered by this mechanism.
