---
promise: P9
level: stable
state: partly built
conformance_test: pending
changes:
  - date: 2026-11-17
    kind: founding
    hash: 7976a9e71c3c513f
---
# P9: Telemetry endpoint

## Promise

A pack emits telemetry to the platform without knowing which collector runs.

## Level

stable

## State

partly built. Pods annotated with `prometheus.io/*` scrape annotations are scraped today. The OTLP endpoint values under `global.nebari` are planned and depend on P5. Tracking issue: to be filed when the contract is accepted.

## Interface

OTLP endpoints in the P5 platform values (`global.nebari.otlpGrpcEndpoint`, `global.nebari.otlpHttpEndpoint`) and the `prometheus.io/*` scrape annotations. Pods annotated `prometheus.io/scrape: "true"` with `prometheus.io/port` and an optional `prometheus.io/path` are scraped. Where the telemetry goes is the business of the observability pack. The ADR-0008 single-owner collector override is not part of the pack contract, but whoever uses it to replace the collector must keep serving OTLP at the endpoints in the platform values and keep scraping annotated pods, so this promise still holds.

## Packs may rely on

- Sending OTLP to the endpoints in the platform values, once P5 is built.
- Pods with the scrape annotations being scraped.

## Packs must not rely on

- The collector's Service name.
- The collector's DaemonSet name.
- The Prometheus Operator or `ServiceMonitor`.
- Telemetry being stored anywhere.

## Per-target notes

No difference.

## Conformance test

Planned: the `pack_contract` journey group (ADR-0017 suite) will test this on every target. A reference pack's OTLP export reaches the collector and its annotated pod is scraped on every target.
