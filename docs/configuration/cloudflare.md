# Cloudflare DNS Configuration

Configuration options for Cloudflare DNS provider.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config represents Cloudflare DNS provider configuration.
API credentials (CLOUDFLARE_API_TOKEN) must be set via environment variables.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| ZoneName | `zone_name` | string | Yes | ZoneName is the DNS zone/domain to manage (e.g., example.com) |
| Email | `email` | string | No | Email is the Cloudflare account email (optional, for API key auth) |
