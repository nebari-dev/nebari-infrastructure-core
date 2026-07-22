# ADR-0010: Helm valueFiles Overlay Seam for Foundational Apps

## Status

Accepted

## Date

2026-07-22

## Context

Software packs need a way to override Helm values on NIC's foundational ArgoCD applications. Two concrete drivers motivated this work:

- The LLM serving pack needs to reconfigure Envoy Gateway, tracked in [issue #406](https://github.com/nebari-dev/nebari-infrastructure-core/issues/406).
- Observability packs need to route the OpenTelemetry Collector's pipelines, tracked in [issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409).

Before this change, each Helm-based Application carried its values inline as a `spec.source.helm.values` block-scalar string. `nic deploy --regen-apps` regenerates every Application manifest from scratch on each run, so any hand edit a user or pack made to that inline block was silently wiped on the next regen. There was no seam for a pack to contribute values that would survive regeneration.

[Issue #406](https://github.com/nebari-dev/nebari-infrastructure-core/issues/406) considered using Kustomize patches to layer overrides onto the generated Application manifests, the same mechanism NIC already uses for raw-manifest apps. That does not work for Helm apps: the inline values block is a single block-scalar string, so a Kustomize patch can only replace the string wholesale. It cannot deep-merge a subset of keys into it, which is exactly what a values override needs to do.

Keeping any part of the values inline alongside an external file was also considered and rejected. ArgoCD's Helm value precedence is `parameters` > `valuesObject` > `values` > `valueFiles`. An inline `values` (or `valuesObject`) entry would silently outrank every `valueFiles` entry, so any base kept inline would always win over a pack's overlay file, regardless of file ordering.

## Decision Drivers

- Pack and user overrides must survive `nic deploy --regen-apps` without NIC needing to know about each pack.
- The seam must fail safe for clean installs where no pack has contributed an overlay yet.
- The mechanism has to work for both chart-based Applications and the two Applications whose non-values source is git-hosted (keycloak, nebari-landingpage).
- The contract needs to be simple enough to state as a filename convention, not a new DSL.
- The seam should be self-enforcing in tests so a newly added Helm app cannot regress it by accident.

## Considered Options

1. **Kustomize patches over the generated Application manifest.**
2. **Inline `values`/`valuesObject` plus an external override file.**
3. **ArgoCD multi-source `$values` refs to files in the gitops repo (`valueFiles`).**

## Decision Outcome

Chosen option: **Option 3, ArgoCD multi-source `$values` refs**.

All 9 Helm-based foundational apps now source their Helm values from files in the gitops repo instead of an inline block:

- envoy-gateway
- cert-manager
- cloudnative-pg
- postgresql
- metallb
- trust-manager
- opentelemetry-collector
- keycloak
- nebari-landingpage

Each app's values live at two paths in the gitops repo:

- `values/<app>/base.yaml` is NIC-owned. It is rewritten by every `nic deploy --regen-apps` and must not be hand-edited.
- `values/<app>/overlays/*.yaml` is user- or pack-owned. NIC never writes to or deletes from this directory. ArgoCD glob-expands the files at sync time and applies them in lexical filename order, with the last file winning on any key collision. `ignoreMissingValueFiles: true` is set on the chart source so a clean install with no overlay files yet, or a repo where the overlays directory does not exist, still syncs cleanly.

Each Application keeps its chart source first in `spec.sources`, and that chart source carries `helm.valueFiles` pointing at `$values/.../base.yaml` and `$values/.../overlays/*.yaml`. A second source supplies the `$values` ref:

- Most apps add the gitops repo itself as this second source, at the repo root.
- keycloak already has a second git source for its realm-setup manifests (`manifests/keycloak`, a PostSync hook); that existing source is reused as the `ref: values` source rather than adding a third source.
- nebari-landingpage's chart is itself hosted in a git repo (`nebari-dev/nebari-landing`), not a Helm chart repository, so it gains the gitops repo as an additional second source purely to serve as the `ref: values` carrier.

Values files live under `values/`, a new top-level directory in the gitops repo, rather than under `apps/`. The root app-of-apps Application (`templates/apps/root.yaml`) points its directory source at `apps/` with `recurse: false` and `include: '*.yaml'`, so it applies every top-level `*.yaml` file directly under `apps/` as a Kubernetes resource, and does not descend into subdirectories at all. A values file placed flat in `apps/` would be applied as a bogus resource; one nested in a subdirectory of `apps/` would be silently ignored instead. Either way, `apps/` is the wrong home for values files, hence the separate top-level `values/` directory.

The two conditional apps, metallb and trust-manager, gate their `values/<app>/base.yaml` file through the same writer predicates (`isMetalLBPath`, `isTrustBundlePath` in `pkg/argocd/writer.go`) that already gate their Application manifest. Critically, these predicates match the `base.yaml` file specifically, never the `values/<app>` directory. Matching the directory would route it through `removeStaleTemplate`'s directory branch, which calls `os.RemoveAll` and would destroy any user overlays sitting alongside `base.yaml` the moment the gate is disabled.

Raw-manifest foundational apps are unaffected by this decision. Kustomize remains the correct merge tool for apps whose ArgoCD source is manifest-shaped rather than Helm-values-shaped.

### Consequences

**Good:**

- Packs and users override values by committing a file under `values/<app>/overlays/`. No Application edit is required.
- Regeneration cannot destroy pack or user changes, because `--regen-apps` only ever rewrites `base.yaml` and the Application manifest, never the overlays directory.
- Overlay ordering is an explicit, visible contract: filenames are prefixed (e.g. `30-llm.yaml`) so pack authors can reason about precedence without reading ArgoCD internals.
- The seam is test-enforced for apps added in the future. `TestHelmApps_SeamInvariants` (`pkg/argocd/writer_test.go`) fails the build if a new Helm app template uses inline `values`/`valuesObject`, omits `valueFiles`, or is not enrolled in the test's app table.

**Bad:**

- Helm merges maps but replaces lists outright, so a list-valued field cannot be appended to from an overlay; a pack that needs to add an item to a list has no way to do so through this seam. The OTel Collector pipeline-routing design ([issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409)) works within this constraint by giving each pack its own named pipeline, a distinct map key, rather than appending to a shared list.
- `ignoreMissingValueFiles: true` applies to the `base.yaml` entry, not only the overlays glob. If the `GitBranch`/`GitPath` combination ever makes `base.yaml` unresolvable, for example a misconfigured path, the sync does not fail; it silently falls back to the chart's own defaults. The risk is low in practice because NIC writes the Application manifest and `base.yaml` together from the same template data, so the two can only disagree if something external mutates the gitops repo's layout, but it is a silent-degradation mode worth knowing about.
- The mechanism requires ArgoCD 2.6 or later, which introduced multi-source Applications. NIC installs ArgoCD v3.3.0, well past that floor.
- Deployments that already exist adopt the new layout the next time `--regen-apps` runs. Any hand edits previously made to `apps/*.yaml` inline values are not migrated automatically; they must be moved into an overlay file by hand.

## Options Detail

### Option 1: Kustomize patches over the generated Application manifest

Apply a Kustomize `patchesStrategicMerge` or JSON patch to the rendered Application, targeting `spec.source.helm.values`.

**Pros:**

- Reuses the same Kustomize layering NIC already applies to raw-manifest apps.

**Cons:**

- `spec.source.helm.values` is a single block-scalar string. A patch can only replace the whole string, never merge a subset of keys into it.
- Provides no way to express "add this key, leave everything else alone" for Helm values, which is the actual use case.

### Option 2: Inline values plus an external override file

Keep a NIC-owned inline `values` (or `valuesObject`) block on the chart source, and add a second `valueFiles` source for overrides.

**Pros:**

- Requires no new gitops repo directory structure.

**Cons:**

- ArgoCD's Helm precedence order is `parameters` > `valuesObject` > `values` > `valueFiles`. Any inline block would outrank every file in `valueFiles`, so the override file would never actually take effect against a key already set inline.

### Option 3: ArgoCD multi-source `$values` refs (chosen)

Move all Helm values out of the Application manifest entirely and into files referenced via `valueFiles`, split into a NIC-owned `base.yaml` and a user/pack-owned `overlays/*.yaml` glob.

**Pros:**

- `valueFiles` entries deep-merge in file order; no precedence conflict with an inline block, because there is no inline block.
- The base/overlays split maps directly onto ownership: NIC owns and rewrites one file, packs and users own everything else.
- Works uniformly across chart-source and git-hosted-chart apps by treating the `$values` ref as an independent source.

**Cons:**

- Introduces a new top-level `values/` directory and a filename-ordering convention that pack authors need to learn.
- Depends on multi-source Application support (ArgoCD 2.6+).

## Relationship to ADR-0008

[ADR-0008](0008-otel-collector-software-pack-override-point.md) established a runtime override ConfigMap for the OpenTelemetry Collector, deep-merged at collector startup via a second `--config` flag. This ADR does not replace or supersede that mechanism; the two are complementary layers:

- ADR-0008's ConfigMap is the runtime, single-owner extension point for the collector's own configuration format, merged by the collector binary itself.
- This ADR's `valueFiles` seam is the git-layer, multi-owner extension point for Helm chart values, merged by Helm at template time.

[Issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409) builds on both: it uses this ADR's overlay seam to adjust the collector's Helm-templated Deployment and service configuration, and continues to rely on ADR-0008's override ConfigMap for the collector's own pipeline configuration.

## Links

- [Issue #406](https://github.com/nebari-dev/nebari-infrastructure-core/issues/406) - LLM serving pack Envoy Gateway override, the driver for this decision.
- [Issue #409](https://github.com/nebari-dev/nebari-infrastructure-core/issues/409) - OTel Collector pipeline routing, which consumes both this ADR and ADR-0008.
- [ADR-0008](0008-otel-collector-software-pack-override-point.md) - OpenTelemetry Collector software pack override point, the complementary runtime extension mechanism.
- `pkg/argocd/templates/values/README.md` - in-repo contract doc generated into the gitops repo.
- `docs/helm-value-overlays.md` - pack-author-facing guide to this seam.
- `pkg/argocd/writer_test.go`, `TestHelmApps_SeamInvariants` - test enforcement for future Helm apps.
