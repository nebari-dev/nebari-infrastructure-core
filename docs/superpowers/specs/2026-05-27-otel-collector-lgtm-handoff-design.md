# OTel collector ConfigMap handoff between NIC and LGTM pack

**Issue:** https://github.com/nebari-dev/nebari-lgtm-pack/issues/8
**Repos touched:** `nebari-dev/nebari-infrastructure-core`, `nebari-dev/nebari-lgtm-pack`
**Status:** Design (revised) — second architecture after the first failed in real-cluster testing

## Problem

NIC deploys a default OpenTelemetry Collector via an ArgoCD `Application` that points at the upstream `opentelemetry-collector` Helm chart (`pkg/argocd/templates/apps/opentelemetry-collector.yaml`). The chart renders a ConfigMap whose `data.relay` field is the entire collector config. The default config exports to a `debug` exporter — useful for a fresh cluster, useless for actual observability.

When a user installs the `nebari-lgtm-pack` Helm chart (which ships Loki, Tempo, Mimir, and Grafana), the collector must be rewired so logs/traces/metrics flow to those backends. Today this requires manual editing of the OTel Application's `helm.values` block in the GitOps repo, and that edit gets clobbered every time `nic deploy` runs.

We want: **installing the LGTM pack automatically rewires the collector, and subsequent `nic deploy` runs do not undo the wiring.**

## Goals

- A user who installs the LGTM Helm chart (or adds the corresponding ArgoCD `Application`) gets a working observability pipeline with no extra steps — no file edits, no kubectl, no `nic` re-runs.
- `nic deploy` is idempotent: repeated runs do not revert the LGTM wiring.
- NIC has no per-pack code paths. The mechanism is a generic extension point that future software packs can use without further NIC changes.
- The default (no LGTM installed) experience is unchanged: NIC ships a collector with debug exporters.

## Non-goals

- A formal "software pack claim" framework with annotations, server-side checks, or per-pack opt-in flags. The extension point is positional (one mount path, one ConfigMap name) — not a structured API.
- Reverting overrides on LGTM uninstall. The override ConfigMap is deleted by Helm; collector pods continue using the cached config until they restart, then fall back to defaults.
- Configurable collector workload kind. We assume `daemonset` mode, matching NIC's current Application values.
- Coordinating two software packs that both want to override. No other pack does this today; documented as a future concern.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  nic deploy (initial or repeat) — NIC adds extension-point values   │
│                                                                      │
│  pkg/argocd/templates/apps/opentelemetry-collector.yaml              │
│  └─ ArgoCD Application (release name, chart, base values: unchanged)│
│     └─ helm.values additions:                                        │
│        ├─ extraVolumes:                                              │
│        │  ├─ overrides-src ← configMap: opentelemetry-collector-    │
│        │  │                    overrides (optional: true)            │
│        │  └─ overrides-resolved ← emptyDir                           │
│        ├─ extraVolumeMounts: overrides-resolved → /conf/overrides    │
│        ├─ initContainers: ensure-overrides                           │
│        │     copies /src/relay.yaml → /dst/relay.yaml,               │
│        │     or writes `{}` if /src/relay.yaml missing               │
│        ├─ command.extraArgs: --config=/conf/overrides/relay.yaml     │
│        └─ clusterRole.create: true (preserves prometheus receiver    │
│           pod-discovery — separate concern from the LGTM handoff)    │
└─────────────────────────────────────────────────────────────────────┘
                              │ (written to GitOps repo, synced by ArgoCD)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  In cluster, NIC only (no LGTM yet)                                 │
│   ArgoCD → Helm renders → opentelemetry-collector-agent CM (debug)  │
│                        └→ opentelemetry-collector-overrides CM      │
│                              does NOT exist; volume mount is empty  │
│                        └→ DaemonSet:                                 │
│                             • init container writes `{}` to emptyDir│
│                             • collector merges base + `{}`           │
│                             • result: NIC defaults (debug exporter) │
└─────────────────────────────────────────────────────────────────────┘
                              │ (user installs LGTM pack)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  LGTM pack chart renders                                             │
│   templates/otel-collector-config-patch.yaml                         │
│   ├─ ConfigMap opentelemetry-collector-overrides                     │
│   │     data.relay.yaml = LGTM exporter + pipeline overrides         │
│   │     (endpoints templated with .Release.Name)                     │
│   └─ post-install/post-upgrade hook:                                 │
│      ServiceAccount + Role + RoleBinding (rollout DaemonSet)         │
│      Job: wait for DaemonSet, then `kubectl rollout restart`         │
└─────────────────────────────────────────────────────────────────────┘
                              │ (DaemonSet rolls)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  New collector pods:                                                 │
│   • Init container sees /src/relay.yaml (mounted from LGTM CM)      │
│   • Copies it to emptyDir at /conf/overrides/relay.yaml             │
│   • Collector starts with two --config files; deep-merges them      │
│   • Final pipelines route to Loki/Tempo/Mimir                       │
└─────────────────────────────────────────────────────────────────────┘
```

Key invariants:

- NIC and the LGTM pack write to **separate** Kubernetes resources. NIC's chart manages `opentelemetry-collector-agent` (base config); LGTM manages `opentelemetry-collector-overrides` (override config). There is no shared field for ArgoCD to clobber.
- The init container guarantees `/conf/overrides/relay.yaml` always exists, even when no software pack has provided one — the collector's `--config` arg never references a missing file.
- The override ConfigMap is regular Helm resource (not a hook). It's owned by the lgtm-pack release and tracked by ArgoCD as part of the lgtm-pack Application. ArgoCD only ever reconciles it to match its own desired state.
- The post-install hook Job exists purely to roll the DaemonSet — the DaemonSet's `checksum/config` annotation is derived from chart values, not from this external ConfigMap, so without an explicit rollout, override-CM changes wouldn't propagate to running pods.

## Detailed design

### NIC changes

Single file edit: `pkg/argocd/templates/apps/opentelemetry-collector.yaml`.

Additions to `helm.values` (alongside the existing `config:` block):

```yaml
extraVolumes:
  - name: overrides-src
    configMap:
      name: opentelemetry-collector-overrides
      optional: true
  - name: overrides-resolved
    emptyDir: {}
extraVolumeMounts:
  - name: overrides-resolved
    mountPath: /conf/overrides
    readOnly: true
initContainers:
  - name: ensure-overrides
    image: busybox:1.37
    command:
      - sh
      - -c
      - |
        if [ -f /src/relay.yaml ]; then
          cp /src/relay.yaml /dst/relay.yaml
        else
          echo '{}' > /dst/relay.yaml
        fi
    volumeMounts:
      - name: overrides-src
        mountPath: /src
        readOnly: true
      - name: overrides-resolved
        mountPath: /dst
command:
  extraArgs:
    - "--config=/conf/overrides/relay.yaml"
```

Removed (from the earlier failed design):

- The `spec.ignoreDifferences` block.
- `RespectIgnoreDifferences=true` from `syncPolicy.syncOptions`.

Independent fix kept in the same change (separate concern but caught in the same investigation):

- `clusterRole.create: true` with rules for pod/node/service/endpoint list+watch. Without this, the prometheus receiver's `kubernetes_sd_configs` (role: pod) fails with `pods is forbidden` and no metrics flow.

No new template variables, no new NIC config flags, no code changes — this is a static YAML edit.

### LGTM pack changes

#### `templates/otel-collector-config-patch.yaml`

Two regular resources (not hooks):

1. **ConfigMap** `opentelemetry-collector-overrides` in `monitoring`:
   ```yaml
   data:
     relay.yaml: |
       exporters:
         otlphttp/loki:
           endpoint: http://{{ .Release.Name }}-loki:3100/otlp
           tls: { insecure: true }
         otlp/tempo:
           endpoint: http://{{ .Release.Name }}-tempo:4317
           tls: { insecure: true }
         otlphttp/mimir:
           endpoint: http://{{ .Release.Name }}-mimir-gateway/otlp
           tls: { insecure: true }
       service:
         pipelines:
           logs:    { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [otlphttp/loki] }
           traces:  { receivers: [otlp],             processors: [memory_limiter, batch], exporters: [otlp/tempo] }
           metrics: { receivers: [otlp, prometheus], processors: [memory_limiter, batch], exporters: [otlphttp/mimir] }
   ```
   Name is hardcoded to match the volume reference NIC's chart values use. Key `relay.yaml` matches what NIC's init container expects.

2. **ServiceAccount + Role + RoleBinding** for the rollout Job. Role has three rules:
   - `daemonsets`: `get`, `patch` (resourceNames-scoped) — `patch` is what `kubectl rollout restart` does (sets `spec.template.metadata.annotations.kubectl.kubernetes.io/restartedAt`).
   - `daemonsets`: `list`, `watch` (namespace-wide) — `kubectl rollout status` uses an informer; Kubernetes RBAC doesn't honor `resourceNames` on these verbs.

3. **Job** `<release>-otel-rollout`, `post-install,post-upgrade` hook with `before-hook-creation,hook-succeeded` delete policy. Script:

   ```sh
   set -euo pipefail
   # Wait up to 5m for NIC's DaemonSet to exist (handles install-order races).
   for i in $(seq 1 60); do
     kubectl -n "$NS" get daemonset "$DS" >/dev/null 2>&1 && break
     sleep 5
   done
   kubectl -n "$NS" get daemonset "$DS" >/dev/null
   kubectl -n "$NS" rollout restart daemonset/"$DS"
   kubectl -n "$NS" rollout status daemonset/"$DS" --timeout=3m
   ```

#### `values.yaml`

```yaml
otelCollectorOverrides:
  enabled: true
  namespace: monitoring
  daemonSetName: opentelemetry-collector-agent
  image: alpine/k8s:1.30.4
  imagePullPolicy: IfNotPresent
```

The override config content lives in the template (needs `{{ .Release.Name }}` substitution at render time). Not exposed for user customization in this iteration.

#### `examples/opentelemetry-collector-overrides.yaml`

Kept as a standalone reference of what the chart auto-applies. Header reframed accordingly.

## Testing strategy

### NIC side

- **Unit test** `TestWriteApplication_OtelCollector_OverridesExtensionPoint`: assert the rendered Application contains `extraVolumes` with the `opentelemetry-collector-overrides` configMap (`optional: true`), the `ensure-overrides` init container, the `--config=/conf/overrides/relay.yaml` extra arg, AND that the old broken-design fragments (`ignoreDifferences:`, `RespectIgnoreDifferences=true`, `jsonPointers:`) are gone.

### LGTM pack side

- **Helm lint + template snapshot test** in CI: stub a DaemonSet (the rollout Job needs something to roll), install the chart, assert that:
  - The override ConfigMap is rendered.
  - Endpoints contain `lgtm-pack-{loki,tempo,mimir}` (or `foo-*` with custom release name).
  - Each pipeline's `exporters` is exactly `[<single LGTM exporter>]` (not `[debug, <LGTM exporter>]`).
  - The DaemonSet's generation was bumped (proves the rollout Job ran).

## Edge cases

| Case | Behavior |
|---|---|
| LGTM installed before NIC's DaemonSet reconciles | Rollout Job's wait-loop (60×5s = 5min) waits for the DaemonSet. Times out → Job fails → `backoffLimit: 5` retries → bubble up. |
| LGTM `helm upgrade` with new values | `post-upgrade` hook fires, Job re-rolls DaemonSet, init container picks up updated override CM. |
| LGTM uninstalled (`helm uninstall`) | Override CM deleted. Running collector pods keep cached config until next restart. On next restart, init container falls back to empty `{}`, collector reverts to NIC defaults (debug exporter). Documented in README. |
| NIC chart upgrade renames the DaemonSet | Rollout Job fails to find the DaemonSet → wait-loop times out → caught by NIC integration tests on the chart upgrade. |
| Two software packs both want to override | Both would render a ConfigMap with the same name → Helm release ownership conflict on the second install. Out of scope; document as future work. |
| User provides a malformed override | Collector pod fails to start → `rollout status` times out → Job exits non-zero → visible failure. |

## Documentation

- **`nebari-lgtm-pack/README.md`** — "OpenTelemetry collector wiring" section explaining the architecture, the override CM, and the uninstall behavior.
- **NIC docs** — short note in observability docs explaining that the OTel collector exposes a `opentelemetry-collector-overrides` ConfigMap extension point that software packs can populate. (Future work — not in this PR.)
- **`nebari-lgtm-pack/examples/opentelemetry-collector-overrides.yaml`** — kept as standalone reference.

## Rejected approaches

### Approach 1 (original) — `ignoreDifferences` + `RespectIgnoreDifferences=true`

The original design had the LGTM pack in-place patch NIC's chart-rendered ConfigMap, with ArgoCD configured to ignore changes to the `data.relay` field. It made it past unit tests, code review, smoke testing on a stub cluster, and the first PR write-up.

It failed in real-cluster testing. ArgoCD's `RespectIgnoreDifferences=true` only suppresses the diff calculation (UI shows Synced) — the apply step on every sync still writes the full rendered resource via a `kubectl apply` (client-side) Update operation, clobbering LGTM's patch. This is upstream ArgoCD bug [argo-cd#7478](https://github.com/argoproj/argo-cd/issues/7478) — open since 2021 with ~150 reactions and no fix.

Tested variants, all failed identically:
- `jsonPointers: [/data/relay]`
- `jqPathExpressions: [.data.relay]`
- Per-resource `argocd.argoproj.io/sync-options: ServerSideApply=true`
- App-level `argocd.argoproj.io/server-side-diff: "true"`
- `managedFieldsManagers: [lgtm-pack]` combined with `kubectl apply --server-side --field-manager=lgtm-pack`

`managedFields` on the live ConfigMap confirmed the failure mode: after a sync, `argocd-controller` had two entries — an SSA `Apply` (correctly excluding `data.relay` per ignoreDifferences) AND a separate CSA `Update` (with `kubectl.kubernetes.io/last-applied-configuration` annotation) that included `data.relay` and overwrote whatever was there.

### Approach 2 — separate collector instance

Have the LGTM pack ship its own `opentelemetry-collector-lgtm` Helm release (different release name → different fullname → different DaemonSet and ConfigMap). Cleanest ownership but requires the LGTM pack to either suspend NIC's collector or alias its Service name; downstream workloads would need to know which collector to send OTLP to. Rejected as too invasive on UX.

### Approach 3 — NIC config flag baking in LGTM endpoints

User adds `software_packs.lgtm: true` in the `nic` config. NIC's chart values then bake in the LGTM exporter/pipeline overrides at render time. Simplest mechanics but couples NIC to specific software packs. Rejected against the "fully automatic via Helm install" UX target.

## Open questions

None — design fully refactored; PRs updated to match.
