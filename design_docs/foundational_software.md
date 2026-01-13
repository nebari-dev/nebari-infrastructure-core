# Foundational Software Stack

### Overview

The foundational software stack provides essential services for GitOps deployment, identity management, certificate management, networking, and observability. These components are installed automatically during the `nic deploy` command and form the base layer upon which other Nebari services are built.

### Stack Components

| Component                   | Purpose                        | Why This Tool                                                   |
| --------------------------- | ------------------------------ | --------------------------------------------------------------- |
| **Argo CD**                 | GitOps continuous deployment   | Industry standard, dependency management, self-healing          |
| **Cert Manager**            | TLS certificate automation     | Let's Encrypt integration, automatic renewal, cloud DNS solvers |
| **Envoy Gateway**           | Ingress & API gateway          | Kubernetes Gateway API, future-proof, advanced routing          |
| **Keycloak**                | Authentication & authorization | Open source, OIDC/SAML, user federation, battle-tested          |
| **OpenTelemetry Collector** | Telemetry aggregation          | Vendor-neutral, metrics/logs/traces, industry standard          |

### Deployment Architecture

**Argo CD App-of-Apps Pattern:**

```
Argo CD (Deployed by NIC via Helm)
  ├── App: cert-manager (Priority: 1)
  ├── App: envoy-gateway (Priority: 2, depends: cert-manager)
  ├── App: opentelemetry-collector (Priority: 3)
  └── App: keycloak (Priority: 4, depends: envoy-gateway)
```

**Repository Structure:**

```
nebari-foundational-software/
├── argocd-apps/
│   ├── cert-manager.yaml
│   ├── envoy-gateway.yaml
│   ├── opentelemetry-collector.yaml
│   └── keycloak.yaml
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
└── observability/
    └── opentelemetry/
        ├── collector-config.yaml
        └── ...
```

### Component Details

#### Argo CD

**Installation Method:** Helm chart via NIC
**Namespace:** `argocd`
**Version:** 7.7.9 (Helm chart)

**Purpose:** GitOps continuous delivery tool for Kubernetes applications

**Key Features:**
- Declarative GitOps continuous delivery
- Automated application deployment and lifecycle management
- Application health monitoring and visualization
- Multi-cluster deployment support

**Installation Code:**

```go
func (d *Deployer) installArgoCD(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "installArgoCD")
    defer span.End()

    // Install Argo CD via Helm
    helmChart := HelmChart{
        Name:       "argo-cd",
        Repo:       "https://argoproj.github.io/argo-helm",
        Chart:      "argo-cd",
        Version:    "7.7.9",
        Namespace:  "argocd",
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
        return fmt.Errorf("installing Argo CD: %w", err)
    }

    // Wait for Argo CD to be ready
    if err := d.waitForArgoCD(ctx); err != nil {
        return fmt.Errorf("waiting for Argo CD: %w", err)
    }

    slog.InfoContext(ctx, "Argo CD installed successfully")
    return nil
}
```

**Access:**
```bash
# Port-forward to access UI
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Get admin password
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d
```

**Post-Installation:**
- Create Argo CD Applications for foundational software
- Configure SSO with Keycloak (after Keycloak deploys)
- Set up RBAC (admin group from Keycloak)

#### Cert Manager

**Purpose:** Automated TLS certificate management

**Status:** Planned

**Features:**
- Let's Encrypt integration (HTTP-01 and DNS-01 challenges)
- Automatic certificate renewal
- Wildcard certificate support
- Cloud DNS solver support (Route53, Cloud DNS, Azure DNS, Cloudflare)

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
          route53: # For AWS
            region: us-west-2
```

#### Envoy Gateway

**Purpose:** Modern ingress controller using Kubernetes Gateway API

**Status:** Planned

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

#### Keycloak

**Installation Method:** Argo CD Application deploying codecentric/keycloakx Helm chart
**Namespace:** `keycloak`
**Version:** 26.5.0 (using keycloakx Helm chart 7.1.6)

**Purpose:** Centralized authentication and authorization

**Key Features:**
- OAuth2 / OIDC provider
- User federation (LDAP, Active Directory)
- Social login (Google, GitHub, etc.)
- Multi-factor authentication
- User self-service (password reset, profile management)
- Single sign-on (SSO) authentication

**Deployment:**
- Uses external PostgreSQL database (Bitnami chart 18.2.0)
- Health checks on management port 9000 under `/auth` path
- Ingress configured for external access
- High-availability mode support (2+ replicas)

**Database:**
- PostgreSQL 18.2.0 deployed via separate Argo CD Application
- 10Gi persistent storage for database
- Automated initialization script creates keycloak database and user
- Resource limits: 1 CPU, 1Gi memory

**Secrets:**
- `keycloak-admin-credentials`: Admin user password
- `keycloak-postgresql-credentials`: Keycloak database user password
- `postgresql-credentials`: PostgreSQL admin and user passwords

**Integration:**
- Argo CD SSO
- OAuth client creation for applications

**Implementation Files:**
```
pkg/kubernetes/foundational/
├── keycloak-application.yaml       # Argo CD Application manifest
├── keycloak-namespace.yaml         # Namespace definition
├── keycloak-secrets.yaml           # Secret templates
├── postgresql-application.yaml     # PostgreSQL Argo CD Application
└── postgresql-secrets.yaml         # PostgreSQL secret templates
```

#### OpenTelemetry Collector

**Purpose:** Centralized telemetry collection

**Status:** Planned

**Features:**
- Receives metrics, logs, traces
- Protocol support: OTLP, Prometheus, Jaeger, Zipkin
- Processing pipelines (filtering, sampling, batching)
- Export to observability backends

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
        - job_name: "kubernetes-pods"
          # Scrape pods with prometheus.io/scrape annotation

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024

exporters:
  logging:
    loglevel: debug
  # Additional exporters can be configured based on backend

service:
  pipelines:
    metrics:
      receivers: [otlp, prometheus]
      processors: [batch]
      exporters: [logging]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
```

### Installation Flow

The foundational software is installed in the following order during `nic deploy`:

1. **Argo CD** - Installed first via Helm SDK directly
2. **Keycloak + PostgreSQL** - Deployed via Argo CD Applications
3. **Cert Manager** - (To be implemented)
4. **Envoy Gateway** - (To be implemented)
5. **OpenTelemetry Collector** - (To be implemented)

### Implementation Details

**Code Organization:**

```
pkg/kubernetes/
├── kubernetes.go           # Argo CD installation via Helm SDK
├── applications.go         # Argo CD Application CRUD operations
├── foundational.go         # Foundational services installation
└── foundational/
    ├── keycloak-application.yaml
    ├── keycloak-namespace.yaml
    ├── keycloak-secrets.yaml
    ├── postgresql-application.yaml
    └── postgresql-secrets.yaml
```

**Installation Trigger:**

Foundational software installation is triggered in `cmd/nic/deploy.go` after infrastructure provisioning completes:

```go
// 1. Install Argo CD
if err := kubernetes.InstallArgoCD(ctx, cfg, provider); err != nil {
    slog.Warn("Failed to install Argo CD", "error", err)
}

// 2. Install foundational services via Argo CD
foundationalCfg := kubernetes.FoundationalConfig{
    Keycloak: kubernetes.KeycloakConfig{
        Enabled:       true,
        AdminPassword: generateSecurePassword(),
        DBPassword:    generateSecurePassword(),
    },
}
if err := kubernetes.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg); err != nil {
    slog.Warn("Failed to install foundational services", "error", err)
}
```

**Error Handling:**
- Foundational service installation failures do NOT fail the deployment
- Warnings are logged for failed installations
- Manual installation commands are provided for recovery
- Idempotent installation allows rerunning without errors

### Health Checks and Readiness

**NIC Health Check Loop:**

```go
func (d *Deployer) waitForFoundationalSoftware(ctx context.Context) error {
    ctx, span := tracer.Start(ctx, "waitForFoundationalSoftware")
    defer span.End()

    components := []Component{
        {Name: "argocd-server", Namespace: "argocd"},
        {Name: "cert-manager", Namespace: "cert-manager"},
        {Name: "envoy-gateway", Namespace: "envoy-gateway-system"},
        {Name: "opentelemetry-collector", Namespace: "monitoring"},
        {Name: "keycloak", Namespace: "keycloak"},
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

### Troubleshooting

#### Argo CD

```bash
# Check Argo CD status
kubectl get pods -n argocd
kubectl get applications -n argocd

# View application sync status
kubectl describe application <app-name> -n argocd
```

#### Keycloak

```bash
# Check Keycloak pod status
kubectl get pods -n keycloak
kubectl logs -n keycloak keycloak-keycloakx-0

# Check PostgreSQL status
kubectl get pods -n keycloak postgresql-0
kubectl logs -n keycloak postgresql-0

# Verify secrets exist
kubectl get secrets -n keycloak
```

#### Common Issues

**Keycloak pod not ready:**
- Verify PostgreSQL is running and healthy
- Check database connection secrets are correct
- Verify health endpoints are accessible on port 9000 under `/auth` path

**Argo CD Application not syncing:**
- Check Application manifest syntax
- Verify Helm chart repository is accessible
- Review Application events for error details

**Database initialization failures:**
- Check PostgreSQL logs for init script errors
- Verify environment variables are set correctly
- Ensure database credentials secrets exist

#### Cert Manager (To Be Implemented)

```bash
# Check cert-manager status
kubectl get pods -n cert-manager
kubectl get certificates -A
kubectl get clusterissuers
```

#### Envoy Gateway (To Be Implemented)

```bash
# Check Envoy Gateway status
kubectl get pods -n envoy-gateway-system
kubectl get gateways -A
kubectl get httproutes -A
```

#### OpenTelemetry Collector (To Be Implemented)

```bash
# Check OTEL Collector status
kubectl get pods -n monitoring
kubectl logs -n monitoring <otel-collector-pod>
```

---
## Helm charts to use
For cert-manager:

oci://quay.io/jetstack/charts/cert-manager:v1.19.2

envoy-gateway:
oci://docker.io/envoyproxy/gateway-helm
version 1.6.2

open telemetry collector:
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm install my-opentelemetry-collector open-telemetry/opentelemetry-collector \
   --set image.repository="otel/opentelemetry-collector-k8s" \
   --set mode=<daemonset|deployment|statefulset>