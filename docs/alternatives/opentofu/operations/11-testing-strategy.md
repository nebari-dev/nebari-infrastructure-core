# Testing Strategy

### 11.1 Testing Levels

**1. Unit Tests:**
- Configuration parsing
- Tfvars generation logic
- Backend configuration generation
- Helper functions
- **Target:** >80% code coverage
- **Run Frequency:** Every commit (pre-commit hook + CI)

**2. Integration Tests:**
- Terraform module validation (`terraform validate`)
- Terraform plan tests (no actual apply)
- Mock terraform-exec calls
- Kubernetes operations against kind clusters
- **Target:** All critical paths covered
- **Run Frequency:** Every PR (CI)

**3. Terraform Module Tests:**
- Use `terraform-test` or Terratest
- Validate module outputs
- Test module composition
- Ensure modules work with various input combinations
- **Target:** All modules tested
- **Run Frequency:** On module changes

**4. Provider Tests (Expensive):**
- Deploy real infrastructure to AWS/GCP/Azure
- Verify Kubernetes cluster functional
- Verify foundational software deployed
- Verify operator functional
- Tear down infrastructure
- **Target:** Nightly or on-demand
- **Run Frequency:** Nightly, release candidates

**5. End-to-End Tests:**
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
  - Terraform backend is configured (S3 + DynamoDB)
  - Terraform modules execute successfully
  - VPC is created with 3 AZs
  - EKS cluster is created (version 1.29)
  - 3 node pools are created (general, compute, gpu)
  - ArgoCD is deployed
  - All 9 foundational components are deployed
  - Nebari operator is deployed
  - Terraform state is saved to S3
  - Kubeconfig is available in Terraform outputs
  - All URLs are accessible (argocd, grafana, keycloak)
```

**Test Case 2: Idempotency**
```gherkin
Given a deployed Nebari platform
When I run `nic deploy -f config.yaml` again
Then:
  - Terraform plan shows no changes
  - No infrastructure modifications are made
  - Command completes in <2 minutes (plan only, no apply)
```

**Test Case 3: Add Node Pool**
```gherkin
Given a deployed Nebari platform
When I add a new node pool to config.yaml
And I run `nic deploy -f config.yaml`
Then:
  - Terraform plan shows only new node pool addition
  - New node pool is created
  - Existing node pools are unchanged
  - Terraform state is updated with new node pool
```

**Test Case 4: Drift Detection**
```gherkin
Given a deployed Nebari platform
When I manually delete a node pool via AWS console
And I run `nic status --check-drift`
Then:
  - NIC runs terraform plan
  - Terraform detects missing node pool
  - Drift report shows expected vs actual state
When I run `nic deploy`
Then:
  - Terraform recreates node pool
  - Drift is resolved
```

**Test Case 5: Destroy**
```gherkin
Given a deployed Nebari platform
When I run `nic destroy -f config.yaml`
Then:
  - Terraform destroy executes
  - All Terraform-managed resources are deleted
  - Terraform state is empty
  - No cloud resources remain (verified via cloud APIs)
```

### 11.3 Terraform Module Testing with Terratest

**Example:**
```go
// terraform/modules/aws/eks/eks_test.go
package test

import (
    "testing"

    "github.com/gruntwork-io/terratest/modules/terraform"
    "github.com/stretchr/testify/assert"
)

func TestAWSEKSModule(t *testing.T) {
    t.Parallel()

    terraformOptions := terraform.WithDefaultRetryableErrors(t, &terraform.Options{
        TerraformDir: "../",

        Vars: map[string]interface{}{
            "cluster_name":       "test-eks",
            "kubernetes_version": "1.29",
            "vpc_id":             "vpc-12345",
            "subnet_ids":         []string{"subnet-1", "subnet-2"},
            "node_pools": []map[string]interface{}{
                {
                    "name":          "general",
                    "instance_type": "m6i.2xlarge",
                    "min_size":      1,
                    "max_size":      3,
                },
            },
        },

        NoColor: true,
    })

    defer terraform.Destroy(t, terraformOptions)

    terraform.InitAndApply(t, terraformOptions)

    clusterEndpoint := terraform.Output(t, terraformOptions, "cluster_endpoint")
    assert.NotEmpty(t, clusterEndpoint)
}
```

### 11.4 Test Infrastructure

**Mock Services:**
- `terraform-exec` can be mocked for unit tests
- `localstack` for AWS API mocking (Terraform works with it)
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

  terraform-validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: 1.7.0
      - run: |
          cd terraform
          terraform init -backend=false
          terraform validate
          terraform fmt -check -recursive

  terraform-module-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - uses: hashicorp/setup-terraform@v3
      - run: go test ./terraform/modules/... -v

  provider-tests-aws:
    runs-on: ubuntu-latest
    if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - uses: hashicorp/setup-terraform@v3
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-west-2
      - run: go test ./test/e2e/aws -v -timeout 60m
```

---
