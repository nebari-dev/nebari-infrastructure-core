# Existing GitOps Repository Configuration

Configuration options for pointing ArgoCD at a GitOps repository you already host.

> This documentation is auto-generated from source code using `go generate`.

## Table of Contents

- [Config](#config)
- [AuthConfig](#authconfig)
- [EnvRef](#envref)

---

## Config

Config holds the configuration for the existing repository provider.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| URL | `url` | string | Yes | URL is the remote repository URL (ssh or https). Examples: "git@github.com:org/repo.git", "https://github.com/org/repo.git". |
| Branch | `branch` | string | No | Branch is the git branch to use (default: "main"). |
| Path | `path` | string | No | Path is an optional subdirectory within the repository. When set, all operations are scoped to this path. |
| Auth | `auth` | AuthConfig | Yes | Auth specifies the credentials NIC uses to push to the repository (requires write access). |
| ArgoCDAuth | `argocd_auth` | `*AuthConfig` | No | ArgoCDAuth specifies optional separate credentials for ArgoCD's in-cluster read access. When unset, Auth is used. |

---

## AuthConfig

AuthConfig selects exactly one authentication method. Each method names the
environment variable its secret is read from. Example:

	auth:
	  token:
	    env: GIT_TOKEN

	auth:
	  ssh:
	    env: GIT_SSH_KEY

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Token | `token` | `*EnvRef` | No | Token authenticates over HTTPS with a token read from Token.Env. |
| SSH | `ssh` | `*EnvRef` | No | SSH authenticates over SSH with a private key read from SSH.Env. |
| InsecureSkipHostKeyVerification | `insecure_skip_host_key_verification` | bool | No | InsecureSkipHostKeyVerification disables SSH host key verification, removing protection against man-in-the-middle attacks. Only intended for ephemeral environments (e.g. CI) where maintaining a kno... |

---

## EnvRef

EnvRef names the environment variable a secret is read from.

| Field | YAML Key | Type | Required | Description |
|-------|----------|------|----------|-------------|
| Env | `env` | string | Yes |  |

