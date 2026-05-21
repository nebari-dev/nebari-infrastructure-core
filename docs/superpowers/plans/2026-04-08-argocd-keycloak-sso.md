# ArgoCD Keycloak SSO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Configure ArgoCD with Keycloak OIDC SSO so users in `argocd-admins` get full admin access and users in `argocd-viewers` get read-only access.

**Architecture:** Extend the existing deploy flow to (1) generate an OIDC client secret upfront, (2) pass it into ArgoCD's Helm values for OIDC config, (3) store it as a K8s secret for the Keycloak realm-setup job, and (4) extend the realm-setup job to create the OIDC client, groups, and group membership.

**Tech Stack:** Go, Helm (ArgoCD chart v9.4.1), Keycloak 24.0 kcadm.sh, Kubernetes fake client for tests

**Spec:** `docs/superpowers/specs/2026-04-08-argocd-keycloak-sso-design.md`
**Issue:** https://github.com/nebari-dev/nebari-infrastructure-core/issues/227

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `pkg/argocd/config.go` | Modify | Add `ConfigWithOIDC()` function |
| `pkg/argocd/config_test.go` | Modify | Add tests for `ConfigWithOIDC()` |
| `pkg/argocd/foundational.go` | Modify | Add `ArgoCDSSOConfig` struct, `argocd-oidc-client-secret` creation |
| `pkg/argocd/foundational_test.go` | Modify | Add tests for new secret and struct |
| `pkg/argocd/install.go` | Modify | Accept `Config` parameter in `Install()` instead of hardcoding `DefaultConfig()` |
| `pkg/argocd/install_test.go` | Verify | Ensure existing tests still pass (no new tests needed - signature change only) |
| `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml` | Modify | Add OIDC client creation, groups, and group membership |
| `cmd/nic/deploy.go` | Modify | Generate client secret, pass OIDC config to `Install()`, wire into `FoundationalConfig` |

---

### Task 1: Add `ConfigWithOIDC()` to config.go (TDD)

**Files:**
- Modify: `pkg/argocd/config_test.go`
- Modify: `pkg/argocd/config.go`

- [ ] **Step 1: Write failing tests for `ConfigWithOIDC`**

Add these table-driven tests to `pkg/argocd/config_test.go`:

```go
func TestConfigWithOIDC(t *testing.T) {
	tests := []struct {
		name            string
		domain          string
		keycloakBasePath string
		clientSecret    string
		wantIssuer      string
		wantURL         string
	}{
		{
			name:            "standard domain with no base path",
			domain:          "nebari.example.com",
			keycloakBasePath: "",
			clientSecret:    "test-secret-123",
			wantIssuer:      "https://keycloak.nebari.example.com/realms/nebari",
			wantURL:         "https://argocd.nebari.example.com",
		},
		{
			name:            "domain with keycloak base path",
			domain:          "nebari.example.com",
			keycloakBasePath: "/auth",
			clientSecret:    "test-secret-456",
			wantIssuer:      "https://keycloak.nebari.example.com/auth/realms/nebari",
			wantURL:         "https://argocd.nebari.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConfigWithOIDC(tt.domain, tt.keycloakBasePath, tt.clientSecret)

			// Should preserve defaults
			if cfg.Version == "" {
				t.Error("Version should not be empty")
			}
			if cfg.Namespace != "argocd" {
				t.Errorf("Namespace = %q, want %q", cfg.Namespace, "argocd")
			}

			// Should still have server.insecure
			configs := cfg.Values["configs"].(map[string]any)
			params := configs["params"].(map[string]any)
			if insecure, ok := params["server.insecure"].(bool); !ok || !insecure {
				t.Error("server.insecure should be true")
			}

			// Check OIDC config in configs.cm
			cm := configs["cm"].(map[string]any)
			if cm["url"] != tt.wantURL {
				t.Errorf("cm.url = %q, want %q", cm["url"], tt.wantURL)
			}
			oidcConfig, ok := cm["oidc.config"].(string)
			if !ok {
				t.Fatal("cm[oidc.config] should be a string")
			}
			if !strings.Contains(oidcConfig, "name: Keycloak") {
				t.Error("oidc.config should contain 'name: Keycloak'")
			}
			if !strings.Contains(oidcConfig, "issuer: "+tt.wantIssuer) {
				t.Errorf("oidc.config should contain issuer %q, got:\n%s", tt.wantIssuer, oidcConfig)
			}
			if !strings.Contains(oidcConfig, "clientID: argocd") {
				t.Error("oidc.config should contain 'clientID: argocd'")
			}
			if !strings.Contains(oidcConfig, "$oidc.keycloak.clientSecret") {
				t.Error("oidc.config should reference $oidc.keycloak.clientSecret")
			}
			if !strings.Contains(oidcConfig, "groups") {
				t.Error("oidc.config should request groups scope")
			}

			// Check RBAC config
			rbac := configs["rbac"].(map[string]any)
			if rbac["policy.default"] != "" {
				t.Errorf("rbac.policy.default = %q, want empty string", rbac["policy.default"])
			}
			if rbac["scopes"] != "[groups]" {
				t.Errorf("rbac.scopes = %q, want %q", rbac["scopes"], "[groups]")
			}
			policyCSV, ok := rbac["policy.csv"].(string)
			if !ok {
				t.Fatal("rbac.policy.csv should be a string")
			}
			if !strings.Contains(policyCSV, "g, argocd-admins, role:admin") {
				t.Error("policy.csv should map argocd-admins to role:admin")
			}
			if !strings.Contains(policyCSV, "g, argocd-viewers, role:readonly") {
				t.Error("policy.csv should map argocd-viewers to role:readonly")
			}

			// Check secret injection
			secret := configs["secret"].(map[string]any)
			extra := secret["extra"].(map[string]any)
			if extra["oidc.keycloak.clientSecret"] != tt.clientSecret {
				t.Errorf("secret.extra[oidc.keycloak.clientSecret] = %q, want %q",
					extra["oidc.keycloak.clientSecret"], tt.clientSecret)
			}
		})
	}
}
```

You'll need to add `"strings"` to the imports at the top of the test file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestConfigWithOIDC -v`
Expected: compilation error - `ConfigWithOIDC` undefined

- [ ] **Step 3: Implement `ConfigWithOIDC` in `config.go`**

Add this function after `DefaultConfig()` in `pkg/argocd/config.go`. You'll need to add `"fmt"` to the imports.

```go
// ConfigWithOIDC returns an Argo CD configuration with Keycloak OIDC SSO enabled.
// It builds on DefaultConfig and adds OIDC provider config, RBAC policies mapping
// Keycloak groups to ArgoCD roles, and the client secret.
//
// The OIDC config references the client secret via $oidc.keycloak.clientSecret,
// which ArgoCD resolves from the argocd-secret Kubernetes Secret. The secret value
// is injected via configs.secret.extra in the Helm values.
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestConfigWithOIDC -v`
Expected: PASS

- [ ] **Step 5: Run all config tests to ensure no regressions**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestConfig -v`
Expected: PASS (both `TestDefaultConfig` and `TestConfigWithOIDC`)

- [ ] **Step 6: Commit**

```bash
git add pkg/argocd/config.go pkg/argocd/config_test.go
git commit -m "feat: add ConfigWithOIDC for ArgoCD Keycloak OIDC SSO (#227)"
```

---

### Task 2: Add `ArgoCDSSOConfig` and OIDC client secret to foundational.go (TDD)

**Files:**
- Modify: `pkg/argocd/foundational_test.go`
- Modify: `pkg/argocd/foundational.go`

- [ ] **Step 1: Write failing tests for the new struct and secret**

Add these tests to `pkg/argocd/foundational_test.go`:

```go
func TestArgoCDSSOConfigDefaults(t *testing.T) {
	cfg := ArgoCDSSOConfig{}
	if cfg.ClientSecret != "" {
		t.Error("ArgoCDSSOConfig.ClientSecret should default to empty")
	}
}

func TestCreateKeycloakSecrets_CreatesArgoCDOIDCSecret(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "keycloak",
		},
	}
	client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

	keycloakCfg := KeycloakConfig{
		Enabled:               true,
		AdminUsername:         "admin",
		AdminPassword:         "admin-pass",
		DBPassword:            "db-pass",
		PostgresAdminPassword: "pg-admin-pass",
		PostgresUserPassword:  "pg-user-pass",
		RealmAdminUsername:    "admin",
		RealmAdminPassword:    "realm-admin-pass",
	}
	argocdSSO := ArgoCDSSOConfig{
		ClientSecret: "argocd-oidc-secret-value",
	}

	err := createKeycloakSecrets(ctx, client, keycloakCfg, argocdSSO)
	if err != nil {
		t.Fatalf("createKeycloakSecrets() error = %v", err)
	}

	// Verify argocd-oidc-client-secret was created
	secret, err := client.CoreV1().Secrets("keycloak").Get(ctx, "argocd-oidc-client-secret", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get argocd-oidc-client-secret: %v", err)
	}
	if got := getSecretValue(secret, "client-secret"); got != "argocd-oidc-secret-value" {
		t.Errorf("client-secret = %q, want %q", got, "argocd-oidc-secret-value")
	}
	// Verify labels
	if secret.Labels["app.kubernetes.io/part-of"] != "nebari-foundational" {
		t.Error("missing or incorrect part-of label")
	}
}

func TestCreateKeycloakSecrets_SkipsArgoCDSecretWhenEmpty(t *testing.T) {
	ctx := context.Background()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "keycloak",
		},
	}
	client := fake.NewSimpleClientset(ns) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

	keycloakCfg := KeycloakConfig{
		Enabled:       true,
		AdminUsername: "admin",
		AdminPassword: "admin-pass",
		DBPassword:    "db-pass",
	}
	argocdSSO := ArgoCDSSOConfig{
		ClientSecret: "", // Empty - should not create secret
	}

	err := createKeycloakSecrets(ctx, client, keycloakCfg, argocdSSO)
	if err != nil {
		t.Fatalf("createKeycloakSecrets() error = %v", err)
	}

	// Verify argocd-oidc-client-secret was NOT created
	_, err = client.CoreV1().Secrets("keycloak").Get(ctx, "argocd-oidc-client-secret", metav1.GetOptions{})
	if err == nil {
		t.Error("argocd-oidc-client-secret should not be created when ClientSecret is empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestArgoCDSSO -v`
Expected: compilation error - `ArgoCDSSOConfig` undefined

- [ ] **Step 3: Add `ArgoCDSSOConfig` struct and update `createKeycloakSecrets` signature**

In `pkg/argocd/foundational.go`:

Add the new struct after `MetalLBConfig`:

```go
// ArgoCDSSOConfig holds ArgoCD SSO configuration
type ArgoCDSSOConfig struct {
	ClientSecret string // Pre-generated OIDC client secret for ArgoCD's Keycloak integration
}
```

Add the `ArgoCD` field to `FoundationalConfig` (after the `Keycloak` field):

```go
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig

	// ArgoCD SSO configuration
	ArgoCD ArgoCDSSOConfig

	// LandingPage configuration
	LandingPage LandingPageConfig

	// MetalLB configuration (local deployments only)
	MetalLB MetalLBConfig
}
```

Update `createKeycloakSecrets` to accept the new parameter. Change the signature on line 223 from:

```go
func createKeycloakSecrets(ctx context.Context, client kubernetes.Interface, keycloakCfg KeycloakConfig) error {
```

to:

```go
func createKeycloakSecrets(ctx context.Context, client kubernetes.Interface, keycloakCfg KeycloakConfig, argocdSSO ArgoCDSSOConfig) error {
```

Add the ArgoCD OIDC client secret creation at the end of `createKeycloakSecrets`, before the final `return nil` (after the realm admin secret block, around line 289):

```go
	// 5. Create ArgoCD OIDC client secret (used by realm-setup job to configure the Keycloak client)
	if argocdSSO.ClientSecret != "" {
		if err := createSecret(ctx, client, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argocd-oidc-client-secret",
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/part-of":    "nebari-foundational",
					"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"client-secret": argocdSSO.ClientSecret,
			},
		}); err != nil {
			return err
		}
	}
```

Update the call site in `InstallFoundationalServices` (line 125) from:

```go
		if err := createKeycloakSecrets(ctx, k8sClient, foundationalCfg.Keycloak); err != nil {
```

to:

```go
		if err := createKeycloakSecrets(ctx, k8sClient, foundationalCfg.Keycloak, foundationalCfg.ArgoCD); err != nil {
```

- [ ] **Step 4: Fix existing tests to match new signature**

The existing `TestCreateKeycloakSecrets` tests in `foundational_test.go` call `createKeycloakSecrets` with the old 3-argument signature. Update each call to pass an empty `ArgoCDSSOConfig{}` as the fourth argument.

Find all calls matching `createKeycloakSecrets(ctx, client, cfg)` and change to `createKeycloakSecrets(ctx, client, cfg, ArgoCDSSOConfig{})`.

There are 4 call sites in the existing tests (in the subtests: "creates all secrets", "creates realm admin secret when password provided", "skips realm admin secret when password empty", "does not overwrite existing secrets").

- [ ] **Step 5: Run all foundational tests**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestCreateKeycloakSecrets -v`
Expected: PASS (both old and new tests)

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestArgoCDSSO -v`
Expected: PASS

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestFoundationalConfig -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/argocd/foundational.go pkg/argocd/foundational_test.go
git commit -m "feat: add ArgoCDSSOConfig and OIDC client secret creation (#227)"
```

---

### Task 3: Update `Install()` to accept a `Config` parameter

**Files:**
- Modify: `pkg/argocd/install.go:22,78`
- Modify: `cmd/nic/deploy.go:169`

- [ ] **Step 1: Change `Install()` signature to accept Config**

In `pkg/argocd/install.go`, change the `Install` function signature on line 22 from:

```go
func Install(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider) error {
```

to:

```go
func Install(ctx context.Context, cfg *config.NebariConfig, prov provider.Provider, argoCDCfg Config) error {
```

On line 78, replace:

```go
	// Get Argo CD configuration
	argoCDCfg := DefaultConfig()
```

with nothing (delete both lines). The variable `argoCDCfg` is now the parameter.

- [ ] **Step 2: Update the call site in `deploy.go`**

In `cmd/nic/deploy.go`, line 169, change:

```go
		if err := argocd.Install(ctx, cfg, provider); err != nil {
```

to:

```go
		if err := argocd.Install(ctx, cfg, provider, argocd.DefaultConfig()); err != nil {
```

This is a temporary passthrough - Task 5 will change it to use `ConfigWithOIDC`.

- [ ] **Step 3: Run all tests to verify no regressions**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./... -v -count=1 2>&1 | tail -30`
Expected: PASS (all packages)

- [ ] **Step 4: Commit**

```bash
git add pkg/argocd/install.go cmd/nic/deploy.go
git commit -m "refactor: accept Config parameter in argocd.Install() (#227)"
```

---

### Task 4: Extend realm-setup job with OIDC client, groups, and membership

**Files:**
- Modify: `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`

- [ ] **Step 1: Add new environment variables to the job container**

In `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`, add these environment variables after the existing `KEYCLOAK_URL` env var (after line 32):

```yaml
            - name: ARGOCD_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: argocd-oidc-client-secret
                  key: client-secret
            - name: DOMAIN
              value: {{ .Domain }}
```

- [ ] **Step 2: Add OIDC client creation, group creation, and membership to the script**

Append the following to the bash script in the job, before the final `echo "Realm setup complete!"` line (before line 110):

```bash
              echo "Creating ArgoCD OIDC client..."
              $KCADM create clients -r nebari \
                -s clientId=argocd \
                -s enabled=true \
                -s protocol=openid-connect \
                -s publicClient=false \
                -s "secret=$ARGOCD_CLIENT_SECRET" \
                -s "redirectUris=[\"https://argocd.$DOMAIN/auth/callback\"]" \
                -s directAccessGrantsEnabled=false \
                -s standardFlowEnabled=true || echo "Client may already exist"

              # Add groups scope to argocd client as a default scope
              ARGOCD_CLIENT_ID=$($KCADM get clients -r nebari --fields id,clientId | \
                grep -B1 '"clientId" *: *"argocd"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

              if [ -n "$ARGOCD_CLIENT_ID" ] && [ -n "$GROUPS_SCOPE_ID" ]; then
                echo "Adding groups scope to argocd client..."
                $KCADM update clients/$ARGOCD_CLIENT_ID/default-client-scopes/$GROUPS_SCOPE_ID -r nebari || true
              fi

              echo "Creating ArgoCD access groups..."
              $KCADM create groups -r nebari -s name=argocd-admins || echo "Group may already exist"
              $KCADM create groups -r nebari -s name=argocd-viewers || echo "Group may already exist"

              echo "Adding admin user to argocd-admins group..."
              ADMIN_USER_ID=$($KCADM get users -r nebari -q username=admin --fields id | \
                sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')
              ADMINS_GROUP_ID=$($KCADM get groups -r nebari --fields id,name | \
                grep -B1 '"name" *: *"argocd-admins"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

              if [ -n "$ADMIN_USER_ID" ] && [ -n "$ADMINS_GROUP_ID" ]; then
                $KCADM update users/$ADMIN_USER_ID/groups/$ADMINS_GROUP_ID -r nebari \
                  -s realm=nebari -s userId=$ADMIN_USER_ID -s groupId=$ADMINS_GROUP_ID -n || true
              fi
```

- [ ] **Step 3: Verify template renders correctly**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestWriteAllToGit -v`
Expected: PASS (template parsing should succeed)

If there's no such test, run the writer tests:

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -run TestWriter -v`

If that also doesn't exist, run all argocd tests:

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -v`
Expected: PASS (no template parse errors)

- [ ] **Step 4: Commit**

```bash
git add pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml
git commit -m "feat: extend realm-setup job with ArgoCD OIDC client and groups (#227)"
```

---

### Task 5: Wire everything together in deploy.go

**Files:**
- Modify: `cmd/nic/deploy.go:166-209`

- [ ] **Step 1: Update the deploy flow to generate and pass the OIDC client secret**

In `cmd/nic/deploy.go`, replace the ArgoCD install + foundational services block (lines 166-209) with:

```go
	// Install Argo CD (skip in dry-run mode)
	if !deployDryRun {
		slog.Info("Installing Argo CD on cluster")

		// Generate OIDC client secret upfront - needed by both ArgoCD Helm values
		// and the Keycloak realm-setup job
		argoCDClientSecret := generateSecurePassword(rand.Reader)

		// Build ArgoCD config with Keycloak OIDC SSO
		argoCDConfig := argocd.ConfigWithOIDC(cfg.Domain, infraSettings.KeycloakBasePath, argoCDClientSecret)

		if err := argocd.Install(ctx, cfg, provider, argoCDConfig); err != nil {
			// Log error but don't fail deployment
			slog.Warn("Failed to install Argo CD", "error", err)
			slog.Warn("You can install Argo CD manually with: helm install argocd argo/argo-cd --namespace argocd --create-namespace")
		} else {
			slog.Info("Argo CD installed successfully")
			argoCDInstalled = true

			// Install foundational services via Argo CD
			slog.Info("Installing foundational services")
			foundationalCfg := argocd.FoundationalConfig{
				Keycloak: argocd.KeycloakConfig{
					Enabled:               true,
					AdminUsername:         "admin",
					AdminPassword:         generateSecurePassword(rand.Reader),
					DBPassword:            generateSecurePassword(rand.Reader),
					PostgresAdminPassword: generateSecurePassword(rand.Reader),
					PostgresUserPassword:  generateSecurePassword(rand.Reader),
					RealmAdminUsername:    "admin",
					RealmAdminPassword:    generateSecurePassword(rand.Reader),
					Hostname:              "", // Will be auto-generated from domain
				},
				ArgoCD: argocd.ArgoCDSSOConfig{
					ClientSecret: argoCDClientSecret,
				},
				// Enable MetalLB only for providers that need it
				MetalLB: argocd.MetalLBConfig{
					Enabled:     infraSettings.NeedsMetalLB,
					AddressPool: infraSettings.MetalLBAddressPool,
				},
			}

			if err := argocd.InstallFoundationalServices(ctx, cfg, provider, foundationalCfg); err != nil {
				// Log warning but don't fail deployment
				slog.Warn("Failed to install foundational services", "error", err)
				slog.Warn("You can install foundational services manually with: kubectl apply -f pkg/foundational/")
			} else {
				slog.Info("Foundational services installed successfully")
				keycloakInstalled = true
			}
		}
	} else {
		slog.Info("Would install Argo CD and foundational services (dry-run mode)")
	}
```

Note: The existing code does not explicitly set `LandingPage.RedisPassword` - it uses the zero value (empty string). Keep this behavior as-is to avoid scope creep.

- [ ] **Step 2: Build to verify compilation**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go build ./...`
Expected: success (no errors)

- [ ] **Step 3: Run all tests**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./... -v -count=1 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/nic/deploy.go
git commit -m "feat: wire ArgoCD OIDC SSO into deploy flow (#227)"
```

---

### Task 6: Update post-deploy instructions

**Files:**
- Modify: `cmd/nic/deploy.go` (the `printArgoCDInstructions` function, lines 437-467)

- [ ] **Step 1: Update ArgoCD instructions to mention SSO login**

Replace the `printArgoCDInstructions` function in `cmd/nic/deploy.go` with:

```go
// printArgoCDInstructions prints instructions for accessing Argo CD
func printArgoCDInstructions(cfg *config.NebariConfig) {
	fmt.Println()
	fmt.Println("===============================================================================")
	fmt.Println("  ARGO CD INSTALLED")
	fmt.Println("===============================================================================")
	fmt.Println()
	fmt.Println("  Argo CD has been successfully installed on your cluster.")
	fmt.Println()
	fmt.Println("  To access Argo CD:")
	fmt.Println()
	if cfg.Domain != "" {
		fmt.Printf("    UI: https://argocd.%s (after DNS configuration)\n", cfg.Domain)
		fmt.Println()
		fmt.Println("  Or use port-forwarding:")
		fmt.Println()
	}
	fmt.Println("    kubectl port-forward svc/argocd-server -n argocd 8080:443")
	fmt.Println("    Then visit: https://localhost:8080")
	fmt.Println()
	fmt.Println("  SSO Login:")
	fmt.Println("    Click 'Log in via Keycloak' to authenticate with your Nebari account.")
	fmt.Println("    Users in the 'argocd-admins' group get full admin access.")
	fmt.Println("    Users in the 'argocd-viewers' group get read-only access.")
	fmt.Println()
	fmt.Println("  Admin fallback (break-glass):")
	fmt.Println()
	fmt.Println("    kubectl -n argocd get secret argocd-initial-admin-secret \\")
	fmt.Println("      -o jsonpath=\"{.data.password}\" | base64 -d")
	fmt.Println()
	fmt.Println("    Username: admin")
	fmt.Println("    Password: <from command above>")
	fmt.Println()
	fmt.Println("===============================================================================")
	fmt.Println()
}
```

- [ ] **Step 2: Build to verify**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go build ./...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add cmd/nic/deploy.go
git commit -m "docs: update ArgoCD post-deploy instructions with SSO info (#227)"
```

---

### Task 7: Final verification

- [ ] **Step 1: Run full test suite**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./... -v -cover 2>&1 | tail -40`
Expected: PASS on all packages

- [ ] **Step 2: Run linter**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && golangci-lint run`
Expected: no errors

- [ ] **Step 3: Run go vet**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go vet ./...`
Expected: no errors

- [ ] **Step 4: Verify template rendering**

Run: `cd /home/chuck/devel/nebari-infrastructure-core && go test ./pkg/argocd/ -v -run Test`
Expected: all PASS, confirming templates parse correctly with new variables
