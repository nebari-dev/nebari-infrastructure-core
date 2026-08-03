# Testing Strategy

## 12.1 Testing Levels

NIC has three testing levels today, plus one (health) that is planned but not yet implemented:

### Unit tests

- **Scope**: Pure Go packages under `pkg/` and `cmd/nic/`.
- **Runner**: `go test ./...` (or `make test` / `make test-unit`).
- **Conventions**: Table-driven tests (per [`AGENTS.md`](../../../AGENTS.md)). Interfaces are injected so concrete dependencies (AWS SDK, Helm, k8s client) can be mocked.
- **Where they run**: Every push and PR via `.github/workflows/ci.yml`, with `-race` and coverage.

### Integration tests (LocalStack)

- **Scope**: AWS provider's state-bucket lifecycle and tofu invocation, against [LocalStack](https://localstack.cloud/).
- **Runner**: `make test-integration` (testcontainers-managed LocalStack; requires Docker).
- **Build tag**: `integration`. Unit-only runs (the default and what CI runs) exclude these via the absence of `-tags=integration`.
- **Where they run**: Locally, on demand. Not currently wired into CI.

### Deployment tests (real infrastructure)

- **Scope**: End-to-end `nic deploy` / `nic destroy` for every implemented provider: local (Kind), existing cluster (k3d on the runner), AWS, Azure, and Hetzner.
- **Runner**: `.github/workflows/deployment-tests.yml`, which builds `nic`, then runs the deploy action at `.github/actions/deploy` with the per-provider configs in `.github/fixtures/deploy/`. The action deploys, waits for the platform to converge, and destroys in a post step that runs even on failure or cancellation.
- **Where they run**: On demand via `workflow_dispatch` (pick one provider or `all`) and on every published release. The local provider also runs on PRs marked ready for review.
- **Cost control**: One run at a time per cloud provider (concurrency groups), Let's Encrypt staging certificates, and teardown in the action's post step with the deploy step time-boxed below the job timeout so destroy always has budget.

### Health tests (planned)

- **Status**: Not implemented. A future `nic health check` subcommand and a corresponding test harness are planned but no code exists today (no `cmd/nic/health.go`, no `tests/health/`, no scheduled workflow). When referenced elsewhere, treat as roadmap.

## 12.2 Test Coverage Targets

There are no enforced coverage thresholds in CI today. The Codecov upload in `.github/workflows/ci.yml` is informational only and is `continue-on-error: true`.

Coverage hygiene is enforced through review:

- New code added under `pkg/` should have unit tests, ideally table-driven.
- The interface-driven design (Go functions take interfaces, return concrete types - see [`AGENTS.md`](../../../AGENTS.md)) is what makes coverage feasible.

## 12.3 Test Infrastructure

| Need | Tool |
|------|------|
| AWS API mocking | LocalStack, managed by testcontainers (`make test-integration`) |
| Kubernetes object mocking | `k8s.io/client-go/kubernetes/fake` |
| Helm SDK mocking | The `Helm` interface in `pkg/helm` with fake implementations |
| Filesystem mocking | `github.com/spf13/afero` (used in `pkg/tofu` and elsewhere) |
| Local cluster for manual testing | Kind via `nic deploy -f examples/local-config.yaml` |

GCS mocking is not in scope while the GCP provider remains a stub. Azure is implemented (AKS via OpenTofu), but Azure integration-test coverage is not yet wired up; today the only integration suite is the AWS provider's (`pkg/providers/cluster/aws/integration_test.go`).

## 12.4 CI Pipeline

`.github/workflows/ci.yml` runs these jobs on every push and PR against `main` (job details live in the workflow file itself; this table describes intent so it does not rot with every YAML change):

| Job | What it does |
|------|--------------|
| `Lint` | `golangci-lint` (latest) |
| `Test` | `go mod download` + `verify`, unit tests with `-race` and coverage, informational Codecov upload (`continue-on-error: true`) |
| `Build` | `make build`, uploads the `nic` binary as a 1-day artifact |
| `Deploy` | downloads the `Build` artifact and runs the deploy action with its built-in default config: a local Kind cluster with an auto-created GitOps repo, deployed, waited on, and destroyed on the runner |
| `Workflow pins & release config` | `check-action-pins.sh` (every action SHA-pinned) + goreleaser config validation |
| `Vulnerabilities` | `govulncheck` gate |

Highlights:

- Go version tracks `go.mod` via `go-version-file`.
- Unit tests run with `-race` and coverage.
- All GitHub Actions are SHA-pinned, enforced by `check-action-pins.sh`.
- A `govulncheck` gate fails the build on known vulnerabilities.
- The LocalStack integration tests are still not wired into CI; the Kind-based `Deploy` job is the end-to-end smoke test.

Other workflows in `.github/workflows/`:

- `deployment-tests.yml` - deployment tests for all providers (see 12.1)
- `release.yml` - cuts releases via goreleaser
- `opentofu-lockfile-pr.yml` - keeps tofu lockfiles fresh
- `add-to-project.yaml` - GitHub Projects auto-add

### CI prerequisites (state outside this repository)

The deployment tests depend on setup that lives in repo settings and sibling repositories, not in this tree:

- **Scratch GitOps repository**: `nebari-dev/nic-ci-gitops`. Each provider job pushes manifests to its own branch (`local`, `aws`, `azure`, `hetzner`, `existing`) under `clusters/<project>-ci`, and `.github/actions/reset-gitops-branch` force-resets that branch to an empty commit before every run so NIC regenerates manifests from scratch instead of skipping bootstrap.
- **Repository secrets**: `NIC_CI_GITOPS_TOKEN` (Contents read/write on the scratch repo, used by every provider job) and `CLOUDFLARE_API_TOKEN` (manages records in the `nebari.dev` zone for the `*-ci.nebari.dev` test domains).
- **Environments**: `aws` (`AWS_ROLE_ARN`, assumed via OIDC, `us-west-2`), `azure` (`AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID`, via OIDC), `hetzner` (`HETZNER_TOKEN`). The cloud environments are where approval gates (required reviewers) belong, so that runs against real cloud accounts need a human click.
- **Certificates**: all cloud fixtures use the Let's Encrypt staging endpoint, so CI never consumes production ACME rate limits.

## 12.5 Local Development Loop

- `make build` - compile the binary
- `make test` - run unit tests
- `make test-race` - unit tests with `-race`
- `make test-coverage` - unit tests with coverage report
- `make test-integration` - integration tests against testcontainers-managed LocalStack (requires Docker)
- `make lint` - `golangci-lint run`
- `make check` - `fmt`, `vet`, `lint`, `test`
- `nic deploy -f examples/local-config.yaml` - end-to-end deploy onto a local Kind cluster. The local provider creates the Kind cluster (reusing it if present), then bootstraps ArgoCD and the foundational apps.
- `nic destroy -f examples/local-config.yaml` - delete the local Kind cluster

The local provider mounts the `file://` GitOps directory into the Kind node so the in-cluster ArgoCD can sync from a local filesystem. See `pkg/providers/cluster/local`.

## 12.6 What "Test Cases" Look Like

A few representative cases:

**Fresh AWS deploy (manual integration):**

- `nic deploy -f examples/aws-config.yaml`
- Expect: state bucket created, EKS cluster up with the configured `kubernetes_version` and `node_groups`, EFS volume mounted, ArgoCD running in `argocd` namespace, foundational apps syncing.
- Verify: `kubectl get nodes`, `kubectl get applications -n argocd`, the printed Argo CD and Keycloak access instructions.

**Local Kind deploy (manual):**

- `nic deploy -f examples/local-config.yaml`
- Expect: Kind cluster `my-nebari-local` (named after `project_name`) up, MetalLB syncing, gateway with an IP from the configured pool, foundational apps green.

**Dry-run (any provider):**

- `nic deploy -f config.yaml --dry-run`
- Expect: no state mutation, plan output streamed.

**Adoption of an existing cluster:**

- `nic deploy -f examples/existing-config.yaml`
- Expect: no infrastructure provisioning, just the GitOps bootstrap + foundational app rollout against the kubeconfig in the config.

## 12.7 Future Work

- Wire integration tests into CI (likely as a separate, slower workflow with a manual trigger).
- Run the deployment tests on a schedule (nightly or weekly) in addition to releases and manual dispatch.
- Implement the `nic health check` subcommand and a paired test harness.
- Add Hetzner-specific integration tests (LocalStack analogue does not exist; may require recorded HTTP fixtures against the Hetzner Cloud API).
