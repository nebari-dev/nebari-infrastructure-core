# ADR-0017: User Journey (E2E) Tests for Foundational Software

## Status

Proposed (2026-08-27)

Records the design for epic [#625](https://github.com/nebari-dev/nebari-infrastructure-core/issues/625), whose first slice is [#626](https://github.com/nebari-dev/nebari-infrastructure-core/issues/626). Fulfills the "integration tests" half of the [#429](https://github.com/nebari-dev/nebari-infrastructure-core/issues/429) placeholder, under the testing epic [#160](https://github.com/nebari-dev/nebari-infrastructure-core/issues/160).

## Date

2026-08-27

## Context

CI proves that `nic deploy` succeeds. It does not prove that the deployed platform works.

A cluster can finish deploying with every ArgoCD application reporting Synced and Healthy while a user cannot log in, cannot get durable storage, or is admitted to a UI they should be denied. [#100](https://github.com/nebari-dev/nebari-infrastructure-core/issues/100) ("End to End tests in CI") was closed by the `Deploy` job, which covers deployment success only. [#429](https://github.com/nebari-dev/nebari-infrastructure-core/issues/429) names the remaining gap directly: "make sure the deployed platform and its components behave as expected."

The foundational stack this must cover is substantial: cert-manager, Envoy Gateway and its routes, Keycloak with its CloudNativePG database, Longhorn and its backups, MetalLB, trust-manager, the nebari-operator, the landing page, the OpenTelemetry collector, and ArgoCD itself. Several of these fail in ways that are invisible to a health check: an OIDC client secret that has drifted ([#463](https://github.com/nebari-dev/nebari-infrastructure-core/issues/463), [#522](https://github.com/nebari-dev/nebari-infrastructure-core/issues/522)), a SecurityPolicy that admits users it should refuse, a MetalLB pool that fell back to unroutable addresses ([#612](https://github.com/nebari-dev/nebari-infrastructure-core/issues/612)), a collector silently dropping every span ([#466](https://github.com/nebari-dev/nebari-infrastructure-core/issues/466)).

The stated goal is a suite that verifies critical user journeys against **any deployed Nebari cluster**, not only one deployed from this checkout, with a shared library of reusable actions and tests written as human-readable stories. The model is [conda-store's `tests/user_journeys`](https://github.com/conda-incubator/conda-store/tree/main/conda-store-server/tests/user_journeys).

This forces the decisions recorded here: **what language and toolchain the suite is written in**, **what the suite is allowed to assume about its target cluster**, and **how much it is allowed to change**.

## Decision Drivers

- UI journeys are a requirement. OIDC login flows and UI authorization cannot be verified through the Kubernetes API.
- Playwright officially supports JavaScript/TypeScript, Python, Java and .NET. It does not support Go; only a community port exists.
- "Any deployed cluster" is the stated goal. Any input the suite requires beyond cluster access narrows the set of clusters it can run against.
- Assertions on declared state cannot prove a user can do anything. Only journeys that exercise the platform can.
- The suite must be safe to point at a real cluster, including one carrying real workloads.
- A test suite people learn to ignore is worse than no test suite. Flakiness is a correctness concern, not an annoyance.
- Nebari domains frequently do not resolve publicly. A kind cluster serves a MetalLB address on a container network under a domain no resolver knows.
- Adding a second toolchain to a Go repository carries real maintenance cost and must stay contained.
- Verifying real journeys requires privileged cluster access and handling live platform credentials. Wherever the suite runs becomes part of the platform's trust boundary, so credential handling and failure artifacts are design concerns, not implementation details.

## Considered Options

1. Go, using `client-go` and the community `playwright-go` port
2. Python and pytest, managed with pixi, using the official Kubernetes client and Playwright
3. Hybrid: Go for API-level journeys, TypeScript and Playwright for UI journeys

## Decision Outcome

Chosen option: **Option 2, a Python and pytest suite managed with pixi**, living in `tests/user_journeys/` with its own `pixi.toml` and committed `pixi.lock`. It is not part of `go test ./...` and runs via `make test-journeys`.

Playwright's official language support is the deciding factor: UI journeys are a requirement, and the suite belongs where the browser automation is first-class rather than community-maintained. Story-style tests also read better in pytest than in Go's testing package, which matters for a suite whose readability is part of its purpose. pixi resolves the entire suite from conda-forge (`pytest`, `kubernetes`, `requests`, `pytest-playwright`, `playwright-python`), aligns with the distribution direction of [#552](https://github.com/nebari-dev/nebari-infrastructure-core/issues/552), and pins an exact environment across CI and developer machines.

The manifest is a standalone `pixi.toml` rather than `pyproject.toml` with `[tool.pixi]`, because this is a test harness, not a distributable package. Browser binaries are not on conda-forge, so `playwright install chromium` stays a separate pixi task, and journeys needing no browser are marked so they can run without one.

The following decisions follow from the same drivers and are recorded here rather than as separate ADRs, since none stands alone.

**The Kubernetes API first; Playwright only where a browser is required.** Anything assertable through the Kubernetes API is tested with the Python client. Browser tests are slower, flakier and harder to debug, so they earn their place rather than being the default.

**A kubeconfig is the only required input.** The suite discovers everything else from the cluster: domain and gateway address from the `Gateway` in `envoy-gateway-system` and its LoadBalancer service, mirroring `pkg/endpoint`; Keycloak credentials from `keycloak-admin-credentials` and `nebari-realm-admin-credentials`; and which optional components are installed by querying ArgoCD, so journeys for absent components skip rather than fail. Shelling out to `nic outputs --format json --show-secrets` was considered and rejected: it would exercise the operator path and reuse an existing seam, but it requires the `nic` binary and a matching `config.yaml`, which defeats running against clusters this checkout did not deploy.

**Journeys mutate, inside scratch namespaces.** A `scratch_namespace` fixture creates `nebari-journey-<uuid8>`, labels it `nebari.dev/test-journey=true`, and deletes it on teardown including on failure, with a `--keep-namespace` debugging escape hatch and a session sweep of leftovers from crashed runs. Everything created is namespaced and labeled, so the blast radius is bounded and leftovers are identifiable.

**TLS is validated, never ignored.** Playwright offers no built-in DNS override, and its documentation marks custom Chromium flags as use-at-your-own-risk. The suite resolves the domain first: if public DNS already points at the gateway, nothing is overridden, so cloud clusters are tested exactly as a user reaches them. Only when resolution fails or points elsewhere does it map names, via a session-scoped `socket.getaddrinfo` patch for Python and `--host-resolver-rules` for Chromium. Both preserve real SNI and real certificate validation. Rather than setting `ignore_https_errors=True` and `verify=False`, the suite derives a trust anchor and validates against it. The anchor comes from the gateway's own TLS secret (`nebari-gateway-tls` in `envoy-gateway-system` by default, or the operator's secret under `certificate.existing_secret`), not from `nebari-trust-bundle`: that bundle is the egress trust store for TLS-inspecting proxies and does not contain the gateway's issuer. When the gateway certificate is publicly trusted, as under ACME, the system trust store is used unchanged and nothing is injected. This turns a workaround into an assertion: a journey completing over TLS has proven cert-manager issued a chain that actually verifies, against the anchor a client would have to trust in practice ([#447](https://github.com/nebari-dev/nebari-infrastructure-core/issues/447), [#490](https://github.com/nebari-dev/nebari-infrastructure-core/issues/490)).

**Constants are pinned by a contract test.** Namespace, secret and application names are Go constants in `pkg/argocd/foundational.go`. `nebari_journeys/constants.py` holds the single Python copy, and a Go test parses that file and fails the build when any value diverges. Drift cannot merge.

**Actions in the library, assertions in the tests.** `nebari_journeys/` exposes verbs (`ns.request_volume(...)`, `keycloak.create_user(...)`, `ui.login_via_keycloak(...)`); the `test_*.py` files hold the assertions. This is what keeps the journeys readable and the library reusable, and it is the split conda-store uses.

**The framework ships with two vertical slices, not alone**, so its conventions are set by journeys that actually ran: identity (realm configuration, ArgoCD login through Keycloak, Longhorn UI group authorization) and storage (data surviving pod replacement, replica health, backup configuration). A `test_smoke.py` asserting every foundational application is Synced and Healthy runs first, so other failures are interpretable: if smoke also fails the cluster is broken, if only a journey fails the feature is broken. Remaining components follow as siblings under #625 ([#627](https://github.com/nebari-dev/nebari-infrastructure-core/issues/627) through [#636](https://github.com/nebari-dev/nebari-infrastructure-core/issues/636)).

**The suite requires a privileged kubeconfig, and this is a deliberate trade.** Reading `keycloak-admin-credentials`, creating and deleting namespaces, and creating Keycloak users all require permissions close to cluster-admin. There is no version of "verify a user can actually log in and get storage" that runs from an unprivileged context. Operators must treat the suite's kubeconfig as a cluster-admin credential: scope it to a dedicated service account, never persist it in CI outside a secret, and understand that pointing the suite at production grants that runner administrative access. A least-privilege Role covering exactly the verbs the suite uses is worth deriving once the journeys stabilise, but is not a precondition for the first slice.

CI adds the suite to the existing `Deploy` job, which already stands up a real cluster, using a SHA-pinned `prefix-dev/setup-pixi` per `scripts/check-action-pins.sh`. Playwright traces, videos and screenshots upload as artifacts on failure.

**Failure artifacts must be redacted before upload.** Playwright traces capture DOM snapshots and network payloads, and the identity journeys type credentials into a login form: an unredacted trace from a failed login would publish the Keycloak password as a CI artifact. Journeys therefore use throwaway scratch users with generated passwords wherever the journey allows it, admin credentials are registered with Playwright's masking so they do not reach snapshots, and artifact retention is kept short. This constraint is binding on the implementation, not advisory.

### Consequences

**Good:**

- Failures are expressed as broken user journeys rather than unhealthy resources, which is both more actionable and closer to what users experience.
- The suite runs against any cluster an operator can reach with a kubeconfig, including clusters deployed from a different NIC version, making it usable as a production smoke test and not only a CI gate.
- Playwright is used where it is officially supported, so browser automation is not carried on a community port.
- `pixi.lock` makes the environment reproducible between CI and laptops, so a failure is a signal about the cluster rather than about the runner.
- Validating TLS against the gateway's own trust anchor converts what would have been a `verify=False` workaround into a real assertion about cert-manager, and exercises the same anchor a downstream client must trust.
- The constants contract test makes a whole class of silent drift unmergeable.

**Bad:**

- A second language and toolchain in a Go repository: another CI path, another dependency surface, and contributors who must be comfortable in both. Contained to one directory, one manifest, one Makefile target, with the Go build unaffected.
- The suite writes to the cluster it tests. Bounded by scratch namespaces and labels, but it is not a read-only observer, and pointing it at production is a deliberate act.
- It needs a near-cluster-admin kubeconfig and reads platform admin credentials out of the cluster. That is inherent to verifying real journeys rather than declared state, but it means the suite's credential is as sensitive as the platform's own, and wherever the suite runs becomes part of the platform's trust boundary.
- Browser failure artifacts are a credential-leak vector, requiring masking and short retention as a standing implementation constraint rather than a one-time review item.
- `--host-resolver-rules` is an unsupported Chromium flag. It is only used when public DNS does not already resolve, so a break degrades local and kind runs rather than cloud runs; the fallback is a local proxy via Playwright's supported `proxy` option.
- Constants are duplicated between Go and Python. The contract test makes the duplication safe but does not remove it.
- Browser journeys are inherently slower and more failure-prone than API assertions, and will need active flakiness management.

## Options Detail

### Option 1: Go, using client-go and playwright-go

A single toolchain, reusing `client-go` (already a dependency) and NIC's own packages such as `pkg/endpoint` and the `pkg/argocd` constants directly.

**Pros:**

- One language, one build, one CI path; no second dependency surface.
- Direct reuse of NIC's packages, eliminating the constants duplication entirely.
- Contributors already work in Go.

**Cons:**

- Playwright does not officially support Go. UI journeys, which are a requirement, would rest on a community port with no upstream guarantee.
- Go's testing package produces markedly less readable story-style tests, working against a stated goal of the suite.
- Diverges from the conda-store precedent the team asked to follow.

### Option 2: Python and pytest, managed with pixi (chosen)

**Pros:**

- Playwright is officially supported, with a maintained pytest plugin.
- pytest reads as prose, which is the point of journey tests.
- The whole suite resolves from conda-forge, and pixi pins it reproducibly.
- Matches conda-store's structure directly, so the precedent transfers rather than being reinterpreted.

**Cons:**

- A second toolchain in a Go repository.
- Constants must be duplicated across the language boundary, requiring the contract test.
- Contributors adding a journey work in a different language from the code under test.

### Option 3: Hybrid, Go for API journeys and TypeScript for UI

**Pros:**

- Each half uses a first-class toolchain: `client-go` for the API, Playwright's native language for the browser.
- API journeys reuse NIC's packages with no duplication.

**Cons:**

- Two suites, two shared libraries, two CI paths: strictly more maintenance than either single-language option.
- Journeys that span both, such as "create a user via the API, then log in as them," are the most valuable ones and become the most awkward to express.
- Three languages in the repository overall.

## Out of Scope

- **`nic` CLI behavior**: deploy idempotency, `--regen-apps` safety, destroy. These require a `config.yaml`, which breaks the kubeconfig-only rule, and belong with the deployment tests.
- **A multi-cloud CI matrix.** Running the suite against AWS, Azure and GCP clusters is tracked under [#160](https://github.com/nebari-dev/nebari-infrastructure-core/issues/160).
- **Unit and integration coverage of the Go packages**, which is unchanged and stays in `go test`.

## Links

- [#625 epic: user journey tests for foundational software](https://github.com/nebari-dev/nebari-infrastructure-core/issues/625)
- [#626 framework plus identity and storage slices](https://github.com/nebari-dev/nebari-infrastructure-core/issues/626)
- [#429 Testing placeholder](https://github.com/nebari-dev/nebari-infrastructure-core/issues/429)
- [#160 Epic: Testing & CI Infrastructure](https://github.com/nebari-dev/nebari-infrastructure-core/issues/160)
- [#100 End to End tests in CI](https://github.com/nebari-dev/nebari-infrastructure-core/issues/100) (closed; deployment success only)
- [conda-store user journey tests](https://github.com/conda-incubator/conda-store/tree/main/conda-store-server/tests/user_journeys) (prior art)
- [Playwright supported languages](https://playwright.dev/docs/languages)
- [ADR-0010](0010-high-security-mode.md) (hardened clusters may restrict what the suite can exercise)
