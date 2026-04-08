# ArgoCD Keycloak SSO Design

**Issue:** https://github.com/nebari-dev/nebari-infrastructure-core/issues/227
**Date:** 2026-04-08

## Problem

ArgoCD is deployed with only the built-in admin account. Nebari users who have Keycloak accounts cannot access ArgoCD without being given the admin password. There is no group-based access control.

## Design

Integrate ArgoCD with Keycloak OIDC as part of the initial deploy flow. Two Keycloak groups control access:

- `argocd-admins` - mapped to ArgoCD `role:admin` (full access)
- `argocd-viewers` - mapped to ArgoCD `role:readonly` (read-only)

The realm admin user created during setup is added to `argocd-admins`.

### Architecture

The integration touches three layers, all executed during `nic deploy`:

```
Go (foundational.go)          Keycloak (realm-setup-job)       ArgoCD (Helm values)
─────────────────────          ──────────────────────────       ────────────────────
Generate client secret   -->   Create OIDC client with    -->  OIDC config referencing
Store in argocd-secret         pre-generated secret             $oidc.keycloak.clientSecret
                               Create groups                    RBAC policy.csv mapping
                               Add admin to argocd-admins       groups to roles
```

### Layer 1: Go Code Changes

#### 1a. New secret in `foundational.go`

Add a new secret `argocd-oidc-secret` to the `argocd` namespace containing the pre-generated OIDC client secret. This secret is created **before** ArgoCD is installed, so ArgoCD's Helm values can reference it.

However, ArgoCD's `$variable` secret reference syntax only works with the `argocd-secret` Secret (or secrets labeled `app.kubernetes.io/part-of: argocd`). The simplest approach is to inject the client secret directly into ArgoCD's Helm values via `configs.secret.extra`, which adds it to the `argocd-secret` Secret.

**Changes to `FoundationalConfig`:**

```go
type FoundationalConfig struct {
    Keycloak    KeycloakConfig
    ArgoCD      ArgoCDSSOConfig  // NEW
    LandingPage LandingPageConfig
    MetalLB     MetalLBConfig
}

type ArgoCDSSOConfig struct {
    ClientSecret string // Pre-generated OIDC client secret for ArgoCD
}
```

**Changes to `deploy.go`:**

Generate the client secret alongside other passwords:

```go
foundationalCfg := argocd.FoundationalConfig{
    // ... existing fields ...
    ArgoCD: argocd.ArgoCDSSOConfig{
        ClientSecret: generateSecurePassword(rand.Reader),
    },
}
```

Also store it as a Kubernetes secret in the `keycloak` namespace so the realm-setup job can read it:

```go
// In createKeycloakSecrets() or a new createArgoCDSecrets():
createSecret(ctx, client, &corev1.Secret{
    ObjectMeta: metav1.ObjectMeta{
        Name:      "argocd-oidc-client-secret",
        Namespace: KeycloakDefaultNamespace,
    },
    Type: corev1.SecretTypeOpaque,
    StringData: map[string]string{
        "client-secret": foundationalCfg.ArgoCD.ClientSecret,
    },
})
```

#### 1b. ArgoCD Helm values in `config.go`

The `DefaultConfig()` function currently only sets `server.insecure: true`. It needs to be extended to accept OIDC parameters. Since the Keycloak issuer URL and client secret are runtime values (they depend on the domain and generated password), `DefaultConfig()` should accept these as parameters, or a new function should build the complete config.

**New function - `ConfigWithOIDC`:**

```go
func ConfigWithOIDC(domain, keycloakBasePath, clientSecret string) Config {
    cfg := DefaultConfig()

    issuerURL := fmt.Sprintf("https://keycloak.%s%s/realms/nebari", domain, keycloakBasePath)
    argocdURL := fmt.Sprintf("https://argocd.%s", domain)

    oidcConfig := fmt.Sprintf(`name: Keycloak
issuer: %s
clientID: argocd
clientSecret: $oidc.keycloak.clientSecret
requestedScopes:
  - openid
  - profile
  - email
  - groups`, issuerURL)

    rbacPolicy := `g, argocd-admins, role:admin
g, argocd-viewers, role:readonly`

    configs := cfg.Values["configs"].(map[string]any)
    configs["cm"] = map[string]any{
        "url":         argocdURL,
        "oidc.config": oidcConfig,
    }
    configs["rbac"] = map[string]any{
        "policy.default": "",
        "scopes":         "[groups]",
        "policy.csv":     rbacPolicy,
    }
    configs["secret"] = map[string]any{
        "extra": map[string]any{
            "oidc.keycloak.clientSecret": clientSecret,
        },
    }

    cfg.Values["configs"] = configs
    return cfg
}
```

**Note:** `policy.default` is set to `""` (empty string) so only users in the two groups get access. Users who authenticate via SSO but aren't in either group will be denied.

#### 1c. Deploy flow changes

In `deploy.go`, the ArgoCD install call currently uses `DefaultConfig()` implicitly (via `argocd.Install()`). The install function needs to accept the OIDC-aware config.

Looking at the current flow:
1. `argocd.Install(ctx, cfg, provider)` - installs ArgoCD via Helm
2. `argocd.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg)` - creates secrets, applies root app-of-apps

The OIDC client secret needs to be:
- Passed to ArgoCD's Helm values (step 1)
- Stored as a K8s secret for the realm-setup job (step 2)

So the client secret must be generated **before** step 1. The deploy flow becomes:

```go
// Generate all passwords upfront
argoCDClientSecret := generateSecurePassword(rand.Reader)

// Install ArgoCD with OIDC config
argoCDConfig := argocd.ConfigWithOIDC(cfg.Domain, infraSettings.KeycloakBasePath, argoCDClientSecret)
argocd.InstallHelm(ctx, kubeconfigBytes, argoCDConfig)

// Install foundational services (creates secrets including the OIDC client secret for realm-setup)
foundationalCfg := argocd.FoundationalConfig{
    ArgoCD: argocd.ArgoCDSSOConfig{
        ClientSecret: argoCDClientSecret,
    },
    // ... rest unchanged
}
argocd.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg)
```

### Layer 2: Keycloak Realm Setup Job

Extend `realm-setup-job.yaml` to create the OIDC client and groups after realm creation.

**New environment variable:**

```yaml
- name: ARGOCD_CLIENT_SECRET
  valueFrom:
    secretKeyRef:
      name: argocd-oidc-client-secret
      key: client-secret
```

**New script sections (appended to existing script):**

```bash
echo "Creating ArgoCD OIDC client..."
$KCADM create clients -r nebari \
  -s clientId=argocd \
  -s enabled=true \
  -s protocol=openid-connect \
  -s publicClient=false \
  -s secret="$ARGOCD_CLIENT_SECRET" \
  -s 'redirectUris=["https://argocd.'"$DOMAIN"'/auth/callback"]' \
  -s directAccessGrantsEnabled=false \
  -s standardFlowEnabled=true || echo "Client may already exist"

# Add groups scope to argocd client as a default scope
ARGOCD_CLIENT_ID=$($KCADM get clients -r nebari --fields id,clientId | \
  grep -B1 '"clientId" *: *"argocd"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

if [ -n "$ARGOCD_CLIENT_ID" ] && [ -n "$GROUPS_SCOPE_ID" ]; then
  echo "Adding groups scope to argocd client..."
  $KCADM update clients/$ARGOCD_CLIENT_ID/default-client-scopes/$GROUPS_SCOPE_ID -r nebari || true
fi

echo "Creating ArgoCD groups..."
$KCADM create groups -r nebari -s name=argocd-admins || echo "Group may already exist"
$KCADM create groups -r nebari -s name=argocd-viewers || echo "Group may already exist"

echo "Adding admin user to argocd-admins group..."
ADMIN_USER_ID=$($KCADM get users -r nebari -q username=admin --fields id | \
  sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')
ADMINS_GROUP_ID=$($KCADM get groups -r nebari --fields id,name | \
  grep -B1 '"name" *: *"argocd-admins"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

if [ -n "$ADMIN_USER_ID" ] && [ -n "$ADMINS_GROUP_ID" ]; then
  $KCADM update users/$ADMIN_USER_ID/groups/$ADMINS_GROUP_ID -r nebari -s realm=nebari -s userId=$ADMIN_USER_ID -s groupId=$ADMINS_GROUP_ID -n || true
fi
```

**New environment variable for domain** (needed for redirect URI):

```yaml
- name: DOMAIN
  value: {{ .Domain }}
```

### Layer 3: ArgoCD RBAC Configuration

Handled entirely through Helm values (see Layer 1b above). The key settings:

| Helm Value | Value | Purpose |
|------------|-------|---------|
| `configs.rbac.policy.default` | `""` | No access for users not in a group |
| `configs.rbac.scopes` | `[groups]` | Use the `groups` claim from OIDC token |
| `configs.rbac.policy.csv` | See below | Map groups to roles |

**policy.csv:**
```
g, argocd-admins, role:admin
g, argocd-viewers, role:readonly
```

### Ordering and Dependencies

```
Time -->

1. Generate passwords     2. Install ArgoCD           3. Create secrets        4. ArgoCD syncs apps
   (deploy.go)               (Helm with OIDC values)     (foundational.go)       (wave 4: Keycloak)

   argoCDClientSecret -->  configs.secret.extra       argocd-oidc-client-     5. Realm setup job
                           has the secret              secret in keycloak ns      creates OIDC client
                                                                                  with same secret
```

ArgoCD is installed at step 2 with OIDC config pointing to a Keycloak that doesn't exist yet. This is fine because:
- ArgoCD's OIDC discovery is lazy (fetches `.well-known/openid-configuration` only on login attempt)
- The built-in admin account still works for initial access
- Once Keycloak comes up (wave 4) and the realm-setup job completes (PostSync), SSO starts working

### Files to Modify

| File | Change |
|------|--------|
| `pkg/argocd/config.go` | Add `ConfigWithOIDC()` function |
| `pkg/argocd/foundational.go` | Add `ArgoCDSSOConfig` struct, create `argocd-oidc-client-secret` in keycloak namespace |
| `pkg/argocd/install.go` | Accept Config parameter instead of using DefaultConfig() internally |
| `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml` | Add OIDC client, groups, and group membership |
| `pkg/argocd/writer.go` | Add `Domain` to template data if not already available (it is) |
| `cmd/nic/deploy.go` | Generate client secret, pass OIDC config to ArgoCD install, update foundational config |

### Files to Add

| File | Purpose |
|------|---------|
| `pkg/argocd/config_test.go` | Test `ConfigWithOIDC()` generates correct Helm values |
| `pkg/argocd/foundational_test.go` (extend) | Test new secret creation |

### Testing Strategy

**Unit tests:**
- `ConfigWithOIDC()` produces correct Helm values structure (OIDC config, RBAC policy, secret)
- `createKeycloakSecrets()` creates the new `argocd-oidc-client-secret`
- `FoundationalConfig` correctly carries the `ArgoCD.ClientSecret` field

**Manual validation:**
- Deploy a local cluster, verify ArgoCD shows "Log in via Keycloak" button
- Log in as realm admin, verify admin access
- Create a user in `argocd-viewers`, verify read-only access
- Create a user not in either group, verify access denied

### Security Considerations

- The OIDC client secret is generated with `generateSecurePassword()` (32 bytes, base64 encoded) - same strength as other secrets
- The client secret exists in two places: `argocd-secret` (argocd namespace) and `argocd-oidc-client-secret` (keycloak namespace) - both are Opaque secrets
- `publicClient=false` ensures the client secret is required for token exchange
- `directAccessGrantsEnabled=false` prevents password-based token grants
- The built-in ArgoCD admin account remains available as a break-glass mechanism
