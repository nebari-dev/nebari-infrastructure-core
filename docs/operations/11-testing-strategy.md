# Testing Strategy

### 11.1 Testing Levels

**1. Unit Tests:**
- Provider implementations
- Configuration parsing
- State management (read/write/lock/unlock)
- Reconciliation logic
- Drift detection
- **Target:** >80% code coverage
- **Run Frequency:** Every commit (pre-commit hook + CI)

**2. Integration Tests:**
- Provider operations against mock cloud APIs
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

**4. End-to-End Tests:**
- Full platform deployment (AWS, GCP, Azure, Local)
- Deploy sample NebariApplication
- Verify auth, o11y, routing work end-to-end
- **Target:** Pre-release validation
- **Run Frequency:** Release candidates, major releases

### 11.2 Critical Test Cases

**Test Case 1: Fresh Deployment (AWS)**
```gherkin
Given a valid config.yaml for AWS
When I run `nic deploy -f config.yaml`
Then:
  - VPC is created with 3 AZs
  - EKS cluster is created (version 1.29)
  - 3 node pools are created (general, compute, gpu)
  - ArgoCD is deployed
  - All 9 foundational components are deployed
  - Nebari operator is deployed
  - State file is created in S3
  - Kubeconfig is saved
  - All URLs are accessible (argocd, grafana, keycloak)
```

**Test Case 2: Idempotency**
```gherkin
Given a deployed Nebari platform
When I run `nic deploy -f config.yaml` again
Then:
  - No infrastructure changes are made
  - State file is unchanged
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
  - State file is updated with new node pool
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
  - State file is deleted
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
          go-version: '1.22'
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

---
