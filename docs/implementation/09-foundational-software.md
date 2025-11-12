# Foundational Software Stack

### 9.1 Stack Overview

**Complete LGTM + Platform Stack:**

| Component | Purpose | Why This Tool |
|-----------|---------|---------------|
| **ArgoCD** | GitOps continuous deployment | Industry standard, dependency management, self-healing |
| **cert-manager** | TLS certificate automation | Let's Encrypt integration, automatic renewal, cloud DNS solvers |
| **Envoy Gateway** | Ingress & API gateway | Kubernetes Gateway API, future-proof, advanced routing |
| **Keycloak** | Authentication & authorization | Open source, OIDC/SAML, user federation, battle-tested |
| **OpenTelemetry Collector** | Telemetry aggregation | Vendor-neutral, metrics/logs/traces, industry standard |
| **Mimir** | Metrics storage | Prometheus-compatible, horizontally scalable, cost-effective |
| **Loki** | Log aggregation | LogQL, integrates with Grafana, low-cost storage |
| **Tempo** | Distributed tracing | OpenTelemetry native, Grafana integration, scalable |
| **Grafana** | Visualization | Unified dashboards, alerting, LGTM native support |

### 9.2 Deployment Architecture

**ArgoCD App-of-Apps Pattern:**
```
ArgoCD (Deployed by NIC via Helm)
  ├── App: cert-manager (Priority: 1)
  ├── App: envoy-gateway (Priority: 2, depends: cert-manager)
  ├── App: opentelemetry-collector (Priority: 3)
  ├── App: mimir (Priority: 4, depends: opentelemetry-collector)
  ├── App: loki (Priority: 4, depends: opentelemetry-collector)
  ├── App: tempo (Priority: 4, depends: opentelemetry-collector)
  ├── App: grafana (Priority: 5, depends: mimir, loki, tempo)
  ├── App: keycloak (Priority: 6, depends: envoy-gateway, grafana)
  └── App: nebari-operator (Priority: 7, depends: keycloak, grafana, envoy-gateway)
```

**Repository Structure:**
```
nebari-foundational-software/
├── argocd-apps/
│   ├── cert-manager.yaml
│   ├── envoy-gateway.yaml
│   ├── opentelemetry-collector.yaml
│   ├── mimir.yaml
│   ├── loki.yaml
│   ├── tempo.yaml
│   ├── grafana.yaml
│   ├── keycloak.yaml
│   └── nebari-operator.yaml
├── cert-manager/
│   ├── kustomization.yaml
│   ├── cluster-issuer-letsencrypt.yaml
│   └── ...
├── envoy-gateway/
│   ├── kustomization.yaml
│   ├── gateway-class.yaml
│   └── ...
├── keycloak/
│   ├── kustomization.yaml
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   └── ...
├── observability/
│   ├── opentelemetry/
│   │   ├── collector-config.yaml
│   │   └── ...
│   ├── mimir/
│   │   ├── values.yaml
│   │   └── ...
│   ├── loki/
│   ├── tempo/
│   └── grafana/
└── operator/
    ├── crd.yaml
    ├── deployment.yaml
    └── ...
```

### 9.3 Component Details

#### 9.3.1 ArgoCD

**Installation Method:** Helm chart via NIC
**Namespace:** `nebari-system`

```go
func (d *Deployer) installArgoCD(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "installArgoCD")
    defer span.End()

    // Install ArgoCD via Helm
    helmChart := HelmChart{
        Name:       "argo-cd",
        Repo:       "https://argoproj.github.io/argo-helm",
        Chart:      "argo-cd",
        Version:    "5.51.0",
        Namespace:  "nebari-system",
        Values: map[string]interface{}{
            "server": map[string]interface{}{
                "ingress": map[string]interface{}{
                    "enabled": true,
                    "hosts":   []string{"argocd." + d.config.Domain},
                    "tls":     true,
                },
            },
            "configs": map[string]interface{}{
                "repositories": map[string]interface{}{
                    "nebari-foundational": map[string]interface{}{
                        "url":  d.config.FoundationalSoftware.ArgoCD.RepoURL,
                        "type": "git",
                    },
                },
            },
        },
    }

    if err := d.helm.Install(ctx, helmChart); err != nil {
        return fmt.Errorf("installing ArgoCD: %w", err)
    }

    // Wait for ArgoCD to be ready
    if err := d.waitForArgoCD(ctx); err != nil {
        return fmt.Errorf("waiting for ArgoCD: %w", err)
    }

    slog.InfoContext(ctx, "ArgoCD installed successfully")
    return nil
}
```

**Post-Installation:**
- Create ArgoCD Applications for foundational software
- Configure SSO with Keycloak (after Keycloak deploys)
- Set up RBAC (admin group from Keycloak)

#### 9.3.2 cert-manager

**Purpose:** Automated TLS certificate management

**Features:**
- Let's Encrypt integration (HTTP-01 and DNS-01 challenges)
- Automatic certificate renewal
- Wildcard certificate support
- Cloud DNS solver support (Route53, Cloud DNS, Azure DNS)

**Example ClusterIssuer:**
```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod-key
    solvers:
      - dns01:
          route53:  # For AWS
            region: us-west-2
```

#### 9.3.3 Envoy Gateway

**Purpose:** Modern ingress controller using Kubernetes Gateway API

**Features:**
- Gateway API (v1 stable)
- Advanced routing (header-based, weight-based)
- TLS termination (via cert-manager)
- Rate limiting, JWT validation
- OpenTelemetry tracing

**Example Gateway:**
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: nebari-gateway
  namespace: envoy-gateway-system
spec:
  gatewayClassName: envoy
  listeners:
    - name: https
      protocol: HTTPS
      port: 443
      tls:
        mode: Terminate
        certificateRefs:
          - name: wildcard-tls
            namespace: envoy-gateway-system
      hostname: "*.nebari.example.com"
```

#### 9.3.4 Keycloak

**Purpose:** Centralized authentication and authorization

**Features:**
- OAuth2 / OIDC provider
- User federation (LDAP, Active Directory)
- Social login (Google, GitHub, etc.)
- Multi-factor authentication
- User self-service (password reset, profile management)

**Deployment:**
- High-availability mode (2+ replicas)
- PostgreSQL database for persistence
- Ingress: `https://auth.nebari.example.com`

**Integration:**
- ArgoCD SSO
- Grafana SSO
- Nebari Operator (OAuth client creation for apps)

#### 9.3.5 OpenTelemetry Collector

**Purpose:** Centralized telemetry collection

**Features:**
- Receives metrics, logs, traces
- Protocol support: OTLP, Prometheus, Jaeger, Zipkin
- Processing pipelines (filtering, sampling, batching)
- Export to LGTM stack

**Example Configuration:**
```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
  prometheus:
    config:
      scrape_configs:
        - job_name: 'kubernetes-pods'
          # Scrape pods with prometheus.io/scrape annotation

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024

exporters:
  prometheusremotewrite:
    endpoint: http://mimir.monitoring.svc:9009/api/v1/push
  loki:
    endpoint: http://loki.monitoring.svc:3100/loki/api/v1/push
  otlp/tempo:
    endpoint: tempo.monitoring.svc:4317

service:
  pipelines:
    metrics:
      receivers: [otlp, prometheus]
      processors: [batch]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [loki]
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/tempo]
```

#### 9.3.6 Mimir (Metrics)

**Purpose:** Scalable Prometheus-compatible metrics storage

**Features:**
- Horizontally scalable
- Long-term storage (object storage: S3/GCS/Azure Blob)
- Prometheus-compatible query API
- Multi-tenancy support
- Compaction and downsampling

**Storage:**
- Short-term: In-cluster (PersistentVolumes)
- Long-term: Cloud object storage (90 days retention)

#### 9.3.7 Loki (Logs)

**Purpose:** Scalable log aggregation

**Features:**
- LogQL query language (similar to PromQL)
- Label-based indexing (cost-effective)
- Cloud object storage for logs
- Grafana native integration

**Collection:**
- Promtail DaemonSet (node logs)
- OpenTelemetry Collector (application logs)

#### 9.3.8 Tempo (Traces)

**Purpose:** Distributed tracing backend

**Features:**
- OpenTelemetry native
- TraceQL query language
- Object storage for traces
- Integration with Grafana and Loki

**Use Cases:**
- Request tracing across microservices
- Performance debugging
- Dependency visualization

#### 9.3.9 Grafana

**Purpose:** Unified visualization and alerting

**Features:**
- Dashboards for metrics, logs, traces
- Alerting with multiple notification channels
- Data source management (Mimir, Loki, Tempo)
- SSO via Keycloak
- Dashboard provisioning via ConfigMaps

**Pre-configured Dashboards:**
- Kubernetes cluster overview
- Node resources
- Pod resources
- Foundational software health
- NIC deployment metrics

### 9.4 Health Checks and Readiness

**NIC Health Check Loop:**
```go
func (d *Deployer) waitForFoundationalSoftware(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "waitForFoundationalSoftware")
    defer span.End()

    components := []Component{
        {Name: "cert-manager", Namespace: "cert-manager"},
        {Name: "envoy-gateway", Namespace: "envoy-gateway-system"},
        {Name: "opentelemetry-collector", Namespace: "monitoring"},
        {Name: "mimir", Namespace: "monitoring"},
        {Name: "loki", Namespace: "monitoring"},
        {Name: "tempo", Namespace: "monitoring"},
        {Name: "grafana", Namespace: "monitoring"},
        {Name: "keycloak", Namespace: "nebari-system"},
        {Name: "nebari-operator", Namespace: "nebari-system"},
    }

    for _, component := range components {
        slog.InfoContext(ctx, "waiting for component", "name", component.Name)

        if err := d.waitForDeployment(ctx, component.Name, component.Namespace, 10*time.Minute); err != nil {
            return fmt.Errorf("waiting for %s: %w", component.Name, err)
        }

        slog.InfoContext(ctx, "component ready", "name", component.Name)
    }

    return nil
}
```

---
