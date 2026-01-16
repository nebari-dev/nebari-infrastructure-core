# Nebari Kubernetes Operator

### 10.1 Operator Purpose

**Problem:** Applications need to integrate with auth, o11y, and routing - currently manual and error-prone.

**Solution:** Kubernetes operator that watches `NebariApplication` CRDs and automates:
- OAuth2 client creation in Keycloak
- HTTPRoute configuration in Envoy Gateway
- TLS certificate provisioning via cert-manager
- Grafana dashboard provisioning
- OpenTelemetry ServiceMonitor creation

### 10.2 NebariApplication CRD

**CRD Definition:**
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: nebariapplications.nebari.dev
spec:
  group: nebari.dev
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required: [displayName, routing]
              properties:
                displayName:
                  type: string
                  description: "Human-readable application name"

                routing:
                  type: object
                  required: [domain, paths]
                  properties:
                    domain:
                      type: string
                      description: "Application domain (e.g., jupyter.example.com)"
                    enableTLS:
                      type: boolean
                      default: true
                    paths:
                      type: array
                      items:
                        type: object
                        required: [path, service, port]
                        properties:
                          path:
                            type: string
                          service:
                            type: string
                          port:
                            type: integer

                authentication:
                  type: object
                  properties:
                    enabled:
                      type: boolean
                      default: true
                    allowedGroups:
                      type: array
                      items:
                        type: string
                    allowedUsers:
                      type: array
                      items:
                        type: string
                    publicPaths:
                      type: array
                      description: "Paths that don't require auth"
                      items:
                        type: string

                observability:
                  type: object
                  properties:
                    metrics:
                      type: object
                      properties:
                        enabled:
                          type: boolean
                          default: true
                        port:
                          type: integer
                        path:
                          type: string
                          default: "/metrics"
                    logs:
                      type: object
                      properties:
                        enabled:
                          type: boolean
                          default: true
                    traces:
                      type: object
                      properties:
                        enabled:
                          type: boolean
                          default: true
                    dashboards:
                      type: array
                      items:
                        type: object
                        properties:
                          name:
                            type: string
                          source:
                            type: string
                            description: "URL to dashboard JSON or ConfigMap reference"

            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: [Pending, Provisioning, Ready, Error]
                url:
                  type: string
                  description: "Public URL of the application"
                keycloakClientID:
                  type: string
                  description: "OAuth2 client ID in Keycloak"
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      lastTransitionTime:
                        type: string
                        format: date-time
                      reason:
                        type: string
                      message:
                        type: string
```

### 10.3 Example Usage

**Deploy JupyterHub with Full Integration:**
```yaml
apiVersion: nebari.dev/v1alpha1
kind: NebariApplication
metadata:
  name: jupyterhub
  namespace: jupyter
spec:
  displayName: "JupyterHub"

  routing:
    domain: jupyter.nebari.example.com
    enableTLS: true
    paths:
      - path: /
        service: jupyterhub
        port: 8000

  authentication:
    enabled: true
    allowedGroups:
      - data-scientists
      - admins
    publicPaths:
      - /hub/health  # Health check endpoint

  observability:
    metrics:
      enabled: true
      port: 9090
      path: /metrics
    logs:
      enabled: true
    traces:
      enabled: true
    dashboards:
      - name: "JupyterHub Overview"
        source: "https://raw.githubusercontent.com/jupyterhub/grafana-dashboards/main/jupyterhub.json"
      - name: "JupyterHub User Activity"
        source: "configmap://jupyter/jupyterhub-dashboard"
```

**Operator Creates:**

1. **Keycloak OAuth2 Client:**
```json
{
  "clientId": "jupyterhub-jupyter",
  "name": "JupyterHub",
  "redirectUris": [
    "https://jupyter.nebari.example.com/hub/oauth_callback"
  ],
  "webOrigins": [
    "https://jupyter.nebari.example.com"
  ],
  "protocol": "openid-connect",
  "publicClient": false,
  "directAccessGrantsEnabled": false,
  "serviceAccountsEnabled": false,
  "authorizationServicesEnabled": false
}
```

2. **Envoy Gateway HTTPRoute:**
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: jupyterhub
  namespace: jupyter
spec:
  parentRefs:
    - name: nebari-gateway
      namespace: envoy-gateway-system
  hostnames:
    - jupyter.nebari.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: jupyterhub
          port: 8000
      filters:
        - type: ExtensionRef
          extensionRef:
            group: gateway.envoyproxy.io
            kind: SecurityPolicy
            name: jupyterhub-oauth
```

3. **cert-manager Certificate:**
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: jupyterhub-tls
  namespace: jupyter
spec:
  secretName: jupyterhub-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - jupyter.nebari.example.com
```

4. **OpenTelemetry ServiceMonitor:**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: jupyterhub-metrics
  namespace: jupyter
  labels:
    app: jupyterhub
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "9090"
    prometheus.io/path: "/metrics"
spec:
  selector:
    app: jupyterhub
  ports:
    - name: metrics
      port: 9090
      targetPort: 9090
```

5. **Grafana Dashboard ConfigMap:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: jupyterhub-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"
data:
  jupyterhub.json: |
    {
      "dashboard": {
        "title": "JupyterHub Overview",
        "panels": [ ... ]
      }
    }
```

6. **Status Update:**
```yaml
status:
  phase: Ready
  url: https://jupyter.nebari.example.com
  keycloakClientID: jupyterhub-jupyter
  conditions:
    - type: RoutingConfigured
      status: "True"
      lastTransitionTime: "2025-01-30T12:00:00Z"
    - type: AuthenticationConfigured
      status: "True"
      lastTransitionTime: "2025-01-30T12:01:00Z"
    - type: ObservabilityConfigured
      status: "True"
      lastTransitionTime: "2025-01-30T12:02:00Z"
    - type: Ready
      status: "True"
      lastTransitionTime: "2025-01-30T12:02:00Z"
      reason: AllComponentsReady
      message: "Application is fully configured and accessible"
```

### 10.4 Operator Implementation

**Controller Logic:**
```go
package operator

import (
    "context"
    "fmt"

    nebaridevv1alpha1 "github.com/nebari-dev/nic/api/v1alpha1"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"
)

type NebariApplicationReconciler struct {
    client.Client
    KeycloakClient *keycloak.Client
    EnvoyClient    *envoy.Client
    GrafanaClient  *grafana.Client
}

func (r *NebariApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ctx, span := tracer.Start(ctx, "Reconcile")
    defer span.End()

    log := log.FromContext(ctx)

    // Fetch NebariApplication
    var app nebaridevv1alpha1.NebariApplication
    if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Update status to Provisioning
    app.Status.Phase = "Provisioning"
    if err := r.Status().Update(ctx, &app); err != nil {
        return ctrl.Result{}, err
    }

    // 1. Configure routing (Envoy HTTPRoute + cert-manager Certificate)
    if err := r.configureRouting(ctx, &app); err != nil {
        log.Error(err, "failed to configure routing")
        return ctrl.Result{}, err
    }
    r.updateCondition(&app, "RoutingConfigured", "True", "RoutingReady", "Routing configured successfully")

    // 2. Configure authentication (Keycloak OAuth client)
    if app.Spec.Authentication.Enabled {
        clientID, err := r.configureAuthentication(ctx, &app)
        if err != nil {
            log.Error(err, "failed to configure authentication")
            return ctrl.Result{}, err
        }
        app.Status.KeycloakClientID = clientID
        r.updateCondition(&app, "AuthenticationConfigured", "True", "AuthReady", "OAuth client created")
    }

    // 3. Configure observability (metrics, dashboards)
    if err := r.configureObservability(ctx, &app); err != nil {
        log.Error(err, "failed to configure observability")
        return ctrl.Result{}, err
    }
    r.updateCondition(&app, "ObservabilityConfigured", "True", "ObservabilityReady", "Observability configured")

    // 4. Update final status
    app.Status.Phase = "Ready"
    app.Status.URL = fmt.Sprintf("https://%s", app.Spec.Routing.Domain)
    r.updateCondition(&app, "Ready", "True", "AllComponentsReady", "Application fully configured")

    if err := r.Status().Update(ctx, &app); err != nil {
        return ctrl.Result{}, err
    }

    log.Info("reconciliation complete", "app", app.Name, "url", app.Status.URL)
    return ctrl.Result{}, nil
}

func (r *NebariApplicationReconciler) configureRouting(ctx context.Context, app *nebaridevv1alpha1.NebariApplication) error {
    ctx, span := tracer.Start(ctx, "configureRouting")
    defer span.End()

    // Create cert-manager Certificate
    if app.Spec.Routing.EnableTLS {
        if err := r.createCertificate(ctx, app); err != nil {
            return fmt.Errorf("creating certificate: %w", err)
        }
    }

    // Create Envoy HTTPRoute
    if err := r.createHTTPRoute(ctx, app); err != nil {
        return fmt.Errorf("creating HTTPRoute: %w", err)
    }

    return nil
}

func (r *NebariApplicationReconciler) configureAuthentication(ctx context.Context, app *nebaridevv1alpha1.NebariApplication) (string, error) {
    ctx, span := tracer.Start(ctx, "configureAuthentication")
    defer span.End()

    redirectURI := fmt.Sprintf("https://%s/oauth_callback", app.Spec.Routing.Domain)

    clientID, clientSecret, err := r.KeycloakClient.CreateOAuthClient(ctx, keycloak.OAuthClientRequest{
        Name:         app.Spec.DisplayName,
        RedirectURIs: []string{redirectURI},
        AllowedGroups: app.Spec.Authentication.AllowedGroups,
    })

    if err != nil {
        return "", fmt.Errorf("creating Keycloak client: %w", err)
    }

    // Store client secret in Kubernetes Secret
    if err := r.createOAuthSecret(ctx, app, clientID, clientSecret); err != nil {
        return "", fmt.Errorf("creating OAuth secret: %w", err)
    }

    return clientID, nil
}

func (r *NebariApplicationReconciler) configureObservability(ctx context.Context, app *nebaridevv1alpha1.NebariApplication) error {
    ctx, span := tracer.Start(ctx, "configureObservability")
    defer span.End()

    // Create ServiceMonitor for metrics
    if app.Spec.Observability.Metrics.Enabled {
        if err := r.createServiceMonitor(ctx, app); err != nil {
            return fmt.Errorf("creating ServiceMonitor: %w", err)
        }
    }

    // Provision Grafana dashboards
    for _, dashboard := range app.Spec.Observability.Dashboards {
        if err := r.provisionDashboard(ctx, app, dashboard); err != nil {
            return fmt.Errorf("provisioning dashboard %s: %w", dashboard.Name, err)
        }
    }

    return nil
}
```

### 10.5 Operator Benefits

**For Users:**
- ✅ One manifest to deploy + integrate application
- ✅ No manual OAuth client creation
- ✅ No manual HTTPRoute configuration
- ✅ No manual dashboard import
- ✅ Automatic TLS certificate provisioning
- ✅ Status updates show integration progress

**For Platform Team:**
- ✅ Consistent integration patterns
- ✅ Centralized configuration management
- ✅ Easier to update (change operator, all apps benefit)
- ✅ Self-documenting (CRD schema is API contract)
- ✅ Audit trail (Git history of CRDs)

---
