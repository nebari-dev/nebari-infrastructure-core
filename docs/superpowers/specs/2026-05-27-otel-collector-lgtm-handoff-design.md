# OTel collector ConfigMap handoff between NIC and LGTM pack

**Issue:** https://github.com/nebari-dev/nebari-lgtm-pack/issues/8
**Repos touched:** `nebari-dev/nebari-infrastructure-core`, `nebari-dev/nebari-lgtm-pack`
**Status:** Design — ready for review

## Problem

NIC deploys a default OpenTelemetry Collector via an ArgoCD `Application` that points at the upstream `opentelemetry-collector` Helm chart (`pkg/argocd/templates/apps/opentelemetry-collector.yaml`). The chart renders a ConfigMap whose `data.relay` field is the entire collector config. The default config exports to a `debug` exporter — useful for a fresh cluster, useless for actual observability.

When a user installs the `nebari-lgtm-pack` Helm chart (which ships Loki, Tempo, Mimir, and Grafana), the collector must be rewired so logs/traces/metrics flow to those backends. Today this requires manual editing of the OTel Application's `helm.values` block in the GitOps repo, and that edit gets clobbered every time `nic deploy` runs.

We want: **installing the LGTM pack automatically rewires the collector, and subsequent `nic deploy` runs do not undo the wiring.**

## Goals

- A user who installs the LGTM Helm chart (or adds the corresponding ArgoCD `Application`) gets a working observability pipeline with no extra steps — no file edits, no kubectl, no `nic` re-runs.
- `nic deploy` is idempotent: repeated runs do not revert the LGTM wiring.
- NIC has no per-pack code paths. The mechanism is general enough that a future software pack could hook the same field without further NIC changes.
- The default (no LGTM installed) experience is unchanged: NIC ships a collector with debug exporters.

## Non-goals

- A formal "software pack claim" framework with annotations, server-side checks, or per-pack opt-in flags. The mechanism in this design is a generic ArgoCD `ignoreDifferences` rule on one field; we don't promise a stable claim API yet.
- Reverting `data.relay` to NIC defaults on LGTM uninstall. Out of scope; documented as a manual step.
- Configurable collector workload kind. We assume `daemonset` mode, matching NIC's current Application values.
- Coordinating two software packs that both want to overwrite the same field. No other pack does this today; documented as a future concern.

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│  nic deploy (initial or repeat)                                       │
│                                                                       │
│  pkg/argocd/templates/apps/opentelemetry-collector.yaml               │
│  └─ ArgoCD Application (release name, chart, base values: unchanged) │
│     ├─ NEW: spec.ignoreDifferences                                    │
│     │   └─ ConfigMap "opentelemetry-collector-opentelemetry-          │
│     │       collector-agent" data.relay (jsonPointer)                 │
│     └─ NEW: syncPolicy.syncOptions += RespectIgnoreDifferences=true   │
└──────────────────────────────────────────────────────────────────────┘
                              │ (written to GitOps repo, synced by ArgoCD)
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│  In cluster (no LGTM yet)                                             │
│   ArgoCD → Helm renders → ConfigMap (default debug exporter)         │
│                        └→ DaemonSet (collector pods)                  │
└──────────────────────────────────────────────────────────────────────┘
                              │ (user installs LGTM pack)
                              ▼
┌──────────────────────────────────────────────────────────────────────┐
│  LGTM pack post-install / post-upgrade Helm hook                      │
│   templates/otel-collector-config-patch.yaml                          │
│   ├─ ServiceAccount + Role + RoleBinding (patch CM, restart DS)       │
│   ├─ ConfigMap (templated overrides.yaml, endpoints use .Release.Name)│
│   └─ Job:                                                             │
│      1. Wait for NIC's ConfigMap (up to 5m)                           │
│      2. yq deep-merge overrides into data.relay                       │
│      3. kubectl patch CM (data.relay + managed-by annotation)         │
│      4. kubectl rollout restart daemonset/<collector>                 │
│      5. kubectl rollout status daemonset/<collector>                  │
└──────────────────────────────────────────────────────────────────────┘
```

Key invariants:

- NIC remains GitOps-write-only — no live cluster reads, no per-pack knowledge.
- NIC's `ignoreDifferences` rule is generic ("don't reconcile the data.relay field of this specific ConfigMap"), not LGTM-aware.
- Repeated `nic deploy` is idempotent: the rule is permanent, and `RespectIgnoreDifferences=true` prevents ArgoCD's `Helm sync` from re-applying the Helm-rendered default into the ignored field.
- The DaemonSet's `checksum/config` annotation is derived from Helm values, not from ConfigMap content; the LGTM Job must therefore trigger the rollout explicitly.

## Detailed design

### NIC changes

Single file edit: `pkg/argocd/templates/apps/opentelemetry-collector.yaml`.

Add two blocks under `spec:`:

```yaml
spec:
  project: foundational

  ignoreDifferences:
    - group: ""
      kind: ConfigMap
      name: opentelemetry-collector-opentelemetry-collector-agent
      namespace: monitoring
      jsonPointers:
        - /data/relay

  source:
    # unchanged: chart, repoURL, targetRevision, helm.values

  destination:
    server: https://kubernetes.default.svc
    namespace: monitoring

  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
      - RespectIgnoreDifferences=true   # NEW
    retry:
      # unchanged
```

Rationale:

- `ignoreDifferences` scoped to `data.relay` (not the whole ConfigMap) means labels, annotations, and the ConfigMap's existence still reconcile normally. If the ConfigMap is deleted, ArgoCD recreates it from Helm.
- `RespectIgnoreDifferences=true` extends the rule from drift detection to drift remediation: without it, ArgoCD detects the difference but still applies the Helm-rendered default during sync. With it, ArgoCD leaves the field alone during sync.
- The ConfigMap name is pinned to the value the chart renders with NIC's release name (`opentelemetry-collector`) and the chart's daemonset-mode template. Chart version bumps that change the name will break the rule and revert LGTM's wiring; this is covered by an integration test.

No new template variables, no new NIC config flags, no new code paths. The change is fully static.

### LGTM pack changes

#### New file: `templates/otel-collector-config-patch.yaml`

Five hook-annotated objects, all `helm.sh/hook: post-install,post-upgrade`, `helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded`:

1. **ServiceAccount** `{{ release }}-otel-patcher` in `monitoring`.
2. **Role** — least-privilege, scoped by `resourceNames`:
   - `configmaps`: `get`, `patch` on `opentelemetry-collector-opentelemetry-collector-agent`
   - `daemonsets` (apps): `get`, `patch` on `opentelemetry-collector-opentelemetry-collector-agent`
3. **RoleBinding** binding the SA to the Role.
4. **ConfigMap** `{{ release }}-otel-overrides` holding `overrides.yaml`. Endpoints are templated using `{{ .Release.Name }}` so the chart survives custom release names:
   ```yaml
   data:
     overrides.yaml: |
       exporters:
         otlphttp/loki:
           endpoint: http://{{ .Release.Name }}-loki:3100/otlp
           tls:
             insecure: true
         otlp/tempo:
           endpoint: http://{{ .Release.Name }}-tempo:4317
           tls:
             insecure: true
         otlphttp/mimir:
           endpoint: http://{{ .Release.Name }}-mimir-gateway/otlp
           tls:
             insecure: true
       service:
         pipelines:
           logs:    { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [otlphttp/loki] }
           traces:  { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [otlp/tempo] }
           metrics: { receivers: [otlp, prometheus], processors: [memory_limiter, batch], exporters: [otlphttp/mimir] }
   ```
5. **Job** `{{ release }}-otel-patch`:
   - `serviceAccountName`: the SA above.
   - `backoffLimit: 5`, `ttlSecondsAfterFinished: 600`.
   - Image: `alpine/k8s:1.30.4` (bundles `kubectl`, `yq`, `jq`). Pinned in `values.yaml`.
   - Volume-mount the overrides ConfigMap at `/overrides`.
   - Script (Helm-templated; `NS`/`CM`/`DS` resolve from `.Values.otelCollectorOverrides.*` at render time):
     ```sh
     set -eu
     NS={{ .Values.otelCollectorOverrides.namespace | quote }}
     CM={{ .Values.otelCollectorOverrides.configMapName | quote }}
     DS={{ .Values.otelCollectorOverrides.daemonSetName | quote }}

     # Wait up to 5m for NIC's collector ConfigMap to exist
     for i in $(seq 1 60); do
       kubectl -n "$NS" get cm "$CM" >/dev/null 2>&1 && break
       sleep 5
     done

     CURRENT=$(kubectl -n "$NS" get cm "$CM" -o jsonpath='{.data.relay}')
     MERGED=$(printf '%s\n' "$CURRENT" | yq -P '. *= load("/overrides/overrides.yaml")' -)
     PATCH=$(jq -n --arg relay "$MERGED" \
       '{metadata:{annotations:{"nic.nebari.dev/managed-by":"lgtm-pack"}},data:{relay:$relay}}')

     kubectl -n "$NS" patch cm "$CM" --type merge --patch "$PATCH"
     kubectl -n "$NS" rollout restart daemonset/"$DS"
     kubectl -n "$NS" rollout status daemonset/"$DS" --timeout=3m
     ```

#### `values.yaml` additions

```yaml
otelCollectorOverrides:
  enabled: true
  namespace: monitoring
  configMapName: opentelemetry-collector-opentelemetry-collector-agent
  daemonSetName: opentelemetry-collector-opentelemetry-collector-agent
  image: alpine/k8s:1.30.4
```

The override content is **not** in `values.yaml` — it lives in the templated ConfigMap, since endpoints need `{{ .Release.Name }}` substitution at render time. If we later want user-tunable additional overrides, we can expose `otelCollectorOverrides.extraConfig` and `extend` the file in the template. Not in this iteration.

#### `examples/opentelemetry-collector-overrides.yaml`

Reframed from "thing you copy into your GitOps repo manually" to "reference of what the chart now auto-applies." Header comment updated; content unchanged.

### Merge semantics (`yq '. *= load(...)'`)

- Maps deep-merge.
- Lists and scalars are replaced (not concatenated).

Consequences:

- New exporter keys (`otlphttp/loki`, etc.) are *added* under `exporters`.
- The three pipelines (`logs`, `traces`, `metrics`) under `service.pipelines` are *replaced* entirely — which is what we want, since the new pipelines list specific exporters.
- NIC's receivers, processors, and extensions are *preserved* untouched.

If NIC later drops a receiver or processor that LGTM's pipelines reference (e.g. `prometheus`), the merged config is broken and the collector pod fails to start. The Job's `rollout status` step times out and the Job exits non-zero — visible failure, not silent drift.

## Testing strategy

### NIC side

- **Unit test** (`pkg/argocd/writer_test.go`): assert the rendered `opentelemetry-collector.yaml` contains `ignoreDifferences` for `ConfigMap/opentelemetry-collector-opentelemetry-collector-agent` with `jsonPointers: [/data/relay]`, and that `syncOptions` includes `RespectIgnoreDifferences=true`. Pure YAML-parse assertion, no cluster.
- **Integration test** (`make test-integration-local`): after `nic deploy` and ArgoCD sync, manually `kubectl patch` the OTel collector ConfigMap's `data.relay` field. Force-trigger an ArgoCD sync. Assert the patched value persists (i.e., `RespectIgnoreDifferences` works).

### LGTM pack side

- **Helm render test**: `helm template . | yq` snapshot with both default release name and a custom one (`--release-name foo`); assert override endpoints contain `lgtm-pack-{loki,tempo,mimir}` and `foo-{loki,tempo,mimir}` respectively.
- **kind integration**: install NIC's OTel Application + LGTM chart, assert merged `data.relay` contains all three new exporter blocks, assert the collector DaemonSet rolls out and reports `Available`. Ride on the existing `Tiltfile` / `ctlptl-config.yaml`.

## Edge cases

| Case | Behavior |
|---|---|
| LGTM installed before NIC's collector reconciles | Job wait-loop (60×5s = 5min) blocks for ConfigMap. Timeout → Job fails → `backoffLimit: 5` retries → surfaced to user. |
| LGTM `helm upgrade` with new values | `post-upgrade` hook fires → Job re-patches → DaemonSet rolls. |
| LGTM `helm uninstall` | ConfigMap `data.relay` keeps LGTM endpoints (now pointing at nonexistent Services). Documented reset: `kubectl -n monitoring delete cm opentelemetry-collector-opentelemetry-collector-agent`; ArgoCD recreates from Helm defaults. |
| NIC chart version bump changes ConfigMap name | `ignoreDifferences` no longer matches → ArgoCD reverts LGTM overrides on next sync → caught by NIC integration test. |
| Two software packs claim the same field | Whichever ran post-install/post-upgrade last wins. Document; out of scope. |
| `yq` merge yields invalid OTel config | Collector pod CrashLoopBackOff → `rollout status` times out (3m) → Job non-zero → visible. |

## Documentation

- **`nebari-lgtm-pack/README.md`**: new "OTel collector wiring" section — explains auto-patch, the `nic.nebari.dev/managed-by=lgtm-pack` annotation, and the uninstall reset command.
- **NIC docs (`docs/`)**: short note in observability docs — NIC ships a default collector with debug exporter; software packs can claim the `data.relay` field via `ignoreDifferences`. No promise of a stable claim API yet.
- **`nebari-lgtm-pack/examples/opentelemetry-collector-overrides.yaml`**: keep file, reframe header comment as "reference / standalone tweaking example."

## Rejected alternatives

- **Approach B — LGTM patches NIC's parent ArgoCD Application's `helm.values`.** Forces NIC into live cluster reads during deploy. Two layers of `ignoreDifferences`. Rejected as over-complex.
- **Approach C — LGTM ships a separate collector instance.** Cleanest ownership boundary but invasive UX: requires Service aliasing or downstream config to route to the LGTM collector. Rejected.
- **NIC config flag (`software_packs.lgtm: true`)**: simpler mechanics but couples NIC to specific packs. Rejected in favor of the "fully automatic via Helm install" UX target.
- **Full-replace `data.relay` instead of merge**: smaller Job, no `yq`. Rejected because LGTM would have to ship a complete OTel config (including NIC's receivers/processors), tightly coupling LGTM versions to NIC's collector internals.

## Open questions

- Should the LGTM `helm uninstall` flow ship a `pre-delete` Job that resets `data.relay` to a known-good empty config (forcing ArgoCD to recreate from Helm defaults)? Currently designed as a documented manual step; could be revisited if user feedback indicates surprise.
