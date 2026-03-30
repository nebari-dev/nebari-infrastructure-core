# CLI Reference

## Commands

### `nic deploy`

Deploy infrastructure and foundational services based on a configuration file.

```bash
nic deploy [flags]
nic deploy -f <config-file> [flags]
```

The config file is optional. When `-f` is omitted NIC resolves it in this order:
1. `NIC_CONFIG_PATH` environment variable
2. `./config.yaml` in the current working directory

**Options:**

| Flag | Description |
|------|-------------|
| `-f, --file` | Path to config.yaml file (auto-discovered if omitted) |
| `--dry-run` | Preview changes without applying them |
| `--timeout` | Override default timeout (e.g., `45m`, `1h`) |
| `--regen-apps` | Regenerate ArgoCD application manifests even if already bootstrapped |

**What it does:**

1. Provisions cloud infrastructure via the selected provider (OpenTofu)
2. Bootstraps a GitOps repository with ArgoCD application manifests (if configured)
3. Installs ArgoCD and foundational services (Keycloak, Envoy Gateway, cert-manager)
4. Configures DNS records (if a DNS provider is configured)

### `nic validate`

Validate a configuration file without deploying any infrastructure.

```bash
nic validate
nic validate -f <config-file>
```

**Options:**

| Flag | Description |
|------|-------------|
| `-f, --file` | Path to config.yaml file (auto-discovered if omitted) |

### `nic destroy`

Destroy all infrastructure resources.

```bash
nic destroy [flags]
nic destroy -f <config-file> [flags]
```

**Options:**

| Flag | Description |
|------|-------------|
| `-f, --file` | Path to config.yaml file (auto-discovered if omitted) |
| `--auto-approve` | Skip confirmation prompt and destroy immediately |
| `--dry-run` | Show what would be destroyed without actually deleting |
| `--force` | Continue destruction even if some resources fail to delete |
| `--timeout` | Override default timeout (e.g., `45m`, `1h`) |

> **Warning**: This operation is destructive and cannot be undone.

### `nic kubeconfig`

Generate a kubeconfig for the deployed Kubernetes cluster.

```bash
nic kubeconfig [-o output-file]
nic kubeconfig -f <config-file> [-o output-file]
```

**Options:**

| Flag | Description |
|------|-------------|
| `-f, --file` | Path to config.yaml file (auto-discovered if omitted) |
| `-o, --output` | Path to output kubeconfig file (defaults to stdout) |

### `nic version`

Show version information and registered providers.

```bash
nic version
```

## Configuration

NIC uses a YAML configuration file. See the [`examples/`](../examples/) directory for sample configurations:

| Example | Description |
|---------|-------------|
| [`aws-config.yaml`](../examples/aws-config.yaml) | AWS/EKS configuration |
| [`aws-config-with-dns.yaml`](../examples/aws-config-with-dns.yaml) | AWS with Cloudflare DNS automation |
| [`aws-existing.yaml`](../examples/aws-existing.yaml) | Deploy to an existing EKS cluster |
| [`gcp-config.yaml`](../examples/gcp-config.yaml) | GCP/GKE configuration |
| [`azure-config.yaml`](../examples/azure-config.yaml) | Azure/AKS configuration |
| [`local-config.yaml`](../examples/local-config.yaml) | Local Kind/K3s configuration |

### Environment Variables

Secrets are never stored in configuration files. Use environment variables or a `.env` file (see
[`.env.example`](../.env.example)):

```bash
cp .env.example .env
```

| Variable | Description |
|----------|-------------|
| `NIC_CONFIG_PATH` | Override the config file path for all commands (lower priority than `--file`) |

### OpenTelemetry Configuration

NIC supports OpenTelemetry tracing with configurable exporters:

| Variable | Description | Default |
|----------|-------------|---------|
| `OTEL_EXPORTER` | Exporter type: `none`, `console`, `otlp`, or `both` | `none` |
| `OTEL_ENDPOINT` | OTLP collector endpoint | `localhost:4317` |

```bash
# Console traces (debugging) — config.yaml auto-discovered in current directory
OTEL_EXPORTER=console nic deploy

# OTLP traces (production) with explicit config path
OTEL_EXPORTER=otlp OTEL_ENDPOINT=localhost:4317 nic deploy -f config.yaml
```
