# Longhorn UI Gateway Exposure Design

**Date:** 2026-05-22

## Problem

The Longhorn UI (`longhorn-frontend` Service in the `longhorn-system` namespace, port `8080`) is not currently reachable from outside the cluster. Operators must use `kubectl port-forward` to inspect or repair volumes.

The repo already runs Envoy Gateway with HTTPS termination and a wildcard certificate covering several `*.<domain>` subdomains (ArgoCD, Keycloak). The natural fit is to expose Longhorn at `longhorn.<domain>` through the same gateway.

Longhorn ships with **no built-in authentication**. Routing the UI publicly without an auth layer would give anyone with the URL full destructive access to PVs. Authentication must be enforced at the gateway.

## Design

Expose `longhorn.<domain>` through the existing `nebari-gateway`, gated by Keycloak OIDC enforced via an Envoy Gateway `SecurityPolicy`. The integration mirrors the existing ArgoCD SSO design (`docs/superpowers/specs/2026-04-08-argocd-keycloak-sso-design.md`) layer-for-layer.

The route is only created when Longhorn is enabled. Keycloak is mandatory infrastructure (always provisioned during foundational install), so the gate is `LonghornEnabled` alone.

Authorization is **authentication-only** in this first cut: any logged-in Keycloak user can reach the UI. The realm-setup job creates `longhorn-admins` / `longhorn-viewers` groups so a future change can add group-based gating, but no gating is enforced now (Longhorn itself has no user/group model to map onto).

### Architecture

Three layers, executed during `nic deploy`:

```
Go (foundational.go + deploy.go)        Keycloak (realm-setup-job)         Envoy Gateway (manifests)
────────────────────────────────        ──────────────────────────         ─────────────────────────
Generate longhorn client secret    -->  Create `longhorn` OIDC client  --> HTTPRoute longhorn.<domain>
Store as Secret in two namespaces:      with pre-generated secret           SecurityPolicy → OIDC
  - keycloak (read by realm-setup)      Create groups:                      Cert dnsName += longhorn.<domain>
  - longhorn-system (read by              longhorn-admins, longhorn-viewers
      SecurityPolicy)                   Add admin user to longhorn-admins
```

All three layers are skipped when `LonghornEnabled` is false.

### Layer 1: Go Code Changes

#### 1a. New `LonghornSSOConfig` on `FoundationalConfig`

In `pkg/argocd/foundational.go`, mirror `ArgoCDSSOConfig`:

```go
type FoundationalConfig struct {
    Keycloak    KeycloakConfig
    ArgoCD      ArgoCDSSOConfig
    Longhorn    LonghornSSOConfig  // NEW
    LandingPage LandingPageConfig
    MetalLB     MetalLBConfig
}

type LonghornSSOConfig struct {
    // ClientSecret is the pre-generated OIDC client secret for Longhorn's
    // Keycloak integration. Empty when Longhorn UI exposure is disabled
    // (i.e., Longhorn is not installed by this provider).
    ClientSecret string
}
```

#### 1b. Provision the client secret in `cmd/nic/deploy.go`

Compute Longhorn-enabled state from the provider, then generate the secret when Longhorn is enabled (Keycloak is always present):

```go
infraSettings := provider.InfraSettings(cfg.Cluster)
// ...
if infraSettings.LonghornEnabled {
    foundationalCfg.Longhorn.ClientSecret = generateSecurePassword(rand.Reader)
}
```

#### 1c. Expose `LonghornEnabled` on `InfraSettings`

Add a new field to `provider.InfraSettings` (`pkg/provider/provider.go`):

```go
type InfraSettings struct {
    // ... existing fields ...

    // LonghornEnabled indicates whether the Longhorn distributed block storage
    // (and therefore the Longhorn UI) is deployed by this provider. Used by
    // the foundational deploy flow to decide whether to expose `longhorn.<domain>`
    // through the gateway and provision an OIDC client.
    LonghornEnabled bool
}
```

Each provider's `InfraSettings()` populates it:

- `pkg/provider/aws/provider.go`: `LonghornEnabled: awsCfg.LonghornEnabled()`
- `pkg/provider/hetzner/provider.go`: `LonghornEnabled: hCfg.LonghornEnabled()`
- `pkg/provider/azure/provider.go`, `gcp`, `local`: `LonghornEnabled: false` (until the respective providers wire Longhorn)

#### 1d. Create the `longhorn-oidc-client-secret` Secret

Extend `createKeycloakSecrets` (or split out a `createLonghornSecrets` helper that follows the same pattern as the existing ArgoCD client-secret path):

```go
if longhornSSO.ClientSecret != "" {
    createSecret(ctx, client, &corev1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "longhorn-oidc-client-secret",
            Namespace: KeycloakDefaultNamespace,
        },
        Type: corev1.SecretTypeOpaque,
        StringData: map[string]string{
            "client-secret": longhornSSO.ClientSecret,
        },
    })
}
```

#### 1e. Create the `longhorn-system` namespace + duplicate Secret

The SecurityPolicy lives in `longhorn-system` (see Layer 3) and references its OIDC client secret by Secret name in the same namespace. So foundational must create:

- the `longhorn-system` namespace (if not already created — the Longhorn helm install also creates it, but foundational runs first), and
- a second copy of `longhorn-oidc-client-secret` in `longhorn-system` with the same `client-secret` value

Both Secret copies hold the same in-memory `ClientSecret` value. Creating them in two namespaces (rather than using a Secret-replication mechanism) is the simplest path and matches how the realm-setup job already reads `argocd-oidc-client-secret` from a fixed namespace.

#### 1f. Thread template data through `writer.go`

`pkg/argocd/writer.go`'s template data needs one new field:

```go
type templateData struct {
    // ... existing fields ...
    LonghornEnabled bool // true when route/policy/cert/realm-snippet should render
}
```

Set to `infraSettings.LonghornEnabled`. The writer gates rendering of the new manifests on this flag.

### Layer 2: Keycloak Realm-Setup Additions

Extend `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`. Two additions, wrapped in `{{ if .LonghornEnabled }} ... {{ end }}`:

1. **Env var** referencing the new secret:

```yaml
{{ if .LonghornEnabled }}
- name: LONGHORN_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: longhorn-oidc-client-secret
      key: client-secret
{{ end }}
```

2. **Inline `kcadm` block** appended to the existing setup script:

```bash
{{ if .LonghornEnabled }}
echo "Creating Longhorn OIDC client..."
$KCADM create clients -r nebari \
  -s clientId=longhorn \
  -s enabled=true \
  -s protocol=openid-connect \
  -s publicClient=false \
  -s "secret=$LONGHORN_CLIENT_SECRET" \
  -s "redirectUris=[\"https://longhorn.$DOMAIN/oauth2/callback\"]" \
  -s directAccessGrantsEnabled=false \
  -s standardFlowEnabled=true || echo "Client may already exist"

echo "Creating Longhorn access groups..."
$KCADM create groups -r nebari -s name=longhorn-admins || echo "Group may already exist"
$KCADM create groups -r nebari -s name=longhorn-viewers || echo "Group may already exist"

echo "Adding admin user to longhorn-admins group..."
LONGHORN_ADMINS_GROUP_ID=$($KCADM get groups -r nebari --fields id,name | \
  grep -B1 '"name" *: *"longhorn-admins"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

if [ -n "$ADMIN_USER_ID" ] && [ -n "$LONGHORN_ADMINS_GROUP_ID" ]; then
  $KCADM update users/$ADMIN_USER_ID/groups/$LONGHORN_ADMINS_GROUP_ID -r nebari \
    -s realm=nebari -s userId=$ADMIN_USER_ID -s groupId=$LONGHORN_ADMINS_GROUP_ID -n || true
fi
{{ end }}
```

`ADMIN_USER_ID` is already resolved earlier in the script (for the ArgoCD admin-group assignment) and can be reused here.

### Layer 3: Manifest Templates

#### 3a. New `longhorn-httproute.yaml`

`pkg/argocd/templates/manifests/networking/routes/longhorn-httproute.yaml`. Lives in `longhorn-system` alongside the backend Service, matching the argocd / keycloak HTTPRoute layout (route + backend in the same namespace, attaching to the gateway in `envoy-gateway-system` via the gateway's `allowedRoutes.namespaces.from: All`):

```yaml
{{ if .LonghornEnabled }}
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: longhorn
  namespace: longhorn-system
  labels:
    app.kubernetes.io/name: longhorn
    app.kubernetes.io/managed-by: nebari-infrastructure-core
spec:
  parentRefs:
    - name: nebari-gateway
      namespace: envoy-gateway-system
      sectionName: https
  hostnames:
    - "longhorn.{{ .Domain }}"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: longhorn-frontend
          port: 8080
{{ end }}
```

No `ReferenceGrant` needed — backendRef is implicitly same-namespace as the HTTPRoute.

#### 3b. New `longhorn-securitypolicy.yaml`

`pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`. Lives in `longhorn-system`, the same namespace as its targetRef HTTPRoute (Envoy Gateway requires same-namespace targetRefs for SecurityPolicy):

```yaml
{{ if .LonghornEnabled }}
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: longhorn-oidc
  namespace: longhorn-system
  labels:
    app.kubernetes.io/name: longhorn
    app.kubernetes.io/managed-by: nebari-infrastructure-core
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      name: longhorn
  oidc:
    provider:
      issuer: "https://keycloak.{{ .Domain }}{{ .KeycloakBasePath }}/realms/nebari"
    clientID: longhorn
    clientSecret:
      name: longhorn-oidc-client-secret
    redirectURL: "https://longhorn.{{ .Domain }}/oauth2/callback"
    logoutPath: "/oauth2/logout"
    scopes:
      - openid
      - profile
      - email
      - groups
{{ end }}
```

The `clientSecret` reference is same-namespace by default — no `namespace:` field, no `ReferenceGrant`. Foundational's `longhorn-oidc-client-secret` Secret in `longhorn-system` (Layer 1e) is what this resolves to.

#### 3c. Extend `gateway-certificate.yaml`

`pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml` — wrap a new dnsName in the conditional:

```yaml
dnsNames:
  - "{{ .Domain }}"
  - "keycloak.{{ .Domain }}"
  - "argocd.{{ .Domain }}"
{{ if .LonghornEnabled }}
  - "longhorn.{{ .Domain }}"
{{ end }}
```

### Conditional Rendering Rules

| Longhorn enabled? | Rendered? |
|---|---|
| No | Nothing Longhorn-UI-related ships |
| Yes | HTTPRoute + SecurityPolicy + cert dnsName + realm-setup snippet + Secrets |

The single switch is `templateData.LonghornEnabled`, mirrored from `infraSettings.LonghornEnabled`. Keycloak is mandatory infrastructure in this codebase (it is provisioned unconditionally during foundational install), so the "Longhorn enabled, Keycloak disabled" branch is unreachable by design.

## Data Flow (Request Path)

```
Browser → https://longhorn.<domain>/
   ↓
Envoy Gateway :443
   - TLS terminated using nebari-gateway-tls (cert covers longhorn.<domain>)
   ↓
HTTPRoute longhorn (parentRef nebari-gateway/https, hostname longhorn.<domain>)
   ↓
SecurityPolicy longhorn-oidc (targetRef → HTTPRoute longhorn)
   - No session?    → 302 to keycloak.<domain>/realms/nebari/protocol/openid-connect/auth
                      with clientId=longhorn, redirect_uri=https://longhorn.<domain>/oauth2/callback
                      → user logs in → callback → tokens exchanged → session cookie issued
   - Valid session? → request proceeds
   ↓
Service longhorn-frontend.longhorn-system:8080 → Longhorn UI
```

## Error & Edge Cases

| Situation | Behavior |
|---|---|
| Longhorn helm install fails | Existing error path. HTTPRoute/SecurityPolicy still ship via ArgoCD; route returns 503 until backend exists |
| Realm-setup job fails to create the `longhorn` client | SecurityPolicy stays `Accepted=False`; UI is unreachable until job re-runs. ArgoCD retries the PostSync hook on next sync |
| User hits `longhorn.<domain>` before realm-setup completes | OIDC redirect loop until Keycloak knows the client — same failure mode ArgoCD already has |
| Re-deploy on existing cluster | `nic deploy` regenerates `ClientSecret` each run; realm-setup uses `kcadm create ... \|\| echo "may already exist"` which leaves the existing client's secret untouched. **Known divergence risk** — same as the ArgoCD client today. Out of scope for this spec; track as follow-up |
| `nic destroy` | ArgoCD-managed manifest cleanup removes HTTPRoute / SecurityPolicy. Keycloak client is destroyed with the realm |
| Keycloak `BasePath` differs (e.g. `/auth` for keycloakx legacy chart) | Use `{{ .KeycloakBasePath }}` in the SecurityPolicy issuer URL — same template variable the existing ArgoCD OIDC config uses |

## Testing

### Unit Tests (Go)

| Test | File | Asserts |
|---|---|---|
| `TestFoundationalConfig_LonghornSSOConfig` | `pkg/argocd/foundational_test.go` | `LonghornSSOConfig` field exists; empty `ClientSecret` is treated as disabled |
| `TestCreateKeycloakSecrets_LonghornSecret` | `pkg/argocd/foundational_test.go` | `longhorn-oidc-client-secret` Secret is created in `keycloak` + `longhorn-system` namespaces when `Longhorn.ClientSecret != ""`; not created when empty. `longhorn-system` namespace is also created when needed |
| `TestInfraSettings_LonghornEnabled` | `pkg/provider/aws/provider_test.go`, `pkg/provider/hetzner/provider_test.go` | `InfraSettings().LonghornEnabled` reflects `LonghornEnabled()` on the parsed provider config |
| `TestWriter_RendersLonghornManifests` | `pkg/argocd/writer_test.go` | Table test over `{LonghornEnabled, Domain}` matrix. Asserts presence/absence of HTTPRoute, SecurityPolicy, cert dnsName entry, realm-setup snippet exactly per the conditional rules |
| `TestWriter_LonghornRouteContent` | `pkg/argocd/writer_test.go` | When rendered: hostname `longhorn.<domain>`, backendRef `longhorn-frontend:8080`, parentRef `nebari-gateway`/`https` section |
| `TestWriter_LonghornSecurityPolicyContent` | `pkg/argocd/writer_test.go` | targetRef → `HTTPRoute/longhorn`; issuer `https://keycloak.<domain><BasePath>/realms/nebari`; clientID `longhorn`; redirect URL `https://longhorn.<domain>/oauth2/callback` |
| `TestWriter_GatewayCertIncludesLonghorn` | `pkg/argocd/writer_test.go` | dnsNames contains `longhorn.<domain>` iff `LonghornEnabled` is true |

### Manual Verification (Acceptance)

Smoke test, not automated:

1. `make build && ./nic deploy --config examples/aws-tyler-config.yaml` with Keycloak + Longhorn both enabled
2. `kubectl -n envoy-gateway-system get gateway nebari-gateway -o yaml` → cert SAN includes `longhorn.<domain>`
3. `kubectl -n longhorn-system get httproute longhorn` → `Accepted=True`
4. `kubectl -n longhorn-system get securitypolicy longhorn-oidc -o yaml` → `status.ancestors[].conditions[Accepted]=True`
5. `kubectl -n keycloak logs job/keycloak-realm-setup` shows `Creating Longhorn OIDC client...` success
6. Browser → `https://longhorn.<domain>/` → 302 to Keycloak → log in → land on Longhorn UI

Negative paths:

7. Set `aws.longhorn.enabled: false`, redeploy → no longhorn HTTPRoute / SecurityPolicy / cert dnsName / Keycloak client / Secret

`make check` must pass before commit.

## Out of Scope

- Group-based authorization (restricting to `longhorn-admins`). Groups are created in Keycloak but the SecurityPolicy doesn't enforce a group claim — any authenticated user reaches the UI. Track as follow-up
- Client-secret rotation on redeploy (matches the existing ArgoCD client behavior — track separately)
- Longhorn UI behind a path prefix on a shared subdomain (e.g. `nebari.<domain>/longhorn`) — not requested
