# Longhorn UI Group-Based Authorization Design

**Date:** 2026-05-25
**Builds on:** `docs/superpowers/specs/2026-05-22-longhorn-ui-gateway-design.md` (open in PR #328)

## Problem

The current Longhorn UI exposure (`https://longhorn.<domain>`) is gated only by OIDC authentication: any user who can log in to Keycloak can reach the UI and operate on persistent volumes. The Longhorn UI has full destructive control over PVs, so unauthenticated access is dangerous and even authenticated-but-unrestricted access is too broad.

We need to restrict access to members of the `longhorn-admins` Keycloak group.

## Design

Extend the existing `longhorn-securitypolicy.yaml` template with a JWT provider and an authorization rule. The Envoy Gateway `SecurityPolicy` resource will perform three steps for every request in this order: OIDC authentication, JWT validation, and claim-based authorization. Only requests whose validated JWT contains `groups: [..., "longhorn-admins", ...]` are admitted; everything else gets 403.

No new Kubernetes resources, no new Go code, no new template files. The change is contained to a single YAML template and the test that renders it.

### Why this works on the realm side

The existing realm-setup job (`pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`) already does the prerequisite work:

- A `groups` client scope exists in the `nebari` realm with a `group-membership` protocol mapper. The mapper is configured with `"access.token.claim":"true"` (line 118), so a user's group memberships appear in the **access** token, not just the ID token.
- The `groups` scope is attached to the `longhorn` Keycloak client as a default client scope (lines 172-180).
- The `longhorn-admins` group exists (line 182) and the realm admin user is a member of it (lines 185-191).

No realm-setup changes are required for this design.

### Architecture

```
Browser → https://longhorn.<domain>/
  ↓ TLS terminated by Envoy Gateway
  ↓
HTTPRoute longhorn (longhorn-system, unchanged)
  ↓
SecurityPolicy longhorn-oidc filters, in order:

  1. OIDC filter
     - no session?   → 302 to Keycloak login → callback → exchange code for tokens → session cookie
     - has session?  → Envoy reads access_token from cookie and sets
                       Authorization: Bearer <access_token> on the request

  2. JWT filter (provider name: keycloak)
     - validates iss claim against the external issuer URL
       (https://keycloak.<domain><base-path>/realms/nebari)
     - fetches JWKS from the in-cluster KeycloakServiceURL
       (http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080<base-path>/realms/nebari/protocol/openid-connect/certs)
     - parses claims, attaches them as a principal to the filter chain

  3. Authorization filter
     - defaultAction: Deny
     - Allow iff principal.jwt.claims.groups (StringArray) contains "longhorn-admins"
  ↓ (only if allowed)
Service longhorn-frontend.longhorn-system:8080
```

The JWKS fetch deliberately uses the in-cluster service URL rather than the public hostname. Validating tokens whose issuer is `https://keycloak.<domain>` by fetching JWKS at that same hostname would force Envoy to resolve its own gateway domain and TLS-terminate against itself. Decoupling the iss-claim URL from the JWKS-fetch URL is the same pattern that `nebari-landingpage`'s oauth2-proxy already uses (`KeycloakIssuerURL` for `iss`, `KeycloakServiceURL` for JWKS).

### Template Change

`pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`. The current body (after PR #328) is:

```yaml
{{- if .LonghornEnabled }}
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
{{- end }}
```

The new body adds `forwardAccessToken: true`, a `jwt` block, and an `authorization` block:

```yaml
{{- if .LonghornEnabled }}
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
    forwardAccessToken: true
  jwt:
    providers:
      - name: keycloak
        issuer: "https://keycloak.{{ .Domain }}{{ .KeycloakBasePath }}/realms/nebari"
        remoteJWKS:
          uri: "{{ .KeycloakServiceURL }}/realms/nebari/protocol/openid-connect/certs"
  authorization:
    defaultAction: Deny
    rules:
      - name: allow-longhorn-admins
        action: Allow
        principal:
          jwt:
            provider: keycloak
            claims:
              - name: groups
                valueType: StringArray
                values:
                  - longhorn-admins
{{- end }}
```

`{{ .Domain }}`, `{{ .KeycloakBasePath }}`, and `{{ .KeycloakServiceURL }}` are already on `TemplateData` (set in `pkg/argocd/writer.go`'s `NewTemplateData`).

### Failure Modes

| Situation | User-visible effect |
|---|---|
| User authenticates, not in `longhorn-admins` | 403 from the gateway. Valid session, valid JWT, missing required claim. |
| User not yet logged in | 302 to Keycloak (unchanged). |
| JWKS fetch fails (Keycloak down) | 5xx from the gateway; resumes once JWKS becomes reachable. EG caches JWKS in memory. |
| Keycloak signing key rotation | Brief window of 401 possible during rotation; resolves on next JWKS refresh. |
| Token expired between page loads | OIDC filter refreshes transparently; if refresh fails, redirect to login. |
| Groups protocol mapper accidentally removed | All requests 403. Caught immediately by the admin. |

### Filter-Ordering Caveat

This design assumes Envoy Gateway composes filters in the order OIDC → JWT → Authorization within a single SecurityPolicy. The EG Azure Entra task documents this composition (oidc + jwt + authorization in one policy). If ordering turns out to be wrong in practice (symptom: every request 401, never redirected to Keycloak login), the fallback is the oauth2-proxy sidecar pattern (separate workload in `longhorn-system`, HTTPRoute backend changes to oauth2-proxy). Worth keeping in mind but not preemptively engineered around.

### Testing

Single test extension. Augment the positive sub-test of `TestWriteAllToGit_LonghornSecurityPolicy` (already in `pkg/argocd/writer_test.go` from PR #328) with assertions for the new content:

```go
for _, want := range []string{
    // ... existing assertions for kind/name/namespace/oidc fields ...
    "forwardAccessToken: true",
    "jwt:",
    "name: keycloak",
    "/realms/nebari/protocol/openid-connect/certs",
    "authorization:",
    "defaultAction: Deny",
    "name: allow-longhorn-admins",
    "action: Allow",
    "name: groups",
    "valueType: StringArray",
    "longhorn-admins",
} {
    if !strings.Contains(out, want) {
        t.Errorf("longhorn-securitypolicy.yaml missing %q\ngot:\n%s", want, out)
    }
}
```

The negative sub-test (LonghornEnabled=false → file renders empty) is unchanged because the entire body is still wrapped in the existing `{{- if .LonghornEnabled }}` conditional.

No new test files. No new Go code to test.

### Manual Acceptance

Added to the PR #328 description (operator-driven, runs after deploy):

1. Deploy to a real cluster.
2. Sign in to `https://longhorn.<domain>` as the realm admin — the admin is already a member of `longhorn-admins` — should land on the Longhorn UI.
3. Create a fresh Keycloak user via the Keycloak UI, do NOT add them to `longhorn-admins`. Sign in as that user → expect 403 from the gateway.
4. Add the user to `longhorn-admins` via the Keycloak UI. Sign out and back in. → Should now land on the Longhorn UI.

## Out of Scope

- Read-only access for `longhorn-viewers` — Longhorn's UI has no read-only mode, so the viewers group remains a forward-compatibility no-op.
- Per-route claim rules (e.g., gating only mutating endpoints) — Longhorn's UI mixes reads and writes on the same paths.
- Group claim rules for other admin UIs (Grafana, etc.) — separate spec when those land.
- Client-secret rotation on redeploy — pre-existing concern from PR #328, tracked separately.

## Spec Status

This work targets the same branch as PR #328 (`tpotts/longhorn-ui-gateway`). The PR #328 description will need a follow-up update to drop the "Group-based authorization on the SecurityPolicy" entry from its "Follow-ups" section.
