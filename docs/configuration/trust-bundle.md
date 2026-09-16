# Trust Bundle Configuration

Enterprise CA trust-bundle propagation to worker-node OS trust stores and, via trust-manager, into the cluster.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [TrustBundleConfig](#trustbundleconfig)

---

## TrustBundleConfig

TrustBundleConfig specifies the source of an extra CA bundle. At most one of
Path or Inline may be set; leaving both unset (or omitting the block) installs
no bundle. Path is a filesystem path to a PEM file on the operator's machine;
Inline is the PEM text itself.

When set at the top level of NebariConfig, the bundle is propagated both to
worker-node OS trust stores (via the cluster provider) and into the cluster
via trust-manager (the in-pod half of the trust-bundle propagation).

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Path | `path` | string | No |  |
| Inline | `inline` | string | No |  |

