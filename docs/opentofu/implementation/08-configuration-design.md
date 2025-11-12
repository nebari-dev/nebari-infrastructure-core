# Configuration Design

### 8.1 Configuration Philosophy

**Same as Native SDK Edition:**
- New clean configuration format (not constrained by old nebari-config.yaml)
- Optimized for new architecture
- Clear separation of concerns
- Validation at parse time

### 8.2 Configuration Structure

**Example: `nebari-config.yaml`**
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

# State backend configuration
state_backend:
  aws:
    bucket: "nebari-prod-terraform-state"
    key: "nic/terraform.tfstate"
    region: "us-west-2"
    dynamodb_table: "nebari-prod-terraform-locks"

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

  observability:
    enabled: true
    grafana:
      version: "10.3.0"
    loki:
      version: "2.9.0"
      retention_days: 30
    mimir:
      version: "2.11.0"
      retention_days: 90
    tempo:
      version: "2.3.0"
      retention_days: 14
    opentelemetry:
      version: "0.95.0"

  nebari_operator:
    enabled: true
    version: "1.0.0"
```

**(Rest of sections 8.3-8.4 identical to Native SDK edition)**

---
