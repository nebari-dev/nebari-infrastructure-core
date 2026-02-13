# Git Repository Configuration

Configuration options for GitOps repository integration with ArgoCD.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [AuthConfig](#authconfig)

---

## Config

Config represents git repository configuration for GitOps bootstrap.
Secrets (SSH keys, tokens) are read from environment variables, never stored in config.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| URL | `url` | string | Yes | URL is the repository URL (SSH or HTTPS format) Examples: "git@github.com:org/repo.git" or "https://github.com/org/repo.git" |
| Branch | `branch` | string | Yes | Branch is the git branch to use (default: "main") |
| Path | `path` | string | Yes | Path is an optional subdirectory within the repository If specified, all operations are scoped to this path |
| Auth | `auth` | AuthConfig | Yes | Auth specifies credentials for NIC to push to the repository (requires write access) |
| ArgoCDAuth | `argocd_auth` | `*AuthConfig` | No | ArgoCDAuth specifies optional separate credentials for ArgoCD (read-only access) If not specified, falls back to Auth |

---

## AuthConfig

AuthConfig specifies authentication credentials for git operations.
Only one of SSHKeyEnv or TokenEnv should be set.
AuthConfig implements CredentialProvider.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| SSHKeyEnv | `ssh_key_env` | string | Yes | SSHKeyEnv is the name of the environment variable containing the SSH private key The key should be in PEM format (e.g., contents of ~/.ssh/id_ed25519) |
| TokenEnv | `token_env` | string | Yes | TokenEnv is the name of the environment variable containing the personal access token Used for HTTPS authentication |
