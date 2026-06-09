# ADR-0005: OpenTelemetry Collector Software Pack Override Point

## Status

Accepted

## Date

2026-06-02

## Context

The OpenTelemetry Collector is deployed as a foundational service in NIC, configured with sensible defaults for metrics collection (pod scraping, cAdvisor, kubelet). However, software packs like `nebari-lgtm-pack` need to customize the collector's configuration — adding exporters (OTLP to Mimir/Tempo/Loki), adjusting pipelines, or enabling additional receivers.

ArgoCD's `ignoreDifferences` mechanism would be the natural way to let external resources modify a shared ConfigMap, but it has a critical bug: ignored differences are not preserved across sync operations (argoproj/argo-cd#7478). This means any configuration injected by a software pack would be reverted on the next ArgoCD sync.

We need an extension mechanism that:
- Allows software packs to inject collector configuration without NIC needing to know about each pack
- Survives ArgoCD sync operations
- Fails visibly when misconfigured (not silently falling back)
- Documents the contract surface so external repos can depend on it

## Decision Drivers

- Software packs must be able to extend collector config without modifying NIC
- ArgoCD sync must not overwrite pack-provided configuration
- Failures should be diagnosable from pod logs
- The contract must be documented for external pack authors
- ADR-0003 established the pack-integration model; this must align with it

## Considered Options

1. Shared ConfigMap with ignoreDifferences
2. Separate ConfigMap merged at collector startup
3. Helm chart values override via ArgoCD ApplicationSet

## Decision Outcome

Chosen option: "Option 2 - Separate ConfigMap merged at collector startup", because it sidesteps the ArgoCD ignoreDifferences bug entirely and keeps NIC and software pack concerns cleanly separated.

### Consequences

**Good:**
- NIC and software packs never write to the same Kubernetes resource
- ArgoCD sync cannot overwrite pack configuration
- Init container logs show which path was taken (override vs fallback)
- Contract is explicit and documented

**Bad:**
- Pack authors must create a ConfigMap with the exact name/namespace/key
- Silent fallback to `{}` if ConfigMap is misconfigured (mitigated by init container logging)
- Additional complexity compared to a single ConfigMap
- Only one software pack can own the override ConfigMap; supporting multiple collector-customizing packs is an explicit non-goal (see [Single-Owner Constraint](#single-owner-constraint))

## Contract Surface

Software packs that need to customize the OpenTelemetry Collector must create a ConfigMap with the following specification:

| Field | Value | Notes |
|-------|-------|-------|
| Name | `opentelemetry-collector-overrides` | Exact match required |
| Namespace | `monitoring` | Must match collector's deployment namespace |
| Key | `relay.yaml` | Contains partial OTel config |
| Content | Valid YAML | Deep-merged with base config via `--config` flag |

### Example ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: opentelemetry-collector-overrides
  namespace: monitoring
data:
  relay.yaml: |
    exporters:
      otlphttp/mimir:
        endpoint: http://mimir.monitoring:9009/otlp
    service:
      pipelines:
        metrics:
          exporters: [otlphttp/mimir]
```

### Merge Behavior

The collector is started with two `--config` flags:
1. The base configuration (from the Helm chart's ConfigMap)
2. `/conf/overrides/relay.yaml` (from this extension point)

The OTel Collector's confmap system merges these configurations as follows: **maps are deep-merged** (keys defined only in the base config are preserved; later sources override matching keys), but **lists/arrays are not merged — the last `--config` source wins and replaces the list wholesale**. (Appending lists is opt-in via the experimental `confmap.enableMergeAppendOption` feature gate, which NIC does not enable.) Practical consequence for pack authors: specify only the map keys you want to change, but any list you touch — e.g. a pipeline's `exporters` — must be re-stated in full. Setting a map key to `null` deletes that component from the base, so use `{}` for an empty map. See the [confmap merge docs](https://github.com/open-telemetry/opentelemetry-collector/blob/main/confmap/README.md).

### Failure Modes

| Scenario | Behavior | Diagnosis |
|----------|----------|-----------|
| ConfigMap missing | Init container writes `{}`, collector runs on defaults | Pod logs: "no /src/relay.yaml found" |
| ConfigMap exists, wrong key name | Same as missing | Pod logs: "no /src/relay.yaml found" |
| ConfigMap in wrong namespace | Same as missing | Pod logs: "no /src/relay.yaml found" |
| Invalid YAML in relay.yaml | Collector fails to start | Collector logs show parse error |
| Valid YAML, invalid OTel config | Collector fails to start | Collector logs show config validation error |

Pack authors should verify their override landed by checking the `ensure-overrides` init container logs:
```bash
kubectl logs -n monitoring <collector-pod> -c ensure-overrides
```

Expected output when override is applied:
```
ensure-overrides: found /src/relay.yaml from software pack ConfigMap, using override config
```

### Single-Owner Constraint

**Supporting more than one collector-customizing software pack is an explicit
non-goal of this design.** The override point is a **singleton**: it is keyed to
one fixed ConfigMap (`opentelemetry-collector-overrides`) and the collector
merges exactly two configs (base + this override). Exactly **one** pack may own
the override.

This is an unusual case in practice — a cluster has a single observability
backend — so the contract optimizes for that rather than for composition. For
the record, here is what happens if two packs both try to customize the
collector:

- **Resource collision.** Each pack is its own ArgoCD Application, so both
  render a ConfigMap with the same name/namespace. ArgoCD raises a
  `SharedResourceWarning` and, with `selfHeal: true`, the two Applications
  thrash — each sync reverts the other's `relay.yaml`. (Via plain Helm, the
  second `helm install` fails: the ConfigMap already exists and is owned by
  another release.)
- **Silent pipeline clobbering (if forced to coexist).** `confmap` deep-merges
  maps but **replaces lists** — the last `--config` source wins (see
  [Merge Behavior](#merge-behavior)). Exporter *definitions* from both packs
  union fine under `exporters:`, but a pipeline's `exporters:` **list** (e.g.
  `service.pipelines.metrics.exporters`) keeps only the last writer, so one
  pack's backend is defined but never wired into the pipeline and receives no
  telemetry — with no error. The `ensure-overrides` init container cannot
  detect this; it is only visible at the ArgoCD layer.

**Guidance:**

- Reserve the override for the **single telemetry-backend pack** (e.g.
  `nebari-lgtm-pack`) that defines *where* telemetry is exported.
- **Producer** packs that merely generate telemetry must use the collector's
  ingest paths instead: send OTLP to the collector's `4317`/`4318` endpoints,
  or annotate pods with `prometheus.io/scrape`. They must never write the
  override ConfigMap.

Supporting multiple collector-customizing packs is therefore **out of scope**.
If it ever becomes a real requirement, the contract would have to change — each
pack creating a uniquely-named override ConfigMap, the collector loading all of
them via additional `--config` flags (a controller regenerating the daemonset's
volume mounts and args dynamically), and the `confmap.enableMergeAppendOption`
feature gate enabled so component lists append-and-dedupe instead of last-wins.
That work is deferred until a concrete multi-backend need exists.

## Options Detail

### Option 1: Shared ConfigMap with ignoreDifferences

NIC creates the collector's ConfigMap; software packs patch it; ArgoCD's `ignoreDifferences` preserves the patches.

**Pros:**
- Single ConfigMap, simpler mental model
- Standard ArgoCD pattern

**Cons:**
- argoproj/argo-cd#7478 breaks this on every sync
- No viable workaround exists

### Option 2: Separate ConfigMap Merged at Collector Startup (Chosen)

NIC creates the base ConfigMap; software packs create a separate `opentelemetry-collector-overrides` ConfigMap; an init container resolves the override (or falls back to `{}`); the collector merges both configs via multiple `--config` flags.

**Pros:**
- ArgoCD sync cannot affect pack-owned ConfigMap
- Clean separation of concerns
- Init container logs make debugging possible

**Cons:**
- Pack authors must know the exact ConfigMap contract
- Requires coordination on namespace

### Option 3: Helm Values Override via ArgoCD ApplicationSet

Use ArgoCD ApplicationSet with a merge generator to inject pack-specific Helm values.

**Pros:**
- Fully declarative
- ArgoCD-native

**Cons:**
- Requires ApplicationSet, adding complexity
- Pack authors would need to understand ArgoCD ApplicationSet generators
- Still subject to sync timing issues between NIC and pack apps

## Links

- [ADR-0003: Software Pack Codegen](0003-software-pack-codegen.md) - pack integration model
- [argoproj/argo-cd#7478](https://github.com/argoproj/argo-cd/issues/7478) - ignoreDifferences sync bug
- [OTel Collector confmap](https://opentelemetry.io/docs/collector/configuration/) - config merge behavior
