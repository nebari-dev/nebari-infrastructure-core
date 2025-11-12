# Future Enhancements

This document provides detailed specifications for future enhancements planned for NIC.

## 1. Git Repository Provisioning & CI/CD Automation

### 1.1 Overview

Enable NIC to automatically provision Git repositories and configure CI/CD workflows for infrastructure automation, providing a complete GitOps experience out of the box.

### 1.2 User Experience

```bash
# Initialize new NIC deployment with Git automation
nic init --provider github --org nebari-team --repo production-platform

# Output:
# ✓ Created GitHub repository: nebari-team/production-platform
# ✓ Initialized config.yaml
# ✓ Generated .github/workflows/nic-ci.yml
# ✓ Generated .github/workflows/nic-deploy.yml
# ✓ Configured branch protection for main
# ✓ Added deployment secrets to repository
# ✓ Created initial commit and pushed to main
#
# Next steps:
#   1. Review and edit config.yaml
#   2. Commit changes: git add . && git commit -m "Configure platform"
#   3. Push to GitHub: git push origin main
#   4. CI/CD will automatically validate and deploy changes
```

### 1.3 Supported Providers

| Provider | Priority | Features |
|----------|----------|----------|
| **GitHub** | ✅ High | Actions workflows, branch protection, deployment environments, OIDC auth |
| **GitLab** | ✅ High | CI/CD pipelines, protected branches, deployment approval gates |
| **Gitea** | 🔄 Medium | Self-hosted, Actions-compatible, basic workflows |
| **Bitbucket** | ❓ Low | TBD based on demand |

### 1.4 Generated CI/CD Workflows

#### Pull Request Validation Workflow

**Triggers:** Pull request to main branch

**Steps:**
1. Checkout code
2. Install NIC binary
3. Run `nic validate -f config.yaml`
4. Run `nic plan -f config.yaml` (dry-run, show changes)
5. Post plan output as PR comment
6. Run security scan (checkov/tfsec equivalent for Go/YAML)
7. Run cost estimation (Infracost-style)

#### Deployment Workflow

**Triggers:** Push to main branch (or manual approval)

**Steps:**
1. Checkout code
2. Install NIC binary
3. Run `nic validate -f config.yaml`
4. Require manual approval (for production)
5. Run `nic deploy -f config.yaml`
6. Run smoke tests (cluster health, foundational software status)
7. Post deployment summary as GitHub issue/comment
8. Export deployment metrics to observability platform

#### Drift Detection Workflow

**Triggers:** Scheduled (daily at 6 AM UTC)

**Steps:**
1. Run `nic status -f config.yaml`
2. Compare actual vs desired state
3. Create GitHub issue if drift detected
4. Notify team (Slack/email integration)

### 1.5 Configuration

```yaml
# config.yaml
project_name: my-platform
provider: aws

# Git automation configuration
git_automation:
  enabled: true
  provider: github
  repository:
    org: nebari-team
    name: production-platform
    visibility: private

  branch_protection:
    require_reviews: 2
    require_status_checks: true
    enforce_admins: false

  workflows:
    validation:
      enabled: true
      on_pull_request: true

    deployment:
      enabled: true
      on_push_to_main: true
      require_approval: true
      approval_environment: production

    drift_detection:
      enabled: true
      schedule: "0 6 * * *"  # Daily at 6 AM UTC
      notify:
        - type: slack
          webhook_url_secret: SLACK_WEBHOOK
        - type: github_issue
          labels: [drift-detected, infrastructure]

  secrets:
    # Automatically configure these secrets in GitHub/GitLab
    - AWS_ACCESS_KEY_ID
    - AWS_SECRET_ACCESS_KEY
    - GCP_SERVICE_ACCOUNT_KEY
    - AZURE_CLIENT_SECRET
```

### 1.6 Security Considerations

- **OIDC Authentication:** Use GitHub/GitLab OIDC for cloud provider authentication (no long-lived secrets)
- **Secret Management:** Integrate with external secret stores (AWS Secrets Manager, HashiCorp Vault)
- **Audit Logging:** All CI/CD actions logged to LGTM stack
- **Least Privilege:** Generated IAM roles with minimal required permissions

---

## 2. Software Stack Specification

### 2.1 Overview

Enable users to declaratively specify complete software stacks (databases, message queues, caching layers, applications) to deploy alongside NIC's foundational software, providing a "full platform in one config" experience.

### 2.2 User Experience

```yaml
# config.yaml
project_name: data-science-platform
provider: aws

# Foundational software (managed by NIC)
foundational_software:
  keycloak: true
  monitoring: true
  envoy_gateway: true

# Application stacks (managed by NIC via ArgoCD)
application_stacks:
  # Database stack
  - name: postgresql
    enabled: true
    chart:
      repository: https://charts.bitnami.com/bitnami
      name: postgresql
      version: 13.2.0
    values:
      auth:
        database: nebari
        postgresPassword:
          secretRef: postgres-credentials
      primary:
        resources:
          requests:
            memory: 4Gi
            cpu: 2
    integration:
      keycloak:
        create_client: true
        client_name: postgresql-admin
      grafana:
        import_dashboards: true

  # Caching stack
  - name: redis
    enabled: true
    chart:
      repository: https://charts.bitnami.com/bitnami
      name: redis
      version: 18.1.0
    values:
      architecture: standalone
      auth:
        password:
          secretRef: redis-credentials

  # Object storage
  - name: minio
    enabled: true
    chart:
      repository: https://charts.min.io/
      name: minio
      version: 5.0.14
    values:
      mode: distributed
      replicas: 4
      persistence:
        size: 500Gi
    integration:
      keycloak:
        create_client: true
      envoy_gateway:
        create_route: true
        hostname: minio.nebari.example.com

  # Data science application
  - name: jupyterhub
    enabled: true
    chart:
      repository: https://jupyterhub.github.io/helm-chart/
      name: jupyterhub
      version: 3.1.0
    values:
      hub:
        config:
          Authenticator:
            admin_users: [admin]
      proxy:
        service:
          type: ClusterIP
    integration:
      keycloak:
        enabled: true
        client_id: jupyterhub
      postgresql:
        enabled: true
        database: jupyterhub
      envoy_gateway:
        create_route: true
        hostname: jupyter.nebari.example.com
        tls: true
      grafana:
        create_dashboard: true

# Stack templates (pre-configured bundles)
stack_templates:
  - name: data-science-complete
    enabled: true
    includes:
      - postgresql
      - redis
      - minio
      - jupyterhub
      - dask-gateway
      - conda-store
```

### 2.3 Stack Templates

NIC will provide pre-built stack templates for common use cases:

#### Data Science Stack
```yaml
stack_templates:
  - name: data-science-complete
    description: "Complete data science platform with Jupyter, Dask, conda-store"
    includes:
      - postgresql (database)
      - redis (caching)
      - minio (object storage)
      - jupyterhub (notebooks)
      - dask-gateway (distributed compute)
      - conda-store (environment management)
```

#### ML Platform Stack
```yaml
stack_templates:
  - name: ml-platform
    description: "Machine learning platform with MLflow, Kubeflow, model registry"
    includes:
      - postgresql (metadata store)
      - minio (artifact storage)
      - mlflow (experiment tracking)
      - kubeflow-pipelines (ML workflows)
      - seldon-core (model serving)
      - feast (feature store)
```

#### Web Application Stack
```yaml
stack_templates:
  - name: web-platform
    description: "Web application platform with database, caching, message queue"
    includes:
      - postgresql (database)
      - redis (caching)
      - rabbitmq (message queue)
      - minio (object storage)
      - cert-manager (TLS)
```

### 2.4 Automatic Integration

NIC's Nebari Operator will automatically handle integration between stacks and foundational software:

| Integration | Automatic Configuration |
|-------------|-------------------------|
| **Keycloak** | OAuth2 clients for each app, OIDC configuration, user sync |
| **Envoy Gateway** | HTTPRoute creation, TLS termination, SecurityPolicy for OAuth |
| **Grafana** | Import app-specific dashboards, configure data sources |
| **OpenTelemetry** | ServiceMonitor creation, trace exporters, metrics scraping |
| **cert-manager** | Certificate requests for app domains |
| **PostgreSQL** | Database creation, user provisioning, connection secrets |

### 2.5 Repository Structure

```
nebari-deployment/
├── config.yaml                 # Main platform configuration
├── stacks/
│   ├── postgresql/
│   │   ├── values.yaml                # PostgreSQL Helm values
│   │   └── backup-policy.yaml         # Backup configuration
│   ├── jupyterhub/
│   │   ├── values.yaml                # JupyterHub Helm values
│   │   ├── profiles.yaml              # User profiles
│   │   └── spawner-config.yaml        # Spawner options
│   └── dask/
│       ├── values.yaml                # Dask Gateway Helm values
│       └── worker-templates.yaml      # Worker configurations
├── policies/
│   ├── network-policies.yaml          # Kubernetes NetworkPolicies
│   ├── opa-policies/                  # OPA/Gatekeeper policies
│   │   ├── allowed-repos.rego
│   │   └── resource-limits.rego
│   └── rbac/
│       ├── roles.yaml
│       └── rolebindings.yaml
├── secrets/
│   ├── .gitignore                     # Never commit secrets!
│   ├── external-secrets.yaml          # ExternalSecrets references
│   └── sealed-secrets.yaml            # Sealed secrets (encrypted)
└── .github/workflows/
    ├── nic-validate.yml               # Auto-generated by NIC
    ├── nic-deploy.yml                 # Auto-generated by NIC
    └── drift-detection.yml            # Auto-generated by NIC
```

### 2.6 Stack Lifecycle Management

```bash
# Add a new stack to existing deployment
nic stack add jupyterhub --template data-science

# Update stack configuration
nic stack update jupyterhub --set replicas=5

# Remove stack from deployment
nic stack remove jupyterhub

# List available stack templates
nic stack templates list

# Show stack status
nic stack status --all
```

### 2.7 Integration with ArgoCD

Stacks will be deployed as ArgoCD Applications with proper dependencies:

```yaml
# Generated by NIC
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: postgresql-stack
  namespace: argocd
spec:
  project: nebari-stacks
  source:
    repoURL: https://charts.bitnami.com/bitnami
    chart: postgresql
    targetRevision: 13.2.0
    helm:
      valuesObject:
        # Merged from config.yaml + stack defaults
  destination:
    server: https://kubernetes.default.svc
    namespace: data-stack
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
  # Dependency on foundational software
  info:
    - name: depends-on
      value: keycloak,grafana,cert-manager
```

---

## 3. Stack Marketplace & Community

### 3.1 Overview

Create a community-driven marketplace for sharing stack configurations, templates, and best practices.

### 3.2 Features

- **Verified Stacks:** Curated, tested stack configurations
- **Community Stacks:** User-contributed configurations
- **Version Compatibility:** Track compatibility with NIC versions
- **Security Scanning:** Automated CVE scanning of stack components
- **Usage Analytics:** Track stack popularity and adoption

### 3.3 Implementation

GitHub-based registry similar to Helm Hub:

```
nebari-stacks/
├── data-science/
│   ├── jupyterhub/
│   │   ├── stack.yaml
│   │   ├── README.md
│   │   └── examples/
│   └── dask/
│       ├── stack.yaml
│       └── README.md
├── ml-platform/
│   └── mlflow/
│       └── stack.yaml
└── index.yaml
```

### 3.4 CLI Integration

```bash
# Search marketplace
nic marketplace search jupyterhub

# Install from marketplace
nic stack install nebari-stacks/data-science/jupyterhub

# Publish custom stack
nic stack publish ./my-custom-stack --to nebari-stacks
```

### 3.5 Implementation Phases

- **Phase 2:** Basic marketplace infrastructure
- **Future:** Full marketplace with community contributions, ratings, security scanning

---

## 4. Benefits Summary

### For Platform Teams

- **Single Source of Truth:** All infrastructure and applications in one repo
- **Version Control:** Full audit trail of all changes
- **Automated Workflows:** CI/CD handles validation, deployment, drift detection
- **Faster Onboarding:** Pre-built templates for common use cases
- **Reduced Toil:** Automatic integration between components

### For Development Teams

- **Self-Service:** Deploy databases, caching, etc. via config changes
- **Consistency:** Same patterns across dev/staging/production
- **GitOps Native:** Infrastructure changes via pull requests
- **Observability:** Automatic integration with monitoring stack

### For Organizations

- **Compliance:** All changes tracked and auditable
- **Cost Control:** Automated cost estimation and optimization
- **Security:** Automated scanning, OIDC authentication, least privilege
- **Standardization:** Consistent platform architecture across teams

---

## 5. Open Questions

1. **Stack Dependencies:** How to handle complex inter-stack dependencies? (DAG-based ordering? Helm hooks?)
2. **Stack Versioning:** Should stacks have independent version lifecycles? (Recommendation: Yes, use ArgoCD ApplicationSets)
3. **Multi-Tenancy:** How to isolate stacks for different teams? (Namespaces? Separate clusters? Virtual clusters?)
4. **Cost Attribution:** How to track costs per stack? (Cloud provider tags? FinOps integration?)
5. **Backup/Restore:** Should NIC handle stack data backup? (Recommendation: Yes via Velero integration)

---

**Status:** Proposed for Phase 2 implementation
**Last Updated:** 2025-01-12
**Feedback:** Please open GitHub issues with tag `enhancement`
