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

The OTel Collector's confmap system deep-merges these configurations. Later configs override earlier ones for scalar values; arrays and maps are merged recursively.

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
