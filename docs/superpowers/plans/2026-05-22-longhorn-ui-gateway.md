# Longhorn UI Gateway Exposure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the Longhorn UI at `https://longhorn.<domain>` through the existing Envoy Gateway, gated by Keycloak OIDC via Envoy Gateway `SecurityPolicy`.

**Architecture:** Mirrors the existing ArgoCD SSO design. The route, SecurityPolicy, and one copy of the client-secret Secret live in `longhorn-system`. A duplicate of the client-secret Secret lives in `keycloak` so the realm-setup job can register the Keycloak client. All Longhorn-UI manifests are conditionally rendered on a single template-data flag (`LonghornEnabled`) computed as `infraSettings.LonghornEnabled && foundationalCfg.Keycloak.Enabled`. When Keycloak is disabled, nothing UI-related ships.

**Tech Stack:** Go 1.21+, Kubernetes Gateway API v1, Envoy Gateway v1.6 `gateway.envoyproxy.io/v1alpha1` SecurityPolicy, Helm, ArgoCD, `text/template` for manifest rendering.

**Spec:** `docs/superpowers/specs/2026-05-22-longhorn-ui-gateway-design.md`

---

## File Structure

**New files:**
- `pkg/argocd/templates/manifests/networking/routes/longhorn-httproute.yaml` — HTTPRoute in `longhorn-system`, hostname `longhorn.<domain>`, backend `longhorn-frontend:8080`
- `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml` — SecurityPolicy in `longhorn-system`, OIDC config pointing at Keycloak
- `docs/superpowers/plans/2026-05-22-longhorn-ui-gateway.md` — this file

**Modified files:**
- `pkg/provider/provider.go` — add `LonghornEnabled bool` to `InfraSettings`
- `pkg/provider/aws/provider.go` — populate `LonghornEnabled` in `InfraSettings()`
- `pkg/provider/hetzner/provider.go` — populate `LonghornEnabled` in `InfraSettings()`
- `pkg/provider/azure/provider.go` — populate `LonghornEnabled` (false) in `InfraSettings()`
- `pkg/provider/gcp/provider.go` — populate `LonghornEnabled` (false) in `InfraSettings()`
- `pkg/provider/local/provider.go` — populate `LonghornEnabled` (false) in `InfraSettings()`
- `pkg/argocd/foundational.go` — add `LonghornSSOConfig`, extend `FoundationalConfig`, create `longhorn-system` namespace + dual Secrets when enabled
- `pkg/argocd/writer.go` — add `LonghornEnabled` field to `TemplateData`, populate it in `NewTemplateData`
- `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml` — add Longhorn client + groups creation (conditional)
- `pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml` — add `longhorn.<domain>` to `dnsNames` (conditional)
- `cmd/nic/deploy.go` — generate Longhorn client secret and pass via `FoundationalConfig.Longhorn`
- `pkg/argocd/foundational_test.go` — tests for new Secret creation paths
- `pkg/argocd/writer_test.go` — tests for new manifests and conditional rendering

---

## Task 1: Add `LonghornEnabled` to `provider.InfraSettings`

**Files:**
- Modify: `pkg/provider/provider.go`

- [ ] **Step 1.1: Add the `LonghornEnabled` field to `InfraSettings`**

Open `pkg/provider/provider.go` and locate the `InfraSettings` struct (around line 26). Add the new field at the end of the struct, just before the closing brace, with a doc comment matching the style of the existing fields:

```go
	// SupportsLocalGitOps indicates whether this provider can use local file:// git repos.
	// True for providers where cluster nodes can access host filesystem paths (local, kind, k3s).
	// Cloud providers (AWS, GCP, Azure) return false - their nodes can't see the dev machine's FS.
	SupportsLocalGitOps bool

	// LonghornEnabled indicates whether the Longhorn distributed block storage
	// (and therefore the Longhorn UI) is deployed by this provider for the given
	// cluster config. Used by the foundational deploy flow to decide whether to
	// expose longhorn.<domain> through the gateway and provision an OIDC client.
	LonghornEnabled bool
}
```

- [ ] **Step 1.2: Verify the package compiles**

Run: `go build ./pkg/provider/...`
Expected: exits 0, no output

- [ ] **Step 1.3: Commit**

```bash
git add pkg/provider/provider.go
git commit -m "feat(provider): add LonghornEnabled flag to InfraSettings"
```

---

## Task 2: Populate `LonghornEnabled` in each provider's `InfraSettings()`

**Files:**
- Modify: `pkg/provider/aws/provider.go`
- Modify: `pkg/provider/hetzner/provider.go`
- Modify: `pkg/provider/azure/provider.go`
- Modify: `pkg/provider/gcp/provider.go`
- Modify: `pkg/provider/local/provider.go`
- Modify: `pkg/provider/aws/provider_test.go`
- Modify: `pkg/provider/hetzner/provider_test.go`

- [ ] **Step 2.1: Write a failing test for AWS `LonghornEnabled` reporting**

Append to `pkg/provider/aws/provider_test.go` (place it near other `TestProvider_InfraSettings*` tests if present, otherwise at end of file). Use the exact name and shape below; if a similar test already exists, extend it with the new sub-tests rather than duplicating.

```go
func TestProvider_InfraSettings_LonghornEnabled(t *testing.T) {
	p := NewProvider()

	t.Run("default config reports Longhorn enabled", func(t *testing.T) {
		cfg := &config.ClusterConfig{
			Provider: "amazon_web_services",
			// No provider config block at all → defaults to Longhorn enabled on AWS.
		}
		s := p.InfraSettings(cfg)
		if !s.LonghornEnabled {
			t.Errorf("InfraSettings.LonghornEnabled = false, want true (AWS default)")
		}
	})

	t.Run("explicit Longhorn disabled reports false", func(t *testing.T) {
		cfg := &config.ClusterConfig{
			Provider: "amazon_web_services",
			AmazonWebServices: map[string]any{
				"region": "us-east-1",
				"longhorn": map[string]any{
					"enabled": false,
				},
			},
		}
		// Note: if your local copy of ClusterConfig stores the raw provider config
		// under a different field name (not AmazonWebServices), mirror what other
		// tests in this file already do to set provider config. The key inputs are
		// (a) AWS provider selected and (b) `longhorn.enabled: false` set on the
		// provider block — assembled however the harness does it.
		s := p.InfraSettings(cfg)
		if s.LonghornEnabled {
			t.Errorf("InfraSettings.LonghornEnabled = true, want false")
		}
	})
}
```

If the existing test harness in `provider_test.go` uses a different way to set provider config (e.g. through a builder, a YAML fixture, or by mutating a `*config.NebariConfig` and pulling Cluster off of it), match that style instead — but keep the two sub-tests, the names, and the assertions.

- [ ] **Step 2.2: Run the test, confirm it fails**

Run: `go test -run TestProvider_InfraSettings_LonghornEnabled ./pkg/provider/aws/ -v`
Expected: FAIL — either compile error (`InfraSettings.LonghornEnabled undefined`) or `LonghornEnabled = false, want true`.

- [ ] **Step 2.3: Update `pkg/provider/aws/provider.go` `InfraSettings()` to set the flag**

Locate `func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) provider.InfraSettings` (around line 720) and refactor so the AWS-config parse result is used both for `StorageClass` and the new `LonghornEnabled` field. Replace the function with:

```go
func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) provider.InfraSettings {
	sc := longhorn.StorageClassName
	longhornEnabled := true // AWS default — see Config.LonghornEnabled
	var efsSC string

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg != nil {
		var awsCfg Config
		if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &awsCfg); err == nil {
			longhornEnabled = awsCfg.LonghornEnabled()
			if !longhornEnabled {
				sc = storageClassGP2
			}
			if awsCfg.EFS != nil && awsCfg.EFS.Enabled {
				efsSC = awsCfg.EFSStorageClassName()
			}
		}
	}

	return provider.InfraSettings{
		StorageClass:    sc,
		NeedsMetalLB:    false,
		EFSStorageClass: efsSC,
		LonghornEnabled: longhornEnabled,
		LoadBalancerAnnotations: map[string]string{
			"service.beta.kubernetes.io/aws-load-balancer-type":            "external",
			"service.beta.kubernetes.io/aws-load-balancer-nlb-target-type": "ip",
			"service.beta.kubernetes.io/aws-load-balancer-scheme":          "internet-facing",
		},
	}
}
```

- [ ] **Step 2.4: Re-run the AWS test, confirm it passes**

Run: `go test -run TestProvider_InfraSettings_LonghornEnabled ./pkg/provider/aws/ -v`
Expected: PASS — both sub-tests pass.

- [ ] **Step 2.5: Write and pass the Hetzner equivalent**

Add the same test (renamed appropriately) to `pkg/provider/hetzner/provider_test.go`. Hetzner's `LonghornEnabled()` defaults to true when the `longhorn` block is nil (mirror the AWS pattern). Run the test, confirm it fails, then update `pkg/provider/hetzner/provider.go` `InfraSettings()` to set `LonghornEnabled: hCfg.LonghornEnabled()` (default `true` when no config is parseable, matching the existing fallthrough). Test should now pass.

Updated Hetzner `InfraSettings()` (replace the whole function):

```go
func (p *Provider) InfraSettings(clusterConfig *config.ClusterConfig) provider.InfraSettings {
	settings := provider.InfraSettings{
		StorageClass:    longhorn.StorageClassName,
		NeedsMetalLB:    false,
		LonghornEnabled: true, // Hetzner default — see Config.LonghornEnabled
	}

	rawCfg := clusterConfig.ProviderConfig()
	if rawCfg != nil {
		var hCfg Config
		if err := config.UnmarshalProviderConfig(context.Background(), rawCfg, &hCfg); err == nil {
			if hCfg.Location != "" {
				settings.LoadBalancerAnnotations = map[string]string{
					"load-balancer.hetzner.cloud/location": hCfg.Location,
				}
			}
			settings.LonghornEnabled = hCfg.LonghornEnabled()
			if !settings.LonghornEnabled {
				settings.StorageClass = "hcloud-volumes"
			}
		}
	}

	return settings
}
```

- [ ] **Step 2.6: Set `LonghornEnabled: false` for the remaining providers**

For `pkg/provider/azure/provider.go`, `pkg/provider/gcp/provider.go`, and `pkg/provider/local/provider.go`: locate their `InfraSettings()` methods and add `LonghornEnabled: false,` to the returned `provider.InfraSettings{...}` struct literal. These providers don't yet wire Longhorn — explicitly setting `false` documents the contract.

Example for `pkg/provider/local/provider.go`:

```go
return provider.InfraSettings{
    StorageClass:        "standard",
    NeedsMetalLB:        true,
    MetalLBAddressPool:  ...,
    SupportsLocalGitOps: true,
    LonghornEnabled:     false,
}
```

(Use whatever existing fields the function already returns — just add the new line. If the function returns a `provider.InfraSettings` constructed via several assignments rather than a struct literal, set `settings.LonghornEnabled = false` after the existing assignments.)

- [ ] **Step 2.7: Run the full provider test suite**

Run: `go test ./pkg/provider/...`
Expected: all pass.

- [ ] **Step 2.8: Commit**

```bash
git add pkg/provider/aws/provider.go pkg/provider/aws/provider_test.go \
        pkg/provider/hetzner/provider.go pkg/provider/hetzner/provider_test.go \
        pkg/provider/azure/provider.go pkg/provider/gcp/provider.go pkg/provider/local/provider.go
git commit -m "feat(provider): expose LonghornEnabled per-provider in InfraSettings"
```

---

## Task 3: Add `LonghornSSOConfig` to `FoundationalConfig`

**Files:**
- Modify: `pkg/argocd/foundational.go`

- [ ] **Step 3.1: Add the `LonghornSSOConfig` struct and field on `FoundationalConfig`**

In `pkg/argocd/foundational.go`, locate the `FoundationalConfig` struct (around line 37) and `ArgoCDSSOConfig` (around line 76). Add the new struct right after `ArgoCDSSOConfig`, and the new field on `FoundationalConfig` right after the `ArgoCD` field:

```go
// FoundationalConfig holds configuration for foundational services
type FoundationalConfig struct {
	// Keycloak configuration
	Keycloak KeycloakConfig

	// ArgoCD SSO configuration
	ArgoCD ArgoCDSSOConfig

	// Longhorn UI SSO configuration
	Longhorn LonghornSSOConfig

	// LandingPage configuration
	LandingPage LandingPageConfig

	// MetalLB configuration (local deployments only)
	MetalLB MetalLBConfig
}
```

```go
// LonghornSSOConfig holds Longhorn UI SSO configuration.
// ClientSecret is the pre-generated OIDC client secret used by the Envoy Gateway
// SecurityPolicy that protects longhorn.<domain>. Empty when Longhorn UI exposure
// is disabled — either because Longhorn is not installed or Keycloak is not enabled.
type LonghornSSOConfig struct {
	ClientSecret string
}
```

- [ ] **Step 3.2: Verify the package compiles**

Run: `go build ./pkg/argocd/...`
Expected: exits 0.

- [ ] **Step 3.3: Commit**

```bash
git add pkg/argocd/foundational.go
git commit -m "feat(argocd): add LonghornSSOConfig to FoundationalConfig"
```

---

## Task 4: Create the Longhorn OIDC client-secret Secret in both `keycloak` and `longhorn-system`

**Files:**
- Modify: `pkg/argocd/foundational.go`
- Modify: `pkg/argocd/foundational_test.go`

- [ ] **Step 4.1: Write a failing test for the new Secret-creation behavior**

Open `pkg/argocd/foundational_test.go` and add a new test function. Place it after `TestCreateKeycloakSecrets` (around line 60+) so it lives next to its sibling. The fake k8s client requires namespaces to exist before secrets can be created into them, matching the pattern of the existing tests.

```go
func TestCreateLonghornSecrets(t *testing.T) {
	ctx := context.Background()

	t.Run("creates client-secret in both keycloak and longhorn-system when enabled", func(t *testing.T) {
		nsKC := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "keycloak"}}
		nsLH := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "longhorn-system"}}
		client := fake.NewSimpleClientset(nsKC, nsLH) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		err := createLonghornSecrets(ctx, client, LonghornSSOConfig{ClientSecret: "longhorn-secret-xyz"})
		if err != nil {
			t.Fatalf("createLonghornSecrets() error = %v", err)
		}

		for _, ns := range []string{"keycloak", "longhorn-system"} {
			sec, err := client.CoreV1().Secrets(ns).Get(ctx, "longhorn-oidc-client-secret", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get longhorn-oidc-client-secret in %s: %v", ns, err)
			}
			if got := getSecretValue(sec, "client-secret"); got != "longhorn-secret-xyz" {
				t.Errorf("client-secret in %s = %q, want %q", ns, got, "longhorn-secret-xyz")
			}
		}
	})

	t.Run("creates no secret when ClientSecret is empty", func(t *testing.T) {
		nsKC := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "keycloak"}}
		nsLH := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "longhorn-system"}}
		client := fake.NewSimpleClientset(nsKC, nsLH) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

		err := createLonghornSecrets(ctx, client, LonghornSSOConfig{ClientSecret: ""})
		if err != nil {
			t.Fatalf("createLonghornSecrets() error = %v", err)
		}

		for _, ns := range []string{"keycloak", "longhorn-system"} {
			_, err := client.CoreV1().Secrets(ns).Get(ctx, "longhorn-oidc-client-secret", metav1.GetOptions{})
			if err == nil {
				t.Errorf("expected longhorn-oidc-client-secret to not exist in %s, but it does", ns)
			}
		}
	})
}
```

- [ ] **Step 4.2: Run the test, confirm it fails**

Run: `go test -run TestCreateLonghornSecrets ./pkg/argocd/ -v`
Expected: FAIL — compile error: `createLonghornSecrets undefined`.

- [ ] **Step 4.3: Implement `createLonghornSecrets`**

In `pkg/argocd/foundational.go`, after `createKeycloakSecrets` (around line 323) and before `createLandingPageSecrets`, add a new constant and helper function:

```go
const (
	// LonghornDefaultNamespace is the namespace where Longhorn (and its UI) is deployed.
	LonghornDefaultNamespace = "longhorn-system"

	// LonghornOIDCClientSecretName is the name of the Kubernetes secret holding the
	// pre-generated OIDC client secret for the Longhorn UI Keycloak client. The same
	// value is written into both the keycloak namespace (read by realm-setup-job) and
	// the longhorn-system namespace (read by the SecurityPolicy that fronts the UI).
	LonghornOIDCClientSecretName = "longhorn-oidc-client-secret" //nolint:gosec // Secret name reference, not a credential
)

// createLonghornSecrets writes the OIDC client secret used to protect the
// Longhorn UI into both the keycloak namespace (for realm-setup-job) and the
// longhorn-system namespace (for the Envoy Gateway SecurityPolicy). When
// longhornSSO.ClientSecret is empty, nothing is created.
func createLonghornSecrets(ctx context.Context, client kubernetes.Interface, longhornSSO LonghornSSOConfig) error {
	if longhornSSO.ClientSecret == "" {
		return nil
	}

	for _, ns := range []string{KeycloakDefaultNamespace, LonghornDefaultNamespace} {
		if err := createSecret(ctx, client, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LonghornOIDCClientSecretName,
				Namespace: ns,
				Labels: map[string]string{
					"app.kubernetes.io/part-of":    NebariFoundationalPartOf,
					"app.kubernetes.io/managed-by": "nebari-infrastructure-core",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"client-secret": longhornSSO.ClientSecret,
			},
		}); err != nil {
			return fmt.Errorf("failed to create %s in %s: %w", LonghornOIDCClientSecretName, ns, err)
		}
	}

	return nil
}
```

- [ ] **Step 4.4: Run the test, confirm it passes**

Run: `go test -run TestCreateLonghornSecrets ./pkg/argocd/ -v`
Expected: PASS — both sub-tests pass.

- [ ] **Step 4.5: Commit**

```bash
git add pkg/argocd/foundational.go pkg/argocd/foundational_test.go
git commit -m "feat(argocd): provision longhorn-oidc-client-secret in keycloak and longhorn-system"
```

---

## Task 5: Wire `createLonghornSecrets` into `InstallFoundationalServices`

**Files:**
- Modify: `pkg/argocd/foundational.go`
- Modify: `pkg/argocd/foundational_test.go`

- [ ] **Step 5.1: Write a failing test for the integration**

`InstallFoundationalServices` is harder to test end-to-end (it calls `prov.GetKubeconfig`). Instead test the directly observable behavior: when the Keycloak block calls into the new function with a `LonghornSSOConfig` carrying a secret, both Secrets exist after the call. The simplest test is to reuse the existing approach — exercise the helper sequence directly. Add to `pkg/argocd/foundational_test.go` at the bottom of the file:

```go
func TestCreateKeycloakAndLonghornSecrets_Together(t *testing.T) {
	ctx := context.Background()

	nsKC := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "keycloak"}}
	nsLH := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "longhorn-system"}}
	client := fake.NewSimpleClientset(nsKC, nsLH) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests

	kcCfg := KeycloakConfig{
		Enabled:               true,
		AdminUsername:         "admin",
		AdminPassword:         "kcpw",
		DBPassword:            "dbpw",
		PostgresAdminPassword: "pgadmin",
		PostgresUserPassword:  "pguser",
	}
	argoSSO := ArgoCDSSOConfig{ClientSecret: "argocd-secret-abc"}
	longhornSSO := LonghornSSOConfig{ClientSecret: "longhorn-secret-xyz"}

	if err := createKeycloakSecrets(ctx, client, kcCfg, argoSSO); err != nil {
		t.Fatalf("createKeycloakSecrets() error = %v", err)
	}
	if err := createLonghornSecrets(ctx, client, longhornSSO); err != nil {
		t.Fatalf("createLonghornSecrets() error = %v", err)
	}

	// ArgoCD client secret only lives in keycloak
	if _, err := client.CoreV1().Secrets("keycloak").Get(ctx, "argocd-oidc-client-secret", metav1.GetOptions{}); err != nil {
		t.Errorf("argocd-oidc-client-secret missing from keycloak ns: %v", err)
	}
	// Longhorn client secret lives in both
	for _, ns := range []string{"keycloak", "longhorn-system"} {
		if _, err := client.CoreV1().Secrets(ns).Get(ctx, "longhorn-oidc-client-secret", metav1.GetOptions{}); err != nil {
			t.Errorf("longhorn-oidc-client-secret missing from %s ns: %v", ns, err)
		}
	}
}
```

- [ ] **Step 5.2: Run the test, confirm it passes (no impl change needed yet — the functions exist)**

Run: `go test -run TestCreateKeycloakAndLonghornSecrets_Together ./pkg/argocd/ -v`
Expected: PASS.

- [ ] **Step 5.3: Update `InstallFoundationalServices` to create the namespace + call the helper**

In `pkg/argocd/foundational.go`, locate `InstallFoundationalServices` and the existing Keycloak branch (around lines 120-152). After the existing landing-page secret creation, insert the Longhorn block. Replace the existing Keycloak-enabled `if` block with:

```go
	// 2. Create secrets if Keycloak is enabled
	if foundationalCfg.Keycloak.Enabled {
		// Create Kubernetes client for secret management
		k8sClient, err := newK8sClient(kubeconfigBytes)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Kubernetes client: %w", err)
		}

		// Create namespace for Keycloak
		if err := createNamespace(ctx, k8sClient, KeycloakDefaultNamespace); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak namespace: %w", err)
		}

		// Create secrets for Keycloak and PostgreSQL
		if err := createKeycloakSecrets(ctx, k8sClient, foundationalCfg.Keycloak, foundationalCfg.ArgoCD); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Keycloak secrets: %w", err)
		}

		// Create namespace for Nebari system services
		if err := createNamespace(ctx, k8sClient, NebariSystemNamespace); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create Nebari system namespace: %w", err)
		}

		// Create Redis secret for landing page
		if err := createLandingPageSecrets(ctx, k8sClient, foundationalCfg.LandingPage); err != nil {
			span.RecordError(err)
			return fmt.Errorf("failed to create landing page secrets: %w", err)
		}

		// Create namespace + dual OIDC client-secret Secret for Longhorn UI exposure.
		// No-op when foundationalCfg.Longhorn.ClientSecret == "" (Longhorn disabled or Keycloak off).
		if foundationalCfg.Longhorn.ClientSecret != "" {
			if err := createNamespace(ctx, k8sClient, LonghornDefaultNamespace); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to create Longhorn namespace: %w", err)
			}
			if err := createLonghornSecrets(ctx, k8sClient, foundationalCfg.Longhorn); err != nil {
				span.RecordError(err)
				return fmt.Errorf("failed to create Longhorn secrets: %w", err)
			}
		}
	}
```

- [ ] **Step 5.4: Run the test, and the full argocd test suite**

Run: `go test ./pkg/argocd/...`
Expected: all pass.

- [ ] **Step 5.5: Commit**

```bash
git add pkg/argocd/foundational.go pkg/argocd/foundational_test.go
git commit -m "feat(argocd): provision Longhorn OIDC secrets during foundational install"
```

---

## Task 6: Thread `LonghornEnabled` through the template data

**Files:**
- Modify: `pkg/argocd/writer.go`

- [ ] **Step 6.1: Add `LonghornEnabled` to `TemplateData`**

In `pkg/argocd/writer.go`, locate the `TemplateData` struct (line 36). After `KeycloakAdminSecretNamespace string` (line 71), add:

```go
	// LonghornEnabled is true when the Longhorn UI should be exposed through the
	// gateway. Computed as `InfraSettings.LonghornEnabled && KeycloakConfig.Enabled`
	// — when false, no Longhorn HTTPRoute, SecurityPolicy, cert dnsName entry, or
	// realm-setup snippet is rendered.
	LonghornEnabled bool
```

- [ ] **Step 6.2: Extend `NewTemplateData` to set `LonghornEnabled`**

`NewTemplateData` currently doesn't see the foundational config — only `cfg` and `settings`. The route only renders when Keycloak is also enabled, but `NewTemplateData` doesn't have visibility into Keycloak's enabled-state either. Take the conservative approach: set `LonghornEnabled` from `settings.LonghornEnabled` alone, and depend on `cmd/nic/deploy.go` to gate the actual client-secret provisioning on Keycloak (Task 9). The template manifests will still render when `LonghornEnabled` is true even if Keycloak is off, but in practice Keycloak-off deployments don't reach the deploy-time path that runs the writer at all (foundational install is skipped). If you want the stricter guarantee, see Step 6.2-strict below.

Replace the existing `data := TemplateData{...}` literal in `NewTemplateData` (around line 83) with:

```go
	data := TemplateData{
		Domain:                  cfg.Domain,
		StorageClass:            settings.StorageClass,
		HTTPSPort:               httpsPort,
		MetalLBAddressRange:     settings.MetalLBAddressPool,
		LoadBalancerAnnotations: settings.LoadBalancerAnnotations,
		KeycloakBasePath:        settings.KeycloakBasePath,
		LonghornEnabled:         settings.LonghornEnabled,

		KeycloakNamespace:            KeycloakDefaultNamespace,
		KeycloakServiceName:          keycloakServiceName,
		KeycloakServiceURL:           fmt.Sprintf("http://%s.%s.svc.cluster.local:8080%s", keycloakServiceName, KeycloakDefaultNamespace, settings.KeycloakBasePath),
		KeycloakIssuerURL:            "", // set after Domain is resolved below
		KeycloakRealm:                "nebari",
		KeycloakAdminSecretName:      KeycloakDefaultAdminSecretName,
		KeycloakAdminSecretNamespace: KeycloakDefaultNamespace,
	}
```

**Step 6.2-strict (skip unless reviewer pushes back):** If `LonghornEnabled` must short-circuit when Keycloak is disabled, `cmd/nic/deploy.go` calls `bootstrapGitOps` (which invokes `WriteAllToGit`) *before* checking Keycloak — see `deploy.go:211-217`. Threading a `keycloakEnabled bool` through `bootstrapGitOps → WriteAllToGit → NewTemplateData` is the surgically correct path. For this plan, accept the looser invariant; revisit if review flags it.

- [ ] **Step 6.3: Verify the package compiles and existing tests pass**

Run: `go test ./pkg/argocd/...`
Expected: all pass.

- [ ] **Step 6.4: Commit**

```bash
git add pkg/argocd/writer.go
git commit -m "feat(argocd): thread LonghornEnabled into TemplateData"
```

---

## Task 7: Add the Longhorn HTTPRoute template

**Files:**
- Create: `pkg/argocd/templates/manifests/networking/routes/longhorn-httproute.yaml`
- Modify: `pkg/argocd/writer_test.go`

- [ ] **Step 7.1: Write a failing test for the new HTTPRoute**

Append to `pkg/argocd/writer_test.go`:

```go
func TestWriteAllToGit_LonghornHTTPRoute(t *testing.T) {
	ctx := context.Background()

	t.Run("includes longhorn-httproute when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{
			StorageClass:    "longhorn",
			LonghornEnabled: true,
		}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		routePath := filepath.Join(tmpDir, "manifests", "networking", "routes", "longhorn-httproute.yaml")
		content, err := os.ReadFile(routePath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn route: %v", err)
		}
		out := string(content)

		for _, want := range []string{
			"kind: HTTPRoute",
			"name: longhorn",
			"namespace: longhorn-system",
			"name: nebari-gateway",
			"namespace: envoy-gateway-system",
			"sectionName: https",
			"longhorn.test.example.com",
			"name: longhorn-frontend",
			"port: 8080",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("longhorn-httproute.yaml missing %q\ngot:\n%s", want, out)
			}
		}
	})

	t.Run("omits longhorn-httproute body when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{
			StorageClass:    "gp2",
			LonghornEnabled: false,
		}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		routePath := filepath.Join(tmpDir, "manifests", "networking", "routes", "longhorn-httproute.yaml")
		content, err := os.ReadFile(routePath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn route file: %v", err)
		}
		out := strings.TrimSpace(string(content))
		if out != "" {
			t.Errorf("longhorn-httproute.yaml should render empty when LonghornEnabled=false, got:\n%s", out)
		}
	})
}
```

- [ ] **Step 7.2: Run the test, confirm it fails**

Run: `go test -run TestWriteAllToGit_LonghornHTTPRoute ./pkg/argocd/ -v`
Expected: FAIL — `failed to read longhorn route: ... no such file`.

- [ ] **Step 7.3: Create the HTTPRoute template**

Create `pkg/argocd/templates/manifests/networking/routes/longhorn-httproute.yaml` with:

```yaml
{{- if .LonghornEnabled }}
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
{{- end }}
```

- [ ] **Step 7.4: Run the test, confirm it passes**

Run: `go test -run TestWriteAllToGit_LonghornHTTPRoute ./pkg/argocd/ -v`
Expected: PASS — both sub-tests pass.

- [ ] **Step 7.5: Commit**

```bash
git add pkg/argocd/templates/manifests/networking/routes/longhorn-httproute.yaml pkg/argocd/writer_test.go
git commit -m "feat(argocd): add Longhorn UI HTTPRoute template"
```

---

## Task 8: Add the Longhorn SecurityPolicy template

**Files:**
- Create: `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml`
- Modify: `pkg/argocd/writer_test.go`

- [ ] **Step 8.1: Write a failing test for the new SecurityPolicy**

Append to `pkg/argocd/writer_test.go`:

```go
func TestWriteAllToGit_LonghornSecurityPolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("includes SecurityPolicy when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{
			StorageClass:    "longhorn",
			LonghornEnabled: true,
		}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		policyPath := filepath.Join(tmpDir, "manifests", "networking", "policies", "longhorn-securitypolicy.yaml")
		content, err := os.ReadFile(policyPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn securitypolicy: %v", err)
		}
		out := string(content)

		for _, want := range []string{
			"kind: SecurityPolicy",
			"apiVersion: gateway.envoyproxy.io/v1alpha1",
			"name: longhorn-oidc",
			"namespace: longhorn-system",
			"kind: HTTPRoute",
			"name: longhorn",
			"issuer: \"https://keycloak.test.example.com/realms/nebari\"",
			"clientID: longhorn",
			"name: longhorn-oidc-client-secret",
			"redirectURL: \"https://longhorn.test.example.com/oauth2/callback\"",
			"logoutPath: \"/oauth2/logout\"",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("longhorn-securitypolicy.yaml missing %q\ngot:\n%s", want, out)
			}
		}
	})

	t.Run("renders empty when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{
			StorageClass:    "gp2",
			LonghornEnabled: false,
		}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		policyPath := filepath.Join(tmpDir, "manifests", "networking", "policies", "longhorn-securitypolicy.yaml")
		content, err := os.ReadFile(policyPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn-securitypolicy file: %v", err)
		}
		out := strings.TrimSpace(string(content))
		if out != "" {
			t.Errorf("longhorn-securitypolicy.yaml should render empty when LonghornEnabled=false, got:\n%s", out)
		}
	})
}
```

- [ ] **Step 8.2: Run the test, confirm it fails**

Run: `go test -run TestWriteAllToGit_LonghornSecurityPolicy ./pkg/argocd/ -v`
Expected: FAIL — `no such file or directory: .../policies/longhorn-securitypolicy.yaml`.

- [ ] **Step 8.3: Create the SecurityPolicy template**

Create `pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml` with:

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

- [ ] **Step 8.4: Run the test, confirm it passes**

Run: `go test -run TestWriteAllToGit_LonghornSecurityPolicy ./pkg/argocd/ -v`
Expected: PASS — both sub-tests pass.

- [ ] **Step 8.5: Commit**

```bash
git add pkg/argocd/templates/manifests/networking/policies/longhorn-securitypolicy.yaml pkg/argocd/writer_test.go
git commit -m "feat(argocd): add Longhorn UI SecurityPolicy template"
```

---

## Task 9: Add `longhorn.<domain>` to the gateway certificate

**Files:**
- Modify: `pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml`
- Modify: `pkg/argocd/writer_test.go`

- [ ] **Step 9.1: Read the current certificate template**

Run: `cat pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml`
Note the current dnsNames list (it includes the bare domain, `keycloak.{{ .Domain }}`, `argocd.{{ .Domain }}`).

- [ ] **Step 9.2: Write a failing test**

Append to `pkg/argocd/writer_test.go`:

```go
func TestWriteAllToGit_GatewayCertIncludesLonghorn(t *testing.T) {
	ctx := context.Background()

	t.Run("cert includes longhorn dnsName when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{LonghornEnabled: true}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		certPath := filepath.Join(tmpDir, "manifests", "security", "certificates", "gateway-certificate.yaml")
		content, err := os.ReadFile(certPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read gateway-certificate: %v", err)
		}
		if !strings.Contains(string(content), "longhorn.test.example.com") {
			t.Errorf("expected longhorn.test.example.com in dnsNames, got:\n%s", string(content))
		}
	})

	t.Run("cert does NOT include longhorn dnsName when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{LonghornEnabled: false}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		certPath := filepath.Join(tmpDir, "manifests", "security", "certificates", "gateway-certificate.yaml")
		content, err := os.ReadFile(certPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read gateway-certificate: %v", err)
		}
		if strings.Contains(string(content), "longhorn.test.example.com") {
			t.Errorf("expected NO longhorn.test.example.com in dnsNames, got:\n%s", string(content))
		}
	})
}
```

- [ ] **Step 9.3: Run, confirm it fails**

Run: `go test -run TestWriteAllToGit_GatewayCertIncludesLonghorn ./pkg/argocd/ -v`
Expected: FAIL — first sub-test errors that `longhorn.test.example.com` is missing.

- [ ] **Step 9.4: Edit the cert template**

Open `pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml`. Locate the `dnsNames:` block (it currently lists `"{{ .Domain }}"`, `"keycloak.{{ .Domain }}"`, `"argocd.{{ .Domain }}"`). Add the conditional entry immediately after `argocd.{{ .Domain }}` and before the next top-level key:

```yaml
dnsNames:
  - "{{ .Domain }}"
  - "keycloak.{{ .Domain }}"
  - "argocd.{{ .Domain }}"
{{- if .LonghornEnabled }}
  - "longhorn.{{ .Domain }}"
{{- end }}
```

Use the existing indentation and the `{{- ... -}}` trim form so the rendered YAML has no blank lines when the conditional is false.

- [ ] **Step 9.5: Run, confirm it passes**

Run: `go test -run TestWriteAllToGit_GatewayCertIncludesLonghorn ./pkg/argocd/ -v`
Expected: PASS.

- [ ] **Step 9.6: Commit**

```bash
git add pkg/argocd/templates/manifests/security/certificates/gateway-certificate.yaml pkg/argocd/writer_test.go
git commit -m "feat(argocd): add longhorn.<domain> to gateway certificate dnsNames"
```

---

## Task 10: Extend `realm-setup-job.yaml` to register the Longhorn Keycloak client

**Files:**
- Modify: `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`
- Modify: `pkg/argocd/writer_test.go`

- [ ] **Step 10.1: Write a failing test**

Append to `pkg/argocd/writer_test.go`:

```go
func TestWriteAllToGit_RealmSetupRegistersLonghornClient(t *testing.T) {
	ctx := context.Background()

	t.Run("realm-setup includes Longhorn client creation when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{LonghornEnabled: true}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		jobPath := filepath.Join(tmpDir, "manifests", "keycloak", "realm-setup-job.yaml")
		content, err := os.ReadFile(jobPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read realm-setup-job: %v", err)
		}
		out := string(content)
		for _, want := range []string{
			"LONGHORN_CLIENT_SECRET",
			"longhorn-oidc-client-secret",
			"clientId=longhorn",
			"https://longhorn.$DOMAIN/oauth2/callback",
			"name=longhorn-admins",
			"name=longhorn-viewers",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("realm-setup-job missing %q\nfull contents:\n%s", want, out)
			}
		}
	})

	t.Run("realm-setup does NOT mention Longhorn when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := provider.InfraSettings{LonghornEnabled: false}
		mock := &mockGitClient{workDir: tmpDir}
		if err := WriteAllToGit(ctx, mock, cfg, settings); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		jobPath := filepath.Join(tmpDir, "manifests", "keycloak", "realm-setup-job.yaml")
		content, err := os.ReadFile(jobPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read realm-setup-job: %v", err)
		}
		for _, dontWant := range []string{
			"LONGHORN_CLIENT_SECRET",
			"longhorn-oidc-client-secret",
			"clientId=longhorn",
		} {
			if strings.Contains(string(content), dontWant) {
				t.Errorf("realm-setup-job unexpectedly contains %q when LonghornEnabled=false", dontWant)
			}
		}
	})
}
```

- [ ] **Step 10.2: Run, confirm it fails**

Run: `go test -run TestWriteAllToGit_RealmSetupRegistersLonghornClient ./pkg/argocd/ -v`
Expected: FAIL — first sub-test errors that `LONGHORN_CLIENT_SECRET` is missing.

- [ ] **Step 10.3: Edit the realm-setup-job template**

Open `pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml`. Two edits:

**Edit 1: Add the env var.** Locate the `env:` block (around line 20). After the existing `DOMAIN` env entry (around line 39), insert (using `{{- ... -}}` to keep YAML clean):

```yaml
            - name: DOMAIN
              value: {{ .Domain }}
{{- if .LonghornEnabled }}
            - name: LONGHORN_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: longhorn-oidc-client-secret
                  key: client-secret
{{- end }}
```

**Edit 2: Add the client + group creation block.** Locate the line `echo "Realm setup complete!"` (around line 152, the last line of the script). Just *above* that line, insert:

```yaml
{{- if .LonghornEnabled }}

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

              # Attach groups scope to longhorn client so id_token carries group claims
              LONGHORN_CLIENT_ID=$($KCADM get clients -r nebari --fields id,clientId | \
                grep -B1 '"clientId" *: *"longhorn"' | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')

              if [ -n "$LONGHORN_CLIENT_ID" ] && [ -n "$GROUPS_SCOPE_ID" ]; then
                echo "Adding groups scope to longhorn client..."
                $KCADM update clients/$LONGHORN_CLIENT_ID/default-client-scopes/$GROUPS_SCOPE_ID -r nebari || true
              fi

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
{{- end }}

              echo "Realm setup complete!"
```

`ADMIN_USER_ID` and `GROUPS_SCOPE_ID` are already populated earlier in the script and remain in scope.

- [ ] **Step 10.4: Run the test, confirm it passes**

Run: `go test -run TestWriteAllToGit_RealmSetupRegistersLonghornClient ./pkg/argocd/ -v`
Expected: PASS — both sub-tests pass.

- [ ] **Step 10.5: Run the full argocd suite to catch any unintended damage to the template**

Run: `go test ./pkg/argocd/...`
Expected: all pass.

- [ ] **Step 10.6: Commit**

```bash
git add pkg/argocd/templates/manifests/keycloak/realm-setup-job.yaml pkg/argocd/writer_test.go
git commit -m "feat(argocd): register Longhorn Keycloak client in realm-setup job"
```

---

## Task 11: Generate the Longhorn client secret in `cmd/nic/deploy.go`

**Files:**
- Modify: `cmd/nic/deploy.go`

- [ ] **Step 11.1: Edit the deploy flow**

In `cmd/nic/deploy.go`, locate the foundational config construction (around line 245). Two changes:

**Change 1:** Just before constructing `foundationalCfg`, generate the Longhorn client secret. Place this generation alongside the existing ArgoCD client-secret generation (around line 230):

```go
		// Generate OIDC client secret upfront - needed by both ArgoCD Helm values
		// and the Keycloak realm-setup job
		argoCDClientSecret := generateSecurePassword(rand.Reader)

		// Generate a Longhorn OIDC client secret only when the provider installs
		// Longhorn. When Longhorn is not installed, the SecurityPolicy / HTTPRoute
		// are never rendered, so we don't need a secret. Keycloak being disabled
		// also short-circuits this entire branch (we're already inside the
		// `if !deployDryRun` + argocd-install-succeeded block).
		var longhornClientSecret string
		if infraSettings.LonghornEnabled {
			longhornClientSecret = generateSecurePassword(rand.Reader)
		}
```

**Change 2:** Add the `Longhorn` field to the `FoundationalConfig` literal:

```go
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
				Longhorn: argocd.LonghornSSOConfig{
					ClientSecret: longhornClientSecret,
				},
				LandingPage: argocd.LandingPageConfig{
					RedisPassword: generateSecurePassword(rand.Reader),
				},
				// Enable MetalLB only for providers that need it
				MetalLB: argocd.MetalLBConfig{
					Enabled:     infraSettings.NeedsMetalLB,
					AddressPool: infraSettings.MetalLBAddressPool,
				},
			}
```

- [ ] **Step 11.2: Verify the package compiles**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 11.3: Run the full test suite**

Run: `go test ./...`
Expected: all pass (the `cmd/nic` package may have no tests beyond the existing ones; what matters is no regressions).

- [ ] **Step 11.4: Commit**

```bash
git add cmd/nic/deploy.go
git commit -m "feat(deploy): generate Longhorn OIDC client secret and wire foundational config"
```

---

## Task 12: End-to-end render sanity check + lint pass

**Files:** none

- [ ] **Step 12.1: Render a sample tree to verify no template-syntax escapes**

Write a one-off ad-hoc test (don't commit) only if a previous step's tests didn't already cover the rendered output. The Task 7/8/9/10 tests should already give full coverage. If you want extra confidence, run:

```bash
go test -count=1 ./pkg/argocd/...
```

Expected: all pass.

- [ ] **Step 12.2: Run the project quality gate**

Run: `make check`
Expected: `fmt`, `vet`, `lint`, and `test` all succeed. If `golangci-lint` flags anything in the new code, fix it before continuing.

- [ ] **Step 12.3: Final test sweep across all packages**

Run: `go test -race ./...`
Expected: all pass.

- [ ] **Step 12.4: Commit any final lint/format fixes**

If `make check` produced fixups (e.g., gofmt rewrote a file), commit them:

```bash
git status --short
# review changes
git add -p
git commit -m "chore: lint fixes for longhorn UI gateway exposure"
```

If nothing was changed, skip this step.

---

## Task 13: Manual acceptance test (operator-driven, not automated)

This task documents the manual verification an operator runs once a cluster has been deployed against this branch. It is not part of CI — flag the steps in the PR description so the reviewer/operator runs them.

**Pre-requisites:**
- A cluster config (e.g. `examples/aws-tyler-config.yaml`) with `domain: <some-domain>`, Keycloak enabled (default), and Longhorn enabled (default for AWS).
- DNS A record `*.<domain>` (or at minimum `longhorn.<domain>`) pointing at the gateway LoadBalancer endpoint.

- [ ] **Step 13.1: Deploy**

Run: `./nic deploy --config examples/aws-tyler-config.yaml`
Expected: deployment completes; logs show `Created secret longhorn-oidc-client-secret` twice (once per namespace).

- [ ] **Step 13.2: Verify gateway cert SAN**

Run: `kubectl -n envoy-gateway-system get certificate nebari-gateway-cert -o jsonpath='{.spec.dnsNames}'`
Expected: list includes `longhorn.<domain>`.

- [ ] **Step 13.3: Verify HTTPRoute accepted**

Run: `kubectl -n longhorn-system get httproute longhorn -o jsonpath='{.status.parents[0].conditions[?(@.type=="Accepted")].status}'`
Expected: `True`.

- [ ] **Step 13.4: Verify SecurityPolicy accepted**

Run: `kubectl -n longhorn-system get securitypolicy longhorn-oidc -o jsonpath='{.status.ancestors[0].conditions[?(@.type=="Accepted")].status}'`
Expected: `True`.

- [ ] **Step 13.5: Verify the realm-setup job ran the Longhorn block**

Run: `kubectl -n keycloak logs job/keycloak-realm-setup | grep -i longhorn`
Expected: lines including `Creating Longhorn OIDC client...` and `Creating Longhorn access groups...`, with no fatal `kcadm` errors.

- [ ] **Step 13.6: Browser test**

Open: `https://longhorn.<domain>/`
Expected: redirect to Keycloak login → log in as the realm admin → land on the Longhorn dashboard.

- [ ] **Step 13.7: Negative path — disable Longhorn, redeploy**

Edit the config to add (under the `amazon_web_services:` block):

```yaml
longhorn:
  enabled: false
```

Re-run `./nic deploy --config ...`. Verify:
- `kubectl -n longhorn-system get httproute longhorn` returns `not found`
- `kubectl -n longhorn-system get securitypolicy longhorn-oidc` returns `not found`
- `kubectl -n envoy-gateway-system get certificate nebari-gateway-cert -o jsonpath='{.spec.dnsNames}'` no longer includes `longhorn.<domain>`

---

## Wrap-up

After Task 12 passes, push the branch and open a PR. The branch is `tpotts/longhorn-ui-gateway` (already pre-created in this worktree). Reference the spec doc in the PR description.

**Out of scope for this PR (track separately):**

- Group-based authorization on the SecurityPolicy (only authenticated, not group-gated)
- Client-secret rotation on redeploy (same divergence risk as today's ArgoCD client)
- Strict `LonghornEnabled` gating against Keycloak being disabled (see Step 6.2-strict)
