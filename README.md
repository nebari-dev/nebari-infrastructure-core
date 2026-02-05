# Nebari Infrastructure Core (NIC)

Nebari Infrastructure Core is a standalone Go CLI tool that manages cloud infrastructure for Nebari using native cloud SDKs with declarative semantics.

## Features

- **Declarative Infrastructure**: Define your desired state, NIC reconciles actual state to match
- **Native Cloud SDKs**: Direct integration with AWS, GCP, Azure, and local K3s
- **Configuration Compatible**: Works with existing `config.yaml` files
- **OpenTelemetry Instrumented**: Full distributed tracing support
- **Structured Logging**: JSON structured logging with slog

## Quick Start

### Build

```bash
go build -o nic ./cmd/nic
```

### Usage

```bash
# Show version and registered providers
./nic version

# Validate configuration file
./nic validate -f config.yaml

# Deploy infrastructure
./nic deploy -f config.yaml

# Destroy infrastructure
./nic destroy -f config.yaml
```

## Commands

### `nic deploy`

Deploy infrastructure based on configuration file.

```bash
./nic deploy -f <config-file>
```

Options:

- `-f, --file`: Path to config.yaml file (required)

### `nic validate`

Validate configuration file without deploying.

```bash
./nic validate -f <config-file>
```

Options:

- `-f, --file`: Path to config.yaml file (required)

### `nic destroy`

Destroy all infrastructure resources in reverse order of creation.

```bash
./nic destroy -f <config-file>
```

Options:

- `-f, --file`: Path to config.yaml file (required)
- `--auto-approve`: Skip confirmation prompt and destroy immediately
- `--dry-run`: Show what would be destroyed without actually deleting
- `--force`: Continue destruction even if some resources fail to delete
- `--timeout`: Override default timeout (e.g., '45m', '1h')

**WARNING**: This operation is destructive and cannot be undone. By default, you will be prompted to confirm before destruction begins.

Example with dry-run:

```bash
# Preview what would be destroyed
./nic destroy -f config.yaml --dry-run

# Destroy with confirmation prompt
./nic destroy -f config.yaml

# Destroy without confirmation
./nic destroy -f config.yaml --auto-approve
```

### `nic version`

Show version information and registered providers.

```bash
./nic version
```

## Configuration

NIC uses the standard `config.yaml` format.

### Configuration Reference

Full documentation for all configuration options is available in [`docs/configuration/`](docs/configuration/):

- [Core Configuration](docs/configuration/core.md) - Project name, provider, domain, certificates
- [AWS Configuration](docs/configuration/aws.md) - EKS, VPC, node groups, EFS
- [GCP Configuration](docs/configuration/gcp.md) - GKE, VPC, node pools, GPUs
- [Azure Configuration](docs/configuration/azure.md) - AKS, networking, node pools
- [Local Configuration](docs/configuration/local.md) - K3s, kind, minikube
- [Git Repository](docs/configuration/git.md) - GitOps with ArgoCD
- [Cloudflare DNS](docs/configuration/cloudflare.md) - DNS provider

> **Note**: Configuration docs are auto-generated from source code. Run `make docs` to regenerate.

### Example Configurations

See `examples/` directory for complete sample configurations:

- `examples/aws-config.yaml` - AWS/EKS configuration
- `examples/gcp-config.yaml` - GCP/GKE configuration
- `examples/azure-config.yaml` - Azure/AKS configuration
- `examples/local-config.yaml` - Local K3s configuration

## OpenTelemetry Configuration

NIC supports OpenTelemetry tracing with configurable exporters:

### Environment Variables

- `OTEL_EXPORTER`: Exporter type (default: "none")

  - `none` - Disable trace export (traces still collected, default)
  - `console` - Export traces to stdout (development/debugging)
  - `otlp` - Export to OTLP endpoint
  - `both` - Export to both console and OTLP

- `OTEL_ENDPOINT`: OTLP endpoint (default: "localhost:4317")

### Examples

```bash
# No trace export (default)
./nic deploy -f config.yaml

# Console traces (debugging)
OTEL_EXPORTER=console ./nic deploy -f config.yaml

# OTLP traces
OTEL_EXPORTER=otlp OTEL_ENDPOINT=localhost:4317 ./nic deploy -f config.yaml

# Both console and OTLP
OTEL_EXPORTER=both ./nic deploy -f config.yaml

# No trace export
OTEL_EXPORTER=none ./nic deploy -f config.yaml
```

## Development

### Local cluster testing with Kind and OrbStack
Due to difficulties with how networking works with Docker Desktop, using OrbStack for Docker is recommended on Mac.

See these docs for installing OrbStack: https://docs.orbstack.dev/quick-start

A github repo needs to be created and the URL added to `local-config.yaml` file.

A valid private SSH key needs to be set as an environment variable `GIT_SSH_PRIVATE_KEY`

With these configured, to deploy a kind cluster and the foundational software with the `local-config.yaml` file, run the following command:

```bash
make localkind-up
```

In order to enable local UI access on a browser, add the following to /etc/hosts:
```bash
/etc/hosts: 192.168.1.100 keycloak.nebari.local argocd.nebari.local
```

Now ArgoCD and Keycloak can be accessed at the following URLs:
- https://argocd.nebari.local
- https://keycloak.nebari.local

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
# Format code
go fmt ./...

# Vet code
go vet ./...

# Lint (requires golangci-lint)
golangci-lint run
```

### Pre-commit Hooks

This project uses pre-commit hooks to ensure code quality before commits. The hooks automatically run formatting, linting, and tests.

#### Installation

```bash
# Install pre-commit hooks (one-time setup)
pre-commit install
```

#### Usage

Pre-commit hooks will automatically run on `git commit`. To manually run all hooks:

```bash
# Run all pre-commit hooks on all files
pre-commit run --all-files

# Run specific hook
pre-commit run golangci-lint --all-files
```

#### Configured Hooks

- **trailing-whitespace**: Remove trailing whitespace
- **end-of-file-fixer**: Ensure files end with newline
- **check-yaml**: Validate YAML files
- **check-added-large-files**: Prevent large files from being committed
- **check-merge-conflict**: Detect merge conflict markers
- **go-fmt**: Format Go code with `gofmt -s -w`
- **go-vet**: Run `go vet` for static analysis
- **golangci-lint**: Run comprehensive linting with auto-fix
- **go-test**: Run all tests with `go test -v ./...`

## Architecture

### Project Structure

```
cmd/
  ├── nic/            # CLI entry point and commands
  └── docgen/         # Configuration documentation generator
docs/
  └── configuration/  # Auto-generated config reference
pkg/
  ├── config/         # Configuration parsing
  ├── provider/       # Provider interface and registry
  │   ├── aws/        # AWS provider implementation
  │   ├── gcp/        # GCP provider implementation
  │   ├── azure/      # Azure provider implementation
  │   └── local/      # Local K3s provider implementation
  └── telemetry/      # OpenTelemetry setup
```

### Provider Registration

All providers are explicitly registered in `cmd/nic/main.go`:

```go
registry := provider.NewRegistry()
registry.Register(ctx, "aws", aws.NewProvider())
registry.Register(ctx, "gcp", gcp.NewProvider())
registry.Register(ctx, "azure", azure.NewProvider())
registry.Register(ctx, "local", local.NewProvider())
```

## Current Status

### AWS Provider (Fully Implemented)

The AWS provider has complete native SDK implementation with stateless reconciliation:

- **VPC**: Creates/manages VPC, subnets, internet gateway, NAT gateways, route tables, security groups
- **EKS**: Creates/manages EKS cluster with version upgrades, logging, endpoint access configuration
- **Node Groups**: Parallel creation/update/deletion with scaling, labels, and taints
- **EFS**: Optional shared storage with mount targets
- **IAM**: Service roles and node instance profiles

**Reconciliation behavior:**
- Discovers actual state by querying AWS APIs with NIC tags (`nic.nebari.dev/*`)
- Compares all config attributes against actual state
- Creates missing resources, updates mutable fields, deletes orphaned resources
- Errors on immutable field changes requiring manual intervention:
  - VPC: CIDR, availability zones
  - EKS: KMS encryption key
  - Node Groups: instance type, AMI type, capacity type (Spot)
  - EFS: performance mode, encryption, KMS key

### GCP, Azure, Local Providers (Stubs)

These providers are stub implementations that print config as JSON and return success. Native SDK implementation pending.

### Next Steps

- Implement GCP provider with Google Cloud Client Libraries
- Implement Azure provider with Azure SDK for Go
- Implement local K3s provider
- Add import from Terraform functionality

## License

See LICENSE file for details.
