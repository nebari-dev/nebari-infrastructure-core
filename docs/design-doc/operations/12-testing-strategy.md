# Testing Strategy

## 12.1 Testing Levels

NIC has three testing levels today, plus one (health) that is planned but not yet implemented:

### Unit tests

- **Scope**: Pure Go packages under `pkg/` and `cmd/nic/`.
- **Runner**: `go test ./...` (or `make test` / `make test-unit`).
- **Conventions**: Table-driven tests (per [`CLAUDE.md`](../../../CLAUDE.md)). Interfaces are injected so concrete dependencies (AWS SDK, Helm, k8s client) can be mocked.
- **Where they run**: Every push and PR via `.github/workflows/ci.yml`, with `-race` and coverage.

### Integration tests (LocalStack)

- **Scope**: AWS provider's state-bucket lifecycle and tofu invocation, against [LocalStack](https://localstack.cloud/).
- **Runner**: `make test-integration` (testcontainers-managed LocalStack) or `make test-integration-local` (uses `docker-compose.test.yml`).
- **Build tag**: `integration`. Unit-only runs (the default and what CI runs) exclude these via the absence of `-tags=integration`.
- **Where they run**: Locally, on demand. Not currently wired into CI.

### Provider tests (real cloud)

- **Status**: Not yet wired up. The intent is a small set of expensive tests that deploy real infrastructure on AWS (and eventually Hetzner) to validate end-to-end provider behavior. These will live behind a separate build tag and run only when explicitly invoked (e.g., for release candidates).

### Health tests (planned)

- **Status**: Not implemented. A future `nic health check` subcommand and a corresponding test harness are planned but no code exists today (no `cmd/nic/health.go`, no `tests/health/`, no scheduled workflow). When referenced elsewhere, treat as roadmap.

## 12.2 Test Coverage Targets

There are no enforced coverage thresholds in CI today. The Codecov upload in `.github/workflows/ci.yml` is informational only and is `continue-on-error: true`.

Coverage hygiene is enforced through review:

- New code added under `pkg/` should have unit tests, ideally table-driven.
- The interface-driven design (Go functions take interfaces, return concrete types - see [`CLAUDE.md`](../../../CLAUDE.md)) is what makes coverage feasible.

## 12.3 Test Infrastructure

| Need | Tool |
|------|------|
| AWS API mocking | LocalStack via `docker-compose.test.yml` |
| Kubernetes object mocking | `k8s.io/client-go/kubernetes/fake` |
| Helm SDK mocking | The `Helm` interface in `pkg/helm` with fake implementations |
| Filesystem mocking | `github.com/spf13/afero` (used in `pkg/tofu` and elsewhere) |
| Local cluster for manual testing | Kind via `make localkind-up` |

GCS and Azure Blob mocking are not in scope while the GCP and Azure providers remain stubs.

## 12.4 CI Pipeline

The actual workflow at `.github/workflows/ci.yml`:

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25.1'
      - run: go mod download
      - run: go mod verify
      - uses: golangci/golangci-lint-action@v9
        with:
          version: latest
      - run: go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
      - uses: codecov/codecov-action@v4    # continue-on-error: true

  build:
    needs: test
    steps:
      - run: make build
      - run: ./nic --help || true
```

Highlights:

- Go 1.25.1.
- Unit tests run with `-race` and coverage.
- Lint via the latest `golangci-lint`.
- No integration-test job, no nightly schedule, no kind-based cluster spin-up.

Other workflows in `.github/workflows/`:

- `release.yml` - cuts releases via goreleaser
- `opentofu-lockfile-pr.yml` - keeps tofu lockfiles fresh
- `add-to-project.yaml` - GitHub Projects auto-add

## 12.5 Local Development Loop

- `make build` - compile the binary
- `make test` - run unit tests
- `make test-race` - unit tests with `-race`
- `make test-coverage` - unit tests with coverage report
- `make test-integration` / `make test-integration-local` - integration tests against LocalStack
- `make lint` - `golangci-lint run`
- `make check` - `fmt`, `vet`, `lint`, `test`
- `make localkind-up` - end-to-end deploy onto a local Kind cluster (uses `examples/local-config.yaml` by default; pass `LOCAL_CONFIG=...` to override)
- `make localkind-rebuild` - tear down and rebuild the local cluster

The Kind workflow mounts the `file://` GitOps directory into the cluster so the in-cluster ArgoCD can sync from a local filesystem. See `pkg/provider/local` and the relevant Makefile target.

## 12.6 What "Test Cases" Look Like

A few representative cases:

**Fresh AWS deploy (manual integration):**

- `nic deploy -f examples/aws-config.yaml`
- Expect: state bucket created, EKS cluster up with `kubernetes_version: "1.34"` and the configured `node_groups`, EFS volume mounted, ArgoCD running in `argocd` namespace, foundational apps syncing.
- Verify: `kubectl get nodes`, `kubectl get applications -n argocd`, the printed Argo CD and Keycloak access instructions.

**Local Kind deploy (manual):**

- `make localkind-up`
- Expect: Kind cluster `nebari-local` up, MetalLB syncing, gateway with an IP from the configured pool, foundational apps green.

**Dry-run (any provider):**

- `nic deploy -f config.yaml --dry-run`
- Expect: no state mutation, plan output streamed.

**Adoption of an existing cluster:**

- `nic deploy -f examples/existing-config.yaml`
- Expect: no infrastructure provisioning, just the GitOps bootstrap + foundational app rollout against the kubeconfig in the config.

## 12.7 Future Work

- Wire integration tests into CI (likely as a separate, slower workflow with a manual trigger).
- Add a `provider-tests` job on a schedule (nightly or weekly) that hits real cloud APIs.
- Implement the `nic health check` subcommand and a paired test harness.
- Add Hetzner-specific integration tests (LocalStack analogue does not exist; may require recorded HTTP fixtures against the Hetzner Cloud API).
