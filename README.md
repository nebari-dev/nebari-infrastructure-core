<p align="center">
  <a href="https://nebari.dev">
    <img src="docs/assets/nebari-logo.svg" alt="Nebari" width="400">
  </a>
</p>

<h1 align="center">Nebari Infrastructure Core</h1>

<p align="center">
  <strong>An opinionated Kubernetes distribution built for AI/ML workflows.</strong>
  <br />
  One config file. Production-ready platform. Any cloud.
</p>

<p align="center">
  <a href="https://github.com/nebari-dev/nebari-infrastructure-core/actions/workflows/ci.yml"><img src="https://github.com/nebari-dev/nebari-infrastructure-core/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/nebari-dev/nebari-infrastructure-core/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License"></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="docs/cli-reference.md">CLI Reference</a> &middot;
  <a href="#architecture">Architecture</a> &middot;
  <a href="#roadmap">Roadmap</a> &middot;
  <a href="docs/design-doc/README.md">Documentation</a>
</p>

---

> **Status**: Under heavy development and very unstable. APIs, configuration formats, and behavior will change without notice. Not yet suitable for production use.

## What is Nebari Infrastructure Core?

Nebari Infrastructure Core (NIC) is an opinionated Kubernetes distribution that ships with sane defaults (that are fully configurable) and a suite of foundational software. A single YAML config file gives you a production-grade Kubernetes cluster with SSO, GitOps, API gateway, TLS certificates, and an OpenTelemetry exporter that plugs into whatever observability system you already run — all wired together and working out of the box.

NIC's composable architecture means you get exactly the platform you need — nothing more, nothing less. Our initial focus is AI/ML workflows (notebook environments, model serving, experiment tracking), but the foundation is general-purpose. Software Packs let you tailor the platform to your workload without carrying software you don't use.

NIC is the successor to [Nebari](https://github.com/nebari-dev/nebari), rebuilt from the ground up in Go based on seven years of lessons learned deploying data science platforms in production.

### The Problem

Getting from a managed Kubernetes cluster to a platform teams can actually use requires assembling and integrating dozens of components: identity providers, certificate management, ingress controllers, telemetry pipelines, GitOps tooling. This takes months of engineering time, and keeping it all working across environments takes even more.

### The Solution

NIC deploys a **complete platform stack** — not just a cluster. You declare what you want, NIC provisions the infrastructure and deploys foundational services that are pre-integrated and production-hardened.

On top of this foundation, **Software Packs** let you compose your platform. Software Packs are curated collections of open-source tools packaged as ArgoCD applications with a `NebariApp` Custom Resource. When installed, they automatically register with the platform — picking up SSO, routing, TLS, and telemetry with zero manual configuration.

Want JupyterHub and conda-store? Install the Data Science Pack. Need model serving? Add the ML Pack (MLflow, KServe, Envoy AI Gateway). Want dashboards and log aggregation? Add the Observability Pack (Grafana LGTM stack). Each pack is independent, so you deploy only what you need.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Software Packs                                                         │
│  Data Science · ML Serving · Observability · Your Custom Pack           │
├─────────────────────────────────────────────────────────────────────────┤
│  Nebari Operator                                                        │
│  Auto-configures SSO, routing, TLS, and telemetry via NebariApp CRD     │
├─────────────────────────────────────────────────────────────────────────┤
│  Foundational Software (deployed by ArgoCD)                             │
│  Keycloak · Envoy Gateway · cert-manager · OTel Collector · ArgoCD      │
├─────────────────────────────────────────────────────────────────────────┤
│  Kubernetes (provisioned by NIC via OpenTofu)                           │
│  VPC & Networking · Node Pools · Storage · IAM & Security               │
├─────────────────────────────────────────────────────────────────────────┤
│  Cloud Provider                                                         │
│  AWS (EKS) · GCP (GKE) · Azure (AKS) · Local (K3s)                     │
└─────────────────────────────────────────────────────────────────────────┘
```

### How It Works

```
nic deploy -f config.yaml
```

1. **Provisions infrastructure** — VPC, managed Kubernetes, node pools, storage, IAM via OpenTofu
2. **Deploys foundational software** — ArgoCD installs Keycloak, Envoy Gateway, cert-manager, OpenTelemetry Collector
3. **Activates the Nebari Operator** — watches for `NebariApp` resources, auto-configures SSO, routing, TLS, and telemetry
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

| Feature | Description |
|---------|-------------|
| **Opinionated Defaults** | Production-ready configuration out of the box — multi-AZ, autoscaling, security best practices |
| **Composable Software Packs** | Install only what you need. Each pack auto-integrates with SSO, telemetry, and routing |
| **Multi-Cloud** | AWS (EKS), GCP (GKE), Azure (AKS), and local (K3s) from the same config format |
| **GitOps Native** | ArgoCD manages all foundational software with dependency ordering and health checks |
| **OpenTelemetry Native** | Built-in OTel Collector exports metrics, logs, and traces — plugs into whatever observability system you run |
| **SSO Everywhere** | Keycloak provides centralized auth. The Nebari Operator creates OAuth clients automatically |
| **Declarative** | One YAML config file. NIC reconciles actual state to match using OpenTofu |
| **DNS Automation** | Optional Cloudflare provider for automatic DNS record management |

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
./nic validate -f config.yaml

# Deploy everything
./nic deploy -f config.yaml
```

See the [CLI Reference](docs/cli-reference.md) for all commands and options.

## Project Structure

```
cmd/nic/              CLI entry point and commands
pkg/
  ├── argocd/         ArgoCD installation, Helm charts, app manifests
  ├── config/         Configuration parsing and validation
  ├── dnsprovider/    DNS provider interface (Cloudflare)
  ├── git/            Git client for GitOps repository management
  ├── kubeconfig/     Kubeconfig generation
  ├── provider/       Cloud provider interface
  │   ├── aws/        AWS provider (EKS, VPC, EFS, IAM)
  │   ├── gcp/        GCP provider
  │   ├── azure/      Azure provider
  │   └── local/      Local Kind/K3s provider
  ├── telemetry/      OpenTelemetry setup
  └── tofu/           OpenTofu binary management and execution
terraform/            OpenTofu/Terraform modules per provider
examples/             Sample configuration files
docs/                 Architecture docs, design decisions, ADRs
```

## Roadmap

NIC is under active development. Here's where we're headed:

### Completed

- [x] Core CLI with provider abstraction and AWS provider
- [x] Foundational software deployment via ArgoCD (Keycloak, cert-manager, Envoy Gateway, OTel Collector)
- [x] Nebari Operator with `NebariApp` CRD (auto-SSO, routing, telemetry)
- [x] Multi-cloud support (AWS, GCP, Azure, Local)
- [x] OpenTelemetry instrumentation throughout
- [x] Cloudflare DNS provider integration

### In Progress

- [ ] AWS credential validation with IAM policy simulation
- [ ] OpenTofu output piped through structured logging
- [ ] State lock recovery (`nic unlock`)

### Planned

- [ ] Software Pack marketplace and community registry
- [ ] Configuration overlays for multi-environment support (base + dev/staging/prod)
- [ ] Git repository auto-provisioning with CI/CD workflow generation
- [ ] Application stack specification (databases, caching, queues in config)
- [ ] Compliance profiles (HIPAA, SOC2, PCI-DSS)

See the [full milestone plan](docs/design-doc/operations/13-milestones.md) and [future enhancements spec](docs/design-doc/appendix/15-future-enhancements.md) for details.

## Documentation

| Document | Description |
|----------|-------------|
| [CLI Reference](docs/cli-reference.md) | All commands, flags, and configuration options |
| [Architecture Overview](docs/design-doc/architecture/02-system-overview.md) | System components and deployment flow |
| [Design Decisions](docs/design-doc/architecture/04-key-decisions.md) | Why OpenTofu, terraform-exec, and ArgoCD |
| [Configuration Reference](docs/design-doc/appendix/16-configuration-reference.md) | Complete config.yaml schema and examples |
| [Nebari Operator](docs/design-doc/implementation/11-nebari-operator.md) | NebariApp CRD and automatic service integration |
| [Testing Strategy](docs/design-doc/operations/12-testing-strategy.md) | Unit, integration, and provider testing approach |

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
