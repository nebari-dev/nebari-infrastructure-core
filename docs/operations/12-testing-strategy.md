# Testing Strategy

### 11.1 Testing Levels

**1. Unit Tests:**

- Provider implementations
- Configuration parsing
- State management (read/write/lock/unlock)
- Reconciliation logic
- Drift detection
- **Run Frequency:** Every commit (pre-commit hook + CI)

**2. Integration Tests:**

- Provider operations against mock cloud APIs
Seems like a bunch of extra effort to mock 3 (and possibly more eventually) cloud APIs
- State backend operations (local, mock S3/GCS/Azure)
- Kubernetes operations against kind clusters
- ArgoCD application deployment
- **Target:** All critical paths covered
- **Run Frequency:** Every PR (CI)

**3. Provider Tests (Expensive):**

- Deploy real infrastructure to AWS/GCP/Azure
- Verify Kubernetes cluster functional
- Verify foundational software deployed
- Verify operator functional
- Tear down infrastructure
- **Target:** Nightly or on-demand
- **Run Frequency:** Nightly, release candidates

**4. Black Box Health Tests:**

- Verify health of any deployed cluster (production, staging, dev)
- Test foundational software availability and functionality
- Validate authentication, observability, routing, TLS
- Provider-agnostic: works against AWS, GCP, Azure, Local deployments
- **Target:** Verify cluster health after any deployment
- **Run Frequency:** Release candidates, after deployments, scheduled daily, on-demand

### 11.2 Critical Test Cases

**Test Case 1: Fresh Deployment (AWS)**

```gherkin
Given a valid config.yaml for AWS
When I run `nic deploy -f config.yaml`
Then:
  - VPC is created with 3 AZs
  - EKS cluster is created (version 32)
  - 3 node pools are created (general, compute, gpu)
  - ArgoCD is deployed
  - All 9 foundational components are deployed
  - Nebari operator is deployed
  - Kubeconfig is saved
  - All URLs are accessible (argocd, grafana, keycloak)
```

**Test Case 2: Idempotency**

```gherkin
Given a deployed Nebari platform
When I run `nic deploy -f config.yaml` again
Then:
  - No infrastructure changes are made
  - Command completes in <2 minutes (only queries, no creates)
```

**Test Case 3: Add Node Pool**

```gherkin
Given a deployed Nebari platform
When I add a new node pool to config.yaml
And I run `nic deploy -f config.yaml`
Then:
  - New node pool is created
  - Existing node pools are unchanged
  - Kubernetes cluster detects new nodes
```

**Test Case 4: NebariApplication Integration**

```gherkin
Given a deployed Nebari platform
When I create a NebariApplication CRD for JupyterHub
Then:
  - OAuth client is created in Keycloak
  - HTTPRoute is created in Envoy Gateway
  - Certificate is provisioned by cert-manager
  - ServiceMonitor is created
  - Grafana dashboards are provisioned
  - Status.URL is set to https://jupyter.example.com
  - Status.Phase is "Ready"
```

**Test Case 5: Drift Detection**

```gherkin
Given a deployed Nebari platform
When I manually delete a node pool via AWS console
And I run `nic status --check-drift`
Then:
  - Drift is detected for node pool
  - Report shows expected vs actual state
When I run `nic deploy`
Then:
  - Node pool is recreated
  - Drift is resolved
```

**Test Case 6: Destroy**

```gherkin
Given a deployed Nebari platform
When I run `nic destroy -f config.yaml`
Then:
  - All ArgoCD applications are deleted
  - Kubernetes cluster is deleted
  - Node pools are deleted
  - VPC is deleted
  - No cloud resources remain (verified via cloud APIs)
```

### 11.3 Test Infrastructure

**Mock Services:**

- `moto` for AWS API mocking
- `fake-gcs-server` for GCS mocking
- `azurite` for Azure Blob mocking
- `kind` for Kubernetes testing

**CI/CD Pipeline:**

```yaml
name: CI

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go test ./... -v -cover

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: |
          kind create cluster
          go test ./... -tags=integration -v

  provider-tests-aws:
    runs-on: ubuntu-latest
    if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-west-2
      - run: go test ./pkg/provider/aws -tags=provider -v
```

### 11.4 Black Box Health Tests

Black box health tests verify the health and functionality of any deployed Nebari cluster without knowledge of the underlying infrastructure provider or deployment method. These tests can be run against production, staging, or development environments to validate cluster health.

**Design Principles:**

- **Provider-agnostic**: Works against AWS, GCP, Azure, and local deployments
- **Deployment-agnostic**: Works regardless of how cluster was deployed (NIC, manual, other tools)
- **Non-destructive**: Read-only operations, safe to run against production
- **Fast**: Complete suite runs in <5 minutes
- **Actionable**: Clear pass/fail criteria with diagnostic information

#### Test Suite Structure

```
tests/health/
├── cluster/           # Kubernetes cluster health tests
├── foundational/      # Foundational software health tests
├── integration/       # Cross-component integration tests
├── performance/       # Basic performance/latency tests
└── security/          # Security posture validation
```

#### Test Case Categories

**Category 1: Kubernetes Cluster Health**

```gherkin
Scenario: Kubernetes API is accessible
  Given I have kubeconfig for the cluster
  When I query the Kubernetes API server
  Then the API responds successfully
  And the cluster version is as expected
  And all control plane components are healthy

Scenario: All nodes are ready
  When I list all nodes in the cluster
  Then all nodes have status "Ready"
  And no nodes have memory pressure
  And no nodes have disk pressure
  And no nodes have PID pressure

Scenario: Critical system pods are running
  When I check pods in kube-system namespace
  Then all kube-proxy pods are Running
  And all coredns pods are Running
  And all CNI pods are Running (if applicable)

Scenario: Node pools match configuration
  When I list all nodes by labels
  Then I see nodes for each expected node group/pool
  And node counts are within min/max ranges
  And nodes have correct taints and labels
```

**Category 2: ArgoCD Health**

```gherkin
Scenario: ArgoCD is accessible
  Given the cluster domain is configured
  When I access https://argocd.<domain>
  Then I receive a valid HTTPS response
  And the TLS certificate is valid
  And the ArgoCD login page is displayed

Scenario: ArgoCD applications are healthy
  When I query ArgoCD API for all applications
  Then all applications have sync status "Synced"
  And all applications have health status "Healthy"
  And no applications are in "Degraded" state
  And no applications are in "Unknown" state

Scenario: ArgoCD can access Git repository
  When I trigger a manual sync of any application
  Then ArgoCD successfully fetches from Git repository
  And the sync completes without errors
```

**Category 3: Keycloak (Authentication) Health**

```gherkin
Scenario: Keycloak is accessible
  When I access https://keycloak.<domain>
  Then I receive a valid HTTPS response
  And the TLS certificate is valid
  And the Keycloak login page is displayed

Scenario: Keycloak master realm is accessible
  When I query Keycloak API for realm information
  Then the master realm is available
  And the Nebari realm is available (if configured)

Scenario: OAuth2 endpoints are functional
  When I query /.well-known/openid-configuration endpoint
  Then I receive valid OpenID Connect metadata
  And authorization_endpoint is accessible
  And token_endpoint is accessible
  And userinfo_endpoint is accessible

Scenario: Keycloak can issue tokens
  Given valid Keycloak credentials
  When I request an access token via client credentials flow
  Then I receive a valid JWT token
  And the token can be verified with JWKS endpoint
```

**Category 4: Envoy Gateway (Ingress) Health**

```gherkin
Scenario: Envoy Gateway is running
  When I check the envoy-gateway-system namespace
  Then envoy-gateway controller pod is Running
  And envoy proxy pods are Running
  And all pods are Ready

Scenario: HTTPRoutes are configured
  When I list all HTTPRoute resources
  Then all expected routes are present
  And all routes have "Accepted" status
  And all routes are attached to Gateway

Scenario: TLS certificates are valid
  When I access each foundational software endpoint via HTTPS
  Then each endpoint has a valid TLS certificate
  And certificates are issued by expected CA (Let's Encrypt)
  And certificates are not expired
  And certificates are not expiring within 30 days

Scenario: HTTP to HTTPS redirect works
  When I access http://<service>.<domain>
  Then I receive a 301 or 302 redirect
  And the redirect location is https://<service>.<domain>
```

**Category 5: cert-manager Health**

```gherkin
Scenario: cert-manager is running
  When I check the cert-manager namespace
  Then cert-manager controller pod is Running
  And cert-manager webhook pod is Running
  And cert-manager cainjector pod is Running
  And all pods are Ready

Scenario: ClusterIssuers are ready
  When I list all ClusterIssuers
  Then letsencrypt-prod ClusterIssuer exists
  And letsencrypt-staging ClusterIssuer exists (if configured)
  And all ClusterIssuers have status "Ready"

Scenario: Certificates are valid
  When I list all Certificate resources
  Then all certificates have status "Ready"
  And no certificates have status "Failed"
  And all certificates are not expired
  And all certificates have valid secrets
```

**Category 6: Observability Stack (LGTM) Health**

```gherkin
Scenario: Grafana is accessible
  When I access https://grafana.<domain>
  Then I receive a valid HTTPS response
  And the Grafana login page is displayed
  And I can authenticate via OAuth (Keycloak)

Scenario: Grafana has data sources configured
  Given I am authenticated to Grafana API
  When I query /api/datasources
  Then Loki data source is configured and healthy
  And Mimir (Prometheus) data source is configured and healthy
  And Tempo data source is configured and healthy

Scenario: Loki is ingesting logs
  When I query Loki for recent logs
  Then I receive log entries from the last 5 minutes
  And logs include entries from multiple namespaces
  And log ingestion rate is > 0

Scenario: Mimir is scraping metrics
  When I query Mimir for up metric
  Then I receive data points from the last 1 minute
  And multiple targets are reporting up=1
  And scrape success rate is > 95%

Scenario: Tempo is ingesting traces
  When I query Tempo for recent traces
  Then I receive traces from the last 5 minutes
  And traces include spans from multiple services

Scenario: OpenTelemetry Collector is running
  When I check the opentelemetry-collector pods
  Then all collector pods are Running
  And collector is receiving telemetry data
  And collector is exporting to Loki, Mimir, and Tempo
```

**Category 7: Nebari Operator Health**

```gherkin
Scenario: Nebari Operator is running
  When I check the nebari-operator-system namespace
  Then operator controller pod is Running
  And operator webhook pod is Running (if applicable)
  And all pods are Ready

Scenario: CRDs are installed
  When I list CustomResourceDefinitions
  Then NebariApplication CRD exists
  And CRD has expected version and schema
  And CRD is established and accepted

Scenario: Operator can reconcile resources
  When I create a test NebariApplication
  Then the operator reconciles the resource
  And status is updated with progress
  And the application becomes Ready
  When I delete the test NebariApplication
  Then the operator cleans up all created resources
```

**Category 8: Cross-Component Integration**

```gherkin
Scenario: OAuth integration works end-to-end
  When I access Grafana without authentication
  Then I am redirected to Keycloak login
  When I authenticate with valid credentials
  Then I am redirected back to Grafana
  And I am successfully logged in
  And my user identity is from Keycloak

Scenario: Monitoring stack observes all components
  When I query Mimir for component metrics
  Then I see metrics for ArgoCD
  And I see metrics for Keycloak
  And I see metrics for Envoy Gateway
  And I see metrics for cert-manager
  And I see metrics for Nebari Operator

Scenario: Logs are aggregated from all components
  When I query Loki with no namespace filter
  Then I see logs from argocd namespace
  And I see logs from keycloak namespace
  And I see logs from envoy-gateway-system namespace
  And I see logs from cert-manager namespace
  And I see logs from observability stack namespaces

Scenario: Distributed tracing captures cross-service requests
  When I make a request that spans multiple services
  Then I can see the full trace in Tempo
  And trace includes spans from ingress (Envoy)
  And trace includes spans from application
  And trace shows service dependencies
```

**Category 9: Performance and Latency**

```gherkin
Scenario: API response times are acceptable
  When I query Kubernetes API
  Then response time is < 500ms
  When I query Grafana API
  Then response time is < 1000ms
  When I query Keycloak API
  Then response time is < 1000ms

Scenario: DNS resolution works
  When I resolve service.<namespace>.svc.cluster.local
  Then DNS resolution succeeds in < 100ms
  When I resolve <service>.<domain>
  Then DNS resolution succeeds in < 200ms

Scenario: Prometheus query performance
  When I run a simple PromQL query
  Then query executes in < 2 seconds
  When I run a complex PromQL query (aggregation)
  Then query executes in < 10 seconds
```

**Category 10: Security Posture**

```gherkin
Scenario: Network policies are enforced
  When I check NetworkPolicy resources
  Then network policies exist for sensitive namespaces
  And policies restrict inter-namespace traffic appropriately

Scenario: RBAC is configured
  When I check ClusterRoles and Roles
  Then service accounts have minimal required permissions
  And no service account has cluster-admin unnecessarily
  And user access is role-based

Scenario: Secrets are encrypted
  When I check secret encryption configuration
  Then secrets are encrypted at rest (cloud provider KMS)
  And secrets are not stored in plain text in etcd

Scenario: Pod security standards are enforced
  When I check PodSecurityPolicy or PodSecurity admission
  Then restricted or baseline standards are enforced
  And privileged containers are only in system namespaces
```

#### Implementation

**Test Execution Tool:**

```bash
# Run all health tests
nic health check --kubeconfig=~/.kube/config

# Run specific category
nic health check --category=foundational

# Run against specific domain
nic health check --domain=nebari.example.com

# Output formats
nic health check --format=json
nic health check --format=junit  # For CI/CD integration

# Example output:
# ✅ Cluster Health (5/5 passed)
# ✅ ArgoCD (3/3 passed)
# ✅ Keycloak (4/4 passed)
# ✅ Envoy Gateway (4/4 passed)
# ✅ cert-manager (3/3 passed)
# ✅ Observability (7/7 passed)
# ✅ Nebari Operator (3/3 passed)
# ✅ Integration (4/4 passed)
# ⚠️  Performance (2/3 passed, 1 warning)
# ✅ Security (4/4 passed)
#
# Overall: 39/40 tests passed (97.5%)
# Duration: 3m 42s
```

**Test Configuration:**

```yaml
# health-test-config.yaml
cluster:
  kubeconfig: ~/.kube/config
  context: nebari-prod # Optional, uses current context if not specified

domain: nebari.example.com # Used for HTTPS endpoint checks

thresholds:
  api_latency_ms: 500
  query_latency_ms: 2000
  certificate_expiry_days: 30

skip_tests:
  - performance.complex-query # Skip specific tests if needed

authentication:
  keycloak:
    client_id: health-check-client
    client_secret_env: HEALTH_CHECK_CLIENT_SECRET
```

**CI/CD Integration:**

```yaml
# .github/workflows/health-check.yaml
name: Daily Health Check

on:
  schedule:
    - cron: "0 8 * * *" # Daily at 8 AM UTC
  workflow_dispatch:

jobs:
  health-check-production:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Configure kubectl
        run: |
          echo "${{ secrets.PROD_KUBECONFIG }}" > /tmp/kubeconfig
          export KUBECONFIG=/tmp/kubeconfig

      - name: Run health checks
        run: |
          nic health check --domain=nebari.company.com --format=junit > health-results.xml

      - name: Upload results
        uses: actions/upload-artifact@v4
        with:
          name: health-check-results
          path: health-results.xml

      - name: Notify on failure
        if: failure()
        uses: slackapi/slack-github-action@v1
        with:
          webhook-url: ${{ secrets.SLACK_WEBHOOK }}
          payload: |
            {
              "text": "🚨 Production health check failed",
              "blocks": [
                {
                  "type": "section",
                  "text": {
                    "type": "mrkdwn",
                    "text": "Production cluster health check failed. Review results in Actions."
                  }
                }
              ]
            }
```

**Benefits of Black Box Health Testing:**

1. **Post-Deployment Verification**: Validate cluster is healthy after any deployment
2. **Continuous Monitoring**: Run daily/hourly to catch drift or degradation
3. **Incident Response**: Run on-demand to quickly assess cluster health during incidents
4. **Provider-Agnostic**: Same tests work on AWS, GCP, Azure, and local clusters
5. **Regression Detection**: Catch issues introduced by configuration changes or upgrades
6. **Compliance**: Document cluster health for audits and SLAs
7. **Onboarding**: New team members can validate their dev environment setup
8. **Pre-Production Gates**: Require health checks to pass before promoting to production

---
