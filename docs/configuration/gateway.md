# Gateway Configuration

Tuning for the shared nebari-gateway, such as the request buffer limit that bounds LLM request size.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [GatewayConfig](#gatewayconfig)

---

## GatewayConfig

GatewayConfig tunes the shared nebari-gateway that NIC deploys.

request_buffer_limit bounds the size of a request the gateway will buffer
per client connection. Envoy AI Gateway buffers the whole request body to
read the model name, so a larger request is rejected with HTTP 413 before it
reaches the model backend. Envoy's own default of 32Ki is too small for
coding assistants, whose requests carry file context and conversation
history. The limit is an upper bound, not a preallocation, but every
connection can grow to it, so a large value raises worst-case gateway memory:
the Envoy proxy pod has a 512Mi memory limit, and a handful of slow
connections each holding a large limit can get it OOM-killed. Raise the
limit only as far as your largest requests need.

Example YAML:

	gateway:
	  request_buffer_limit: 20Mi

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| RequestBufferLimit | `request_buffer_limit` | string | No | Largest request buffered per client connection; larger ones get HTTP 413. Default: 10Mi. A whole-number quantity (e.g. 20Mi, 100M, 1048576), from 32Ki (32768 bytes) to 4294967295 bytes. |

