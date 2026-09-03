# Local GitOps Repository Configuration

Configuration options for the NIC-managed local GitOps repository ArgoCD syncs from.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)

---

## Config

Config holds the configuration for the local repository provider.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Path | `path` | string | No | Path is the directory of the repository. When empty, the provider defaults to ~/.nic/gitops/<project_name>, falling back to a per-project directory under the OS temp dir only when the home director... |
| Branch | `branch` | string | No | Branch is the git branch to use (default: "main"). |

