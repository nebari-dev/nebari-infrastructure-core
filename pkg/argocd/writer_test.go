package argocd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

func TestApplications(t *testing.T) {
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}

	// Should not include _example.yaml (underscore prefix)
	for _, app := range apps {
		if strings.HasPrefix(app, "_") {
			t.Errorf("Applications() included underscore-prefixed file: %s", app)
		}
	}
}

func TestWriteApplication_CertManager(t *testing.T) {
	// Test reading an actual application template
	var buf bytes.Buffer
	ctx := context.Background()

	err := WriteApplication(ctx, &buf, "cert-manager")
	if err != nil {
		t.Fatalf("WriteApplication(cert-manager) error: %v", err)
	}

	content := buf.String()
	if !strings.Contains(content, "kind: Application") {
		t.Error("expected manifest to contain 'kind: Application'")
	}
	if !strings.Contains(content, "apiVersion: argoproj.io/v1alpha1") {
		t.Error("expected manifest to contain ArgoCD API version")
	}
}

func TestWriteApplication_NotFound(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	err := WriteApplication(ctx, &buf, "nonexistent-app")
	if err == nil {
		t.Error("WriteApplication(nonexistent-app) should return error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestWriteAll(t *testing.T) {
	ctx := context.Background()

	// Track what gets written
	written := make(map[string]*bytes.Buffer)
	err := WriteAll(ctx, func(appName string) (io.WriteCloser, error) {
		buf := &bytes.Buffer{}
		written[appName] = buf
		return &nopWriteCloser{buf}, nil
	})

	if err != nil {
		t.Fatalf("WriteAll() error: %v", err)
	}

	// Verify we wrote the expected applications
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}

	if len(written) != len(apps) {
		t.Errorf("WriteAll wrote %d apps, expected %d", len(written), len(apps))
	}

	// Verify each app was written with valid content
	for _, appName := range apps {
		buf, ok := written[appName]
		if !ok {
			t.Errorf("Application %q was not written", appName)
			continue
		}
		content := buf.String()
		if !strings.Contains(content, "kind: Application") {
			t.Errorf("Application %q missing 'kind: Application'", appName)
		}
		if !strings.Contains(content, appName) {
			t.Errorf("Application %q content doesn't contain app name", appName)
		}
	}
}

func TestNewTemplateData_WithInfraSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings provider.InfraSettings
		wantSC   string
		wantLBA  int
		wantKBP  string
		wantMLBA string
	}{
		{
			name:     "aws defaults",
			settings: provider.InfraSettings{StorageClass: "gp2"},
			wantSC:   "gp2",
			wantLBA:  0,
		},
		{
			name: "hetzner with annotations",
			settings: provider.InfraSettings{
				StorageClass:            "hcloud-volumes",
				LoadBalancerAnnotations: map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
			},
			wantSC:  "hcloud-volumes",
			wantLBA: 1,
		},
		{
			name: "local with MetalLB",
			settings: provider.InfraSettings{
				StorageClass:       "standard",
				NeedsMetalLB:       true,
				MetalLBAddressPool: "192.168.1.100-192.168.1.110",
			},
			wantSC:   "standard",
			wantMLBA: "192.168.1.100-192.168.1.110",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{Provider: "test", Domain: "test.example.com"}
			data := NewTemplateData(cfg, tt.settings)
			if data.StorageClass != tt.wantSC {
				t.Errorf("StorageClass = %q, want %q", data.StorageClass, tt.wantSC)
			}
			if len(data.LoadBalancerAnnotations) != tt.wantLBA {
				t.Errorf("LoadBalancerAnnotations count = %d, want %d", len(data.LoadBalancerAnnotations), tt.wantLBA)
			}
			if data.KeycloakBasePath != tt.wantKBP {
				t.Errorf("KeycloakBasePath = %q, want %q", data.KeycloakBasePath, tt.wantKBP)
			}
			if data.MetalLBAddressRange != tt.wantMLBA {
				t.Errorf("MetalLBAddressRange = %q, want %q", data.MetalLBAddressRange, tt.wantMLBA)
			}
		})
	}
}

func TestNewTemplateData_KeycloakServiceURL(t *testing.T) {
	cfg := &config.NebariConfig{Provider: "hetzner", Domain: "test.example.com"}
	settings := provider.InfraSettings{
		StorageClass:     "hcloud-volumes",
		KeycloakBasePath: "/auth",
	}
	data := NewTemplateData(cfg, settings)

	if !strings.HasSuffix(data.KeycloakServiceURL, "/auth") {
		t.Errorf("KeycloakServiceURL = %q, should end with /auth", data.KeycloakServiceURL)
	}

	// Without base path
	settings.KeycloakBasePath = ""
	data = NewTemplateData(cfg, settings)
	if strings.HasSuffix(data.KeycloakServiceURL, "/auth") {
		t.Errorf("KeycloakServiceURL = %q, should NOT end with /auth", data.KeycloakServiceURL)
	}
}

func TestGatewayTemplate_WithAnnotations(t *testing.T) {
	data := TemplateData{
		Domain:   "test.example.com",
		Provider: "hetzner",
		LoadBalancerAnnotations: map[string]string{
			"load-balancer.hetzner.cloud/location": "ash",
		},
		CertificateIssuer: "selfsigned-issuer",
	}

	// Read the gateway template
	content, err := templates.ReadFile("templates/manifests/networking/gateway.yaml")
	if err != nil {
		t.Fatalf("failed to read gateway template: %v", err)
	}

	processed, err := processTemplate("manifests/networking/gateway.yaml", content, data)
	if err != nil {
		t.Fatalf("processTemplate() error: %v", err)
	}

	output := string(processed)

	// Verify the annotations block is present and well-formed
	if !strings.Contains(output, "infrastructure:") {
		t.Error("expected 'infrastructure:' block in rendered gateway")
	}
	if !strings.Contains(output, "annotations:") {
		t.Error("expected 'annotations:' block in rendered gateway")
	}
	if !strings.Contains(output, `load-balancer.hetzner.cloud/location: "ash"`) {
		t.Errorf("expected annotation in rendered gateway, got:\n%s", output)
	}
	if !strings.Contains(output, "kind: Gateway") {
		t.Error("expected 'kind: Gateway' in rendered output")
	}
}

func TestGatewayTemplate_WithoutAnnotations(t *testing.T) {
	data := TemplateData{
		Domain:            "test.example.com",
		Provider:          "aws",
		CertificateIssuer: "selfsigned-issuer",
	}

	content, err := templates.ReadFile("templates/manifests/networking/gateway.yaml")
	if err != nil {
		t.Fatalf("failed to read gateway template: %v", err)
	}

	processed, err := processTemplate("manifests/networking/gateway.yaml", content, data)
	if err != nil {
		t.Fatalf("processTemplate() error: %v", err)
	}

	output := string(processed)

	if strings.Contains(output, "infrastructure:") {
		t.Error("should NOT contain 'infrastructure:' block when no annotations")
	}
	if !strings.Contains(output, "kind: Gateway") {
		t.Error("expected 'kind: Gateway' in rendered output")
	}
}

func TestKeycloakTemplate_HealthProbes(t *testing.T) {
	tests := []struct {
		name             string
		keycloakBasePath string
		wantProbe        string
		wantHostname     string
		wantRelPath      string
	}{
		{
			name:             "empty base path serves at root",
			keycloakBasePath: "",
			wantProbe:        "/health/live",
			wantHostname:     "https://keycloak.test.example.com",
			wantRelPath:      `relativePath: "/"`,
		},
		{
			name:             "auth base path preserves legacy behavior",
			keycloakBasePath: "/auth",
			wantProbe:        "/auth/health/live",
			wantHostname:     "https://keycloak.test.example.com/auth",
			wantRelPath:      `relativePath: "/auth/"`,
		},
	}

	content, err := templates.ReadFile("templates/apps/keycloak.yaml")
	if err != nil {
		t.Fatalf("failed to read keycloak template: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				Domain:                  "test.example.com",
				Provider:                "hetzner",
				KeycloakBasePath:        tt.keycloakBasePath,
				KeycloakNamespace:       "keycloak",
				KeycloakAdminSecretName: "keycloak-admin",
				GitRepoURL:              "https://github.com/example/repo",
				GitBranch:               "main",
			}

			processed, err := processTemplate("apps/keycloak.yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			output := string(processed)

			if !strings.Contains(output, tt.wantProbe) {
				t.Errorf("expected health probe path %q in rendered template, got:\n%s", tt.wantProbe, output)
			}
			if !strings.Contains(output, tt.wantHostname) {
				t.Errorf("expected KC_HOSTNAME to contain %q, got:\n%s", tt.wantHostname, output)
			}
			if !strings.Contains(output, tt.wantRelPath) {
				t.Errorf("expected %q in rendered template, got:\n%s", tt.wantRelPath, output)
			}
			if strings.Contains(output, "//health") {
				t.Error("rendered template contains '//health' - double slash in health probe path")
			}
		})
	}
}

func TestOperatorDeploymentPatch_KeycloakContextPath(t *testing.T) {
	tests := []struct {
		name             string
		keycloakBasePath string
		wantContextPath  string
		wantServiceURL   string
	}{
		{
			name:             "empty base path passes empty context path",
			keycloakBasePath: "",
			wantContextPath:  `value: ""`,
			wantServiceURL:   "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080",
		},
		{
			name:             "auth base path passes /auth context path",
			keycloakBasePath: "/auth",
			wantContextPath:  `value: "/auth"`,
			wantServiceURL:   "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080/auth",
		},
	}

	content, err := templates.ReadFile("templates/manifests/nebari-operator/deployment-patch.yaml")
	if err != nil {
		t.Fatalf("failed to read operator deployment patch: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				KeycloakBasePath:        tt.keycloakBasePath,
				KeycloakServiceURL:      fmt.Sprintf("http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080%s", tt.keycloakBasePath),
				KeycloakNamespace:       "keycloak",
				KeycloakRealm:           "nebari",
				KeycloakAdminSecretName: "keycloak-admin-credentials",
			}

			processed, err := processTemplate("manifests/nebari-operator/deployment-patch.yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			output := string(processed)

			if !strings.Contains(output, "KEYCLOAK_ISSUER_CONTEXT_PATH") {
				t.Error("expected KEYCLOAK_ISSUER_CONTEXT_PATH env var in rendered template")
			}
			if !strings.Contains(output, tt.wantContextPath) {
				t.Errorf("expected context path %q in rendered template, got:\n%s", tt.wantContextPath, output)
			}
			if !strings.Contains(output, tt.wantServiceURL) {
				t.Errorf("expected service URL %q in rendered template, got:\n%s", tt.wantServiceURL, output)
			}
		})
	}
}

func TestLandingPageTemplate(t *testing.T) {
	tests := []struct {
		name              string
		keycloakBasePath  string
		wantIssuerURL     string
		wantOIDCIssuerURL string
	}{
		{
			name:              "no base path",
			keycloakBasePath:  "",
			wantIssuerURL:     "https://keycloak.test.example.com",
			wantOIDCIssuerURL: "https://keycloak.test.example.com/realms/nebari",
		},
		{
			name:              "auth base path included in issuer URL",
			keycloakBasePath:  "/auth",
			wantIssuerURL:     "https://keycloak.test.example.com/auth",
			wantOIDCIssuerURL: "https://keycloak.test.example.com/auth/realms/nebari",
		},
	}

	content, err := templates.ReadFile("templates/apps/nebari-landingpage.yaml")
	if err != nil {
		t.Fatalf("failed to read nebari-landingpage template: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				Domain:                       "test.example.com",
				KeycloakServiceURL:           fmt.Sprintf("http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080%s", tt.keycloakBasePath),
				KeycloakIssuerURL:            tt.wantIssuerURL,
				KeycloakRealm:                "nebari",
				KeycloakAdminSecretName:      "keycloak-admin-credentials",
				KeycloakAdminSecretNamespace: "keycloak",
				GitRepoURL:                   "https://github.com/example/repo",
				GitBranch:                    "main",
			}

			processed, err := processTemplate("apps/nebari-landingpage.yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			output := string(processed)

			if !strings.Contains(output, "kind: Application") {
				t.Error("expected 'kind: Application' in rendered output")
			}
			if !strings.Contains(output, tt.wantIssuerURL) {
				t.Errorf("expected issuer URL %q in rendered output, got:\n%s", tt.wantIssuerURL, output)
			}
			if !strings.Contains(output, tt.wantOIDCIssuerURL) {
				t.Errorf("expected OIDC issuer URL %q in rendered output, got:\n%s", tt.wantOIDCIssuerURL, output)
			}
			if !strings.Contains(output, data.KeycloakServiceURL) {
				t.Errorf("expected in-cluster service URL %q in rendered output, got:\n%s", data.KeycloakServiceURL, output)
			}
			if !strings.Contains(output, "realm: \"nebari\"") {
				t.Error("expected realm 'nebari' in rendered output")
			}
			if !strings.Contains(output, "hostname: \"test.example.com\"") {
				t.Error("expected hostname in rendered output")
			}
			// KeycloakAdminSecretNamespace is a new field; verify it renders to the
			// expected value and not an empty string (which a typo in the template
			// field name would silently produce).
			if !strings.Contains(output, "adminSecretNamespace: \"keycloak\"") {
				t.Error("expected adminSecretNamespace 'keycloak' in rendered output")
			}
			// Ensure no unresolved template placeholders remain
			if strings.Contains(output, "{{") {
				t.Errorf("rendered template still contains unresolved placeholders:\n%s", output)
			}
		})
	}
}

func TestNewTemplateData_KeycloakIssuerURL(t *testing.T) {
	tests := []struct {
		name             string
		domain           string
		keycloakBasePath string
		wantIssuerURL    string
	}{
		{
			name:          "no domain - issuer URL left empty",
			domain:        "",
			wantIssuerURL: "",
		},
		{
			name:          "domain without base path",
			domain:        "test.example.com",
			wantIssuerURL: "https://keycloak.test.example.com",
		},
		{
			name:             "domain with /auth base path",
			domain:           "test.example.com",
			keycloakBasePath: "/auth",
			wantIssuerURL:    "https://keycloak.test.example.com/auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{Provider: "test", Domain: tt.domain}
			settings := provider.InfraSettings{KeycloakBasePath: tt.keycloakBasePath}
			data := NewTemplateData(cfg, settings)

			if data.KeycloakIssuerURL != tt.wantIssuerURL {
				t.Errorf("KeycloakIssuerURL = %q, want %q", data.KeycloakIssuerURL, tt.wantIssuerURL)
			}
		})
	}
}

// nopWriteCloser wraps a bytes.Buffer to satisfy io.WriteCloser
type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

// mockGitClient is a minimal git.Client implementation for testing WriteAllToGit.
type mockGitClient struct {
	workDir string
}

func (m *mockGitClient) ValidateAuth(_ context.Context) error            { return nil }
func (m *mockGitClient) Init(_ context.Context) error                    { return nil }
func (m *mockGitClient) WorkDir() string                                 { return m.workDir }
func (m *mockGitClient) CommitAndPush(_ context.Context, _ string) error { return nil }
func (m *mockGitClient) IsBootstrapped(_ context.Context) (bool, error)  { return false, nil }
func (m *mockGitClient) WriteBootstrapMarker(_ context.Context) error    { return nil }
func (m *mockGitClient) Cleanup() error                                  { return nil }

// Verify mockGitClient implements git.Client at compile time.
var _ git.Client = (*mockGitClient)(nil)

func TestWriteAllToGit_NoOverwriteCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockGitClient{workDir: tmpDir}
	cfg := &config.NebariConfig{
		Provider: "local",
		Domain:   "test.local",
		GitRepository: &git.Config{
			URL:    "https://github.com/test/repo.git",
			Branch: "main",
		},
	}

	ctx := context.Background()

	// First call should create the _nooverwrite_ file
	err := WriteAllToGit(ctx, client, cfg, "")
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// The _nooverwrite_overrides.yaml should have been written as overrides.yaml
	overridesPath := filepath.Join(tmpDir, "manifests", "opentelemetry-collector", "overrides.yaml")
	content, err := os.ReadFile(filepath.Clean(overridesPath))
	if err != nil {
		t.Fatalf("overrides.yaml should exist after first WriteAllToGit, got error: %v", err)
	}

	if !strings.Contains(string(content), "OpenTelemetry Collector overrides") {
		t.Error("overrides.yaml should contain the default content")
	}
}

func TestWriteAllToGit_NoOverwritePreservesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockGitClient{workDir: tmpDir}
	cfg := &config.NebariConfig{
		Provider: "local",
		Domain:   "test.local",
		GitRepository: &git.Config{
			URL:    "https://github.com/test/repo.git",
			Branch: "main",
		},
	}

	ctx := context.Background()

	// Pre-create the overrides file with custom content
	overridesDir := filepath.Join(tmpDir, "manifests", "opentelemetry-collector")
	if err := os.MkdirAll(overridesDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	customContent := []byte("# Custom pack overrides\nconfig:\n  exporters:\n    otlphttp/loki:\n      endpoint: http://loki:3100/otlp\n")
	overridesPath := filepath.Join(overridesDir, "overrides.yaml")
	if err := os.WriteFile(overridesPath, customContent, 0600); err != nil {
		t.Fatalf("failed to write custom overrides: %v", err)
	}

	// WriteAllToGit should NOT overwrite the existing file
	err := WriteAllToGit(ctx, client, cfg, "")
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// Verify the file still has the custom content
	content, err := os.ReadFile(filepath.Clean(overridesPath))
	if err != nil {
		t.Fatalf("failed to read overrides.yaml: %v", err)
	}

	if string(content) != string(customContent) {
		t.Errorf("overrides.yaml was overwritten.\ngot:  %q\nwant: %q", string(content), string(customContent))
	}
}

func TestWriteAllToGit_RendersOtelTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	client := &mockGitClient{workDir: tmpDir}
	cfg := &config.NebariConfig{
		Provider: "local",
		Domain:   "test.local",
		GitRepository: &git.Config{
			URL:    "https://github.com/test/repo.git",
			Branch: "main",
			Path:   "clusters/prod",
		},
	}

	ctx := context.Background()

	err := WriteAllToGit(ctx, client, cfg, "")
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// Verify the OTel app template was rendered with multi-source config
	otelAppPath := filepath.Join(tmpDir, "apps", "opentelemetry-collector.yaml")
	content, err := os.ReadFile(filepath.Clean(otelAppPath))
	if err != nil {
		t.Fatalf("failed to read opentelemetry-collector.yaml: %v", err)
	}

	contentStr := string(content)

	// Should have multi-source (sources: instead of source:)
	if !strings.Contains(contentStr, "sources:") {
		t.Error("opentelemetry-collector.yaml should use multi-source (sources:)")
	}

	// Should reference the GitOps repo
	if !strings.Contains(contentStr, "https://github.com/test/repo.git") {
		t.Error("opentelemetry-collector.yaml should reference the GitOps repo URL")
	}

	// Should reference the base values file with the correct git path
	if !strings.Contains(contentStr, "$values/clusters/prod/manifests/opentelemetry-collector/values.yaml") {
		t.Error("opentelemetry-collector.yaml should reference the base values file with correct git path")
	}

	// Should reference the overrides file with the correct git path
	if !strings.Contains(contentStr, "$values/clusters/prod/manifests/opentelemetry-collector/overrides.yaml") {
		t.Error("opentelemetry-collector.yaml should reference the overrides file with correct git path")
	}

	// Overrides must come after base values so they take precedence
	valuesIdx := strings.Index(contentStr, "$values/clusters/prod/manifests/opentelemetry-collector/values.yaml")
	overridesIdx := strings.Index(contentStr, "$values/clusters/prod/manifests/opentelemetry-collector/overrides.yaml")
	if valuesIdx >= overridesIdx {
		t.Error("overrides.yaml must come after values.yaml in valueFiles so it takes precedence")
	}

	// Should have the ref: values source
	if !strings.Contains(contentStr, "ref: values") {
		t.Error("opentelemetry-collector.yaml should have a ref: values source")
	}

	// Should NOT have inline values (base config is in valueFiles now)
	if strings.Contains(contentStr, "values: |") {
		t.Error("opentelemetry-collector.yaml should not have inline values, base config should be in valueFiles")
	}

	// Verify base values.yaml was written to manifests
	baseValuesPath := filepath.Join(tmpDir, "manifests", "opentelemetry-collector", "values.yaml")
	baseContent, err := os.ReadFile(filepath.Clean(baseValuesPath))
	if err != nil {
		t.Fatalf("values.yaml should exist in manifests: %v", err)
	}
	if !strings.Contains(string(baseContent), "otel/opentelemetry-collector-k8s") {
		t.Error("base values.yaml should contain the collector image config")
	}
}
