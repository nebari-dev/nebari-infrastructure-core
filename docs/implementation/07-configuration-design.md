# Configuration Design

### 7.1 Configuration Philosophy

**New Clean Configuration Format:**
- Not constrained by old config.yaml
- Optimized for new architecture
- Clear separation of concerns
- Validation at parse time

### 7.2 Configuration Structure

**Example: `config.yaml`**
```yaml
version: "2025.1.0"
name: nebari-prod

provider:
  type: aws  # aws | gcp | azure | local
  region: us-west-2

  # Provider-specific configuration
  aws:
    account_id: "123456789012"
    vpc:
      cidr: "10.0.0.0/16"
      availability_zones: 3

kubernetes:
  version: "1.29"

  node_pools:
    - name: general
      instance_type: m6i.2xlarge
      min_size: 3
      max_size: 10
      labels:
        workload: general
      taints: []

    - name: compute
      instance_type: m6i.8xlarge
      min_size: 0
      max_size: 20
      labels:
        workload: compute
      taints:
        - key: compute
          value: "true"
          effect: NoSchedule

    - name: gpu
      instance_type: g5.2xlarge
      min_size: 0
      max_size: 5
      labels:
        workload: gpu
        nvidia.com/gpu: "true"
      taints:
        - key: nvidia.com/gpu
          value: "true"
          effect: NoSchedule

domain: nebari.example.com

tls:
  enabled: true
  letsencrypt:
    enabled: true
    email: admin@example.com
  # Or bring your own cert:
  # certificate_secret: custom-tls-cert

foundational_software:
  argocd:
    enabled: true
    version: "2.10.0"
    repo_url: "https://github.com/nebari-dev/nebari-foundational-software"

  cert_manager:
    enabled: true
    version: "1.14.0"

  envoy_gateway:
    enabled: true
    version: "1.0.0"

  keycloak:
    enabled: true
    version: "23.0.0"
    admin_username: admin
    # admin_password generated and stored in secret
    themes:
      - nebari-theme

  observability:
    enabled: true

    grafana:
      version: "10.3.0"
      admin_username: admin

    loki:
      version: "2.9.0"
      retention_days: 30
      storage_size: 100Gi

    mimir:
      version: "2.11.0"
      retention_days: 90
      storage_size: 500Gi

    tempo:
      version: "2.3.0"
      retention_days: 14
      storage_size: 100Gi

    opentelemetry:
      version: "0.95.0"
      # Endpoints exported by default to LGTM stack

  nebari_operator:
    enabled: true
    version: "1.0.0"

# Optional: override default images
images:
  registry: ghcr.io/nebari-dev
  pull_policy: IfNotPresent

# Optional: enable features
features:
  auto_upgrade: false
  backup: true
  monitoring_alerts: true
```

### 7.3 Configuration Validation

**Validation Stages:**
1. **Schema validation**: YAML structure matches schema
2. **Provider validation**: Provider-specific settings valid
3. **Version compatibility**: Kubernetes version supported by provider
4. **Resource limits**: Instance types valid for region
5. **Dependency checks**: e.g., TLS requires domain

**CLI Validation:**
```bash
$ nic validate -f config.yaml
✅ Configuration valid

Summary:
  Provider: AWS (us-west-2)
  Kubernetes: 1.29
  Node pools: 3 (general, compute, gpu)
  Domain: nebari.example.com
  TLS: Let's Encrypt
  Foundational software: 9 components enabled
```

### 7.4 Multi-Environment Support

**MVP Approach:** Use separate configuration files per environment (dev.yaml, staging.yaml, production.yaml).

**Future Enhancement:** Config overlays with base/override pattern (see docs/appendix/15-future-enhancements.md).

---
