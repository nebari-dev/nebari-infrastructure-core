# Cloudflare DNS Configuration

Configuration options for Cloudflare DNS provider.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config represents Cloudflare-specific DNS configuration
Secrets like API tokens are read from environment variables, not config

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| ZoneName | `zone_name` | string | Yes | Domain zone (e.g., example.com) |

