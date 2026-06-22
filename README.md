<p align="center">
  <a href="https://nebari.dev">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/nebari-dev/nebari-design/main/logo-mark/horizontal/standard/Nebari-Logo-Horizontal-Lockup-White-text.png">
      <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/nebari-dev/nebari-design/main/logo-mark/horizontal/standard/Nebari-Logo-Horizontal-Lockup.png">
      <img alt="Nebari" src="docs/Nebari-Logo-Horizontal-Lockup.png" width="300">
    </picture>
  </a>
</p>

<h1 align="center">Nebari Infrastructure Core</h1>

<p align="center">
  <strong>An opinionated Kubernetes distribution built for AI/ML workflows.</strong>
  <br />
  One config file. Production-ready platform. Any cloud.
</p>

<p align="center">
  <a href="https://github.com/nebari-dev/nebari-infrastructure-core/actions/workflows/ci.yml"><img
  src="https://github.com/nebari-dev/nebari-infrastructure-core/actions/workflows/ci.yml/badge.svg" alt="CI"></a> <a
  href="https://github.com/nebari-dev/nebari-infrastructure-core/blob/main/LICENSE"><img
  src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License"></a> <a href="https://golang.org"><img
  src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &middot; <a href="docs/cli-reference.md">CLI Reference</a> &middot; <a
  href="#architecture">Architecture</a> &middot; <a href="#roadmap">Roadmap</a> &middot; <a
  href="docs/design-doc/README.md">Documentation</a>
</p>



> **Status**: Under heavy development and very unstable. APIs, configuration formats, and behavior will change without
> notice. Not yet suitable for production use.

## What is Nebari Infrastructure Core?

Nebari Infrastructure Core (NIC) is an opinionated Kubernetes distribution that ships with sane defaults (that are fully
configurable) and a suite of foundational software. A single YAML config file gives you a production-grade Kubernetes
cluster with SSO, GitOps, API gateway, TLS certificates, and an OpenTelemetry exporter that plugs into whatever
observability system you already run — all wired together and working out of the box.

NIC's composable architecture means you get exactly the platform you need — nothing more, nothing less. Our initial
focus is AI/ML workflows (notebook environments, model serving, experiment tracking), but the foundation is
general-purpose. Software Packs let you tailor the platform to your workload without carrying software you don't use.

NIC is the successor to [Nebari](https://github.com/nebari-dev/nebari), rebuilt from the ground up, based on seven years
of lessons learned deploying data science platforms in production.

### The Problem

Getting from a managed Kubernetes cluster to a platform teams can actually use requires assembling and integrating
dozens of components: identity providers, certificate management, ingress controllers, telemetry pipelines, GitOps
tooling. This takes months of engineering time, and keeping it all working across environments takes even more.

### The Solution

NIC deploys a **complete platform stack** — not just a cluster. You declare what you want, NIC provisions the
infrastructure and deploys foundational services that are pre-integrated and production-hardened.

On top of this foundation, **Software Packs** let you compose your platform. Software Packs are curated collections of
open-source tools packaged as ArgoCD applications with a `NebariApp` Custom Resource. When installed, they automatically
register with the platform — picking up SSO, routing, TLS, and telemetry with zero manual configuration.

Want JupyterHub and conda-store? Install the Data Science Pack. Need model serving? Add the ML Pack (MLflow, KServe,
Envoy AI Gateway). Want dashboards and log aggregation? Add the Observability Pack (Grafana LGTM stack). Each pack is
independent, so you deploy only what you need.

## Architecture

```mermaid
flowchart TD
  subgraph SP["Software Packs"]
    direction LR
    ds["Data Science"] ~~~ ml["ML Serving"] ~~~ obs["Observability"] ~~~ custom["Your Pack"]
  end

  subgraph NO["Nebari Operator"]
    op["Auto-configures SSO, routing, TLS, telemetry via NebariApp CRD"]
  end

  subgraph FS["Foundational Software"]
    direction LR
    kc["Keycloak"] ~~~ eg["Envoy GW"] ~~~ cm["cert-manager"] ~~~ ot["OTel"] ~~~ ac["ArgoCD"]
  end

  subgraph K8["Kubernetes Cluster"]
    direction LR
    vpc["VPC"] ~~~ np["Node Pools"] ~~~ st["Storage"] ~~~ iam["IAM"]
  end

  subgraph CP["Cloud Provider"]
    direction LR
    aws["AWS EKS"] ~~~ gcp["GCP GKE"] ~~~ az["Azure AKS"] ~~~ hz["Hetzner K3s"] ~~~ k3s["Local K3s"]
  end

  SP --> NO --> FS --> K8 --> CP

  style SP fill:#f3e8fc,stroke:#c840e9,color:#6b21a8
  style NO fill:#d4f5f2,stroke:#20aaa1,color:#0d5d57
  style FS fill:#fef0db,stroke:#e8952c,color:#7c4a03
  style K8 fill:#eeeef3,stroke:#4a4a6a,color:#1a1a2e
  style CP fill:#e8faf8,stroke:#20aaa1,color:#0d5d57
```

### How It Works

```
nic deploy -f config.yaml
```

1. **Provisions infrastructure** — VPC, managed Kubernetes, node pools, storage, IAM via OpenTofu
2. **Deploys foundational software** — ArgoCD installs Keycloak, Envoy Gateway, cert-manager, OpenTelemetry Collector
3. **Activates the Nebari Operator** — watches for `NebariApp` resources, auto-configures SSO, routing, TLS, and
   telemetry
4. **Configures DNS** — optional Cloudflare integration for automatic record management

## Launchpad

Every NIC deployment includes a landing page where users discover and access all deployed services.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/launchpad-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/launchpad-light.png">
    <img alt="Nebari Launchpad — service discovery and access portal" src="docs/assets/launchpad-light.png" width="800">
  </picture>
</p>

## Key Features

| Feature                       | Description                                                                                                  |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------ |
| **Opinionated Defaults**      | Production-ready configuration out of the box — multi-AZ, autoscaling, security best practices               |
| **Composable Software Packs** | Install only what you need. Each pack auto-integrates with SSO, telemetry, and routing                       |
| **Multi-Cloud**               | AWS (EKS), GCP (GKE), Azure (AKS), Hetzner (K3s), and local (K3s) from the same config format                |
| **GitOps Native**             | ArgoCD manages all foundational software with dependency ordering and health checks                          |
| **OpenTelemetry Native**      | Built-in OTel Collector exports metrics, logs, and traces — plugs into whatever observability system you run |
| **SSO Everywhere**            | Keycloak provides centralized auth. The Nebari Operator creates OAuth clients automatically                  |
| **Declarative**               | One YAML config file. NIC reconciles actual state to match using OpenTofu                                    |
| **DNS Automation**            | Optional Cloudflare provider for automatic DNS record management                                             |

## Quick Start

### Prerequisites

- Go 1.25+
- Cloud provider credentials (AWS, GCP, or Azure) configured via environment variables

NIC automatically downloads and manages its own OpenTofu binary — no manual installation required.

### Install

```bash
# From source
make build

# Or install to $GOPATH/bin
make install
```

### Deploy

```bash
# Copy and edit a sample config
cp examples/aws-config.yaml config.yaml

# Set your credentials
cp .env.example .env  # Edit with your cloud provider credentials

# Validate your config
./nic validate

# Deploy everything
./nic deploy
```

See the [CLI Reference](docs/cli-reference.md) for all commands and options.

### `nic deploy`

Deploy infrastructure and foundational services based on a configuration file.

```bash
./nic deploy [flags]
./nic deploy -f <config-file> [flags]
```

The `-f` flag is optional. When omitted, NIC looks for `config.yaml` in the current directory. You can also set
`NIC_CONFIG_PATH` as an environment variable.

Options:

- `-f, --file`: Path to config.yaml file (auto-discovered if omitted)
- `--dry-run`: Preview changes without applying them
- `--timeout`: Override default timeout (e.g., '45m', '1h')
- `--regen-apps`: Regenerate ArgoCD application manifests even if already bootstrapped

The deploy command:

1. Provisions cloud infrastructure via the selected provider (OpenTofu)
2. Bootstraps a GitOps repository with ArgoCD application manifests (if configured)
3. Installs ArgoCD and foundational services (Keycloak, Envoy Gateway, cert-manager)
4. Configures DNS records (if a DNS provider is configured)

### `nic validate`

Validate a configuration file without deploying any infrastructure.

```bash
./nic validate
./nic validate -f <config-file>
```

Options:

- `-f, --file`: Path to config.yaml file (auto-discovered if omitted)

### `nic destroy`

Destroy all infrastructure resources.

```bash
./nic destroy [flags]
./nic destroy -f <config-file> [flags]
```

Options:

- `-f, --file`: Path to config.yaml file (auto-discovered if omitted)
- `--auto-approve`: Skip confirmation prompt and destroy immediately
- `--dry-run`: Show what would be destroyed without actually deleting
- `--force`: Continue destruction even if some resources fail to delete
- `--timeout`: Override default timeout (e.g., '45m', '1h')

**WARNING**: This operation is destructive and cannot be undone.

### `nic kubeconfig`

Generate a kubeconfig for the deployed Kubernetes cluster.

```bash
./nic kubeconfig [-o output-file]
./nic kubeconfig -f <config-file> [-o output-file]
```

Options:

- `-f, --file`: Path to config.yaml file (auto-discovered if omitted)
- `-o, --output`: Path to output kubeconfig file (defaults to stdout)

### `nic version`

Show version information and registered providers.

```bash
./nic version
```

## Configuration

NIC uses a YAML configuration file. See the `examples/` directory for sample configurations:

- `examples/aws-config.yaml` - AWS/EKS configuration
- `examples/aws-config-with-dns.yaml` - AWS with Cloudflare DNS automation
- `examples/aws-existing.yaml` - Deploy to an existing EKS cluster
- `examples/gcp-config.yaml` - GCP/GKE configuration
- `examples/azure-config.yaml` - Azure/AKS configuration
- `examples/hetzner-config.yaml` - Hetzner Cloud/K3s configuration
- `examples/local-config.yaml` - Local Kind/K3s configuration

### Environment Variables

Secrets are never stored in configuration files. Use environment variables or a `.env` file (see `.env.example`):

```bash
# Copy the example and fill in your values
cp .env.example .env
```

## OpenTelemetry Configuration

NIC supports OpenTelemetry tracing with configurable exporters:

- `OTEL_EXPORTER`: Exporter type — `none` (default), `console`, `otlp`, or `both`
- `OTEL_ENDPOINT`: OTLP endpoint (default: `localhost:4317`)

```bash
# Console traces (debugging) — config.yaml auto-discovered in current directory
OTEL_EXPORTER=console ./nic deploy

# OTLP traces
OTEL_EXPORTER=otlp OTEL_ENDPOINT=localhost:4317 ./nic deploy -f config.yaml
```

## Development

### Local Cluster Testing with Kind

For local development, you can deploy a Kind cluster with foundational services:

```bash
make localkind-up    # Create Kind cluster and deploy
make localkind-down  # Tear down
```

When using a remote repo, a repo URL must be set in your `local-config.yaml`, and a valid private SSH key must be set as the `GIT_SSH_PRIVATE_KEY` environment variable. 

Ommitting the `git_repository` or explicitely setting a local git path will result in a local git directory being used for gitops. 

### Local Cluster Testing with an Existing Cluster (k3s/k3d/minikube)

The `local` provider works against any cluster already present in your kubeconfig — it does not create the cluster. To use a tool other than Kind:

1. **Create the cluster** with your tool of choice.
2. **Point NIC at it** by setting `kube_context` in your `local-config.yaml` to the context name of that cluster (NIC reads the kubeconfig from `$KUBECONFIG`, falling back to `~/.kube/config`). `kube_context` is a context *name*, not a file path — list available names with `kubectl config get-contexts -o name`.
3. **Make the local GitOps directory visible to the cluster.** When `git_repository` is omitted (or set to a `file://` path), NIC uses a local GitOps directory at `/tmp/nebari-gitops-<project_name>`, where `project_name` comes from your config. ArgoCD's repo-server mounts this path via a `hostPath` volume, so it must exist *inside* the cluster node, not just on your host. Cluster nodes run in containers/VMs that don't share your host filesystem, so the directory must be bind-mounted in when the cluster is created. The `make localkind-up` target does this for you by generating a kind config with `extraMounts`; for k3d and minikube you mount it manually as shown below.

#### k3d

k3d nodes run as Docker containers and don't see your host's `/tmp` by default. Create the directory first, then mount it into the nodes at the same path:

```bash
mkdir -p /tmp/nebari-gitops-my-nebari-local

k3d cluster create \
  --volume /tmp/nebari-gitops-my-nebari-local:/tmp/nebari-gitops-my-nebari-local@all

k3d kubeconfig get --all > kubeconfig
export KUBECONFIG=$(pwd)/kubeconfig

./nic deploy --file local-config.yaml
```

Set `kube_context: "k3d-<cluster-name>"` in your config (k3d prefixes the context with `k3d-`). For k3s clusters, also set `storage_class: local-path` and disable MetalLB (k3s ships ServiceLB) as noted in `examples/local-config.yaml`.

#### minikube

minikube runs the node inside a VM/container. Mount the host directory before deploying:

```bash
mkdir -p /tmp/nebari-gitops-my-nebari-local

minikube start
minikube mount /tmp/nebari-gitops-my-nebari-local:/tmp/nebari-gitops-my-nebari-local &

export KUBECONFIG=$HOME/.kube/config   # minikube updates this automatically
./nic deploy --file local-config.yaml
```

`minikube mount` runs in the foreground and must stay running for the duration of the deploy (and while ArgoCD is reconciling), so launch it in a separate terminal or background it as shown. Set `kube_context: "minikube"` in your config.

> If you'd rather avoid the host-path mount entirely, set an explicit remote `git_repository` (see OPTION 3 in `examples/local-config.yaml`); ArgoCD then clones the repo over HTTPS/SSH and no local directory needs to be mounted into the node.

### Running Tests

```bash
# Run all tests
go test ./... -v

# Run with coverage
go test ./... -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Code Quality

```bash
# Format, vet, lint, and test
make check

# Or individually:
make fmt
make vet
make lint
make test
```

### Pre-commit Hooks

```bash
# Install hooks (one-time setup)
pre-commit install

# Run all hooks manually
pre-commit run --all-files
```

### Project Structure

```
cmd/nic/              CLI entry point and commands
pkg/
  ├── argocd/         ArgoCD installation, Helm charts, app manifests
  ├── config/         Configuration parsing and validation
  ├── git/            Git client for GitOps repository management
  ├── kubeconfig/     Kubeconfig generation
  ├── providers/      Provider implementations
  │   ├── cluster/    Cluster/cloud provider interface
  │   │   ├── aws/        AWS provider (EKS, VPC, EFS, IAM)
  │   │   ├── gcp/        GCP provider
  │   │   ├── azure/      Azure provider
  │   │   ├── hetzner/    Hetzner Cloud provider (K3s via hetzner-k3s)
  │   │   └── local/      Local Kind/K3s provider
  │   └── dns/        DNS provider interface
  │       └── cloudflare/ Cloudflare DNS provider
  ├── telemetry/      OpenTelemetry setup
  └── tofu/           OpenTofu binary management and execution
terraform/            OpenTofu/Terraform modules per provider
examples/             Sample configuration files
docs/                 Architecture docs, design decisions, ADRs
```

## Roadmap

NIC is under very active development.

Our current roadmap can be found at [2026-02-04-roadmap.md](docs/plans/2026-02-04-roadmap.md). We welcome feedback and
contributions to help shape the future of the project!

## Documentation

| Document                                             | Description                                                                                                                                                                                                                                                                                                                                                |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [CLI Reference](docs/cli-reference.md)               | All commands, flags, and configuration options                                                                                                                                                                                                                                                                                                             |
| [Design Doc](docs/design-doc/README.md)              | The original design document that laid the foundation for NIC's architecture and implementation. It includes detailed explanations of the core components, design decisions, and implementation details. The document is organized into sections covering architecture, design decisions, configuration reference, Nebari Operator, and testing strategy.) |
| [Architectural Decision Records](docs/adr/README.md) | Architectural decision records recording design decisions as we build                                                                                                                                                                                                                                                                                      |

## Contributing

Contributions are welcome! To get started:

```bash
# Clone the repo
git clone https://github.com/nebari-dev/nebari-infrastructure-core.git
cd nebari-infrastructure-core

# Install dependencies and build
make build

# Run tests
go test ./... -v

# Run all checks (fmt, vet, lint, test)
make check

# Install pre-commit hooks
pre-commit install
```

See our [issue tracker](https://github.com/nebari-dev/nebari-infrastructure-core/issues) for open issues.

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

## OpenTofu lockfile updates

If you change provider templates under `pkg/providers/cluster/**/templates/`, regenerate the provider lockfile(s) locally:

```bash
./scripts/pre-commit-tofu-lock.sh
```
