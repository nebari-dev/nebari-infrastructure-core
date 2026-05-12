package argocd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
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
		name                    string
		settings                provider.InfraSettings
		wantStorageClass        string
		wantLBAnnotationCount   int
		wantKeycloakBasePath    string
		wantMetalLBAddressRange string
		wantHTTPSPort           int
	}{
		{
			name:             "aws defaults",
			settings:         provider.InfraSettings{StorageClass: "gp2"},
			wantStorageClass: "gp2",
			wantHTTPSPort:    443,
		},
		{
			name: "hetzner with annotations",
			settings: provider.InfraSettings{
				StorageClass:            "hcloud-volumes",
				LoadBalancerAnnotations: map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
			},
			wantStorageClass:      "hcloud-volumes",
			wantLBAnnotationCount: 1,
			wantHTTPSPort:         443,
		},
		{
			name: "local with MetalLB",
			settings: provider.InfraSettings{
				StorageClass:       "standard",
				NeedsMetalLB:       true,
				MetalLBAddressPool: "192.168.1.100-192.168.1.110",
			},
			wantStorageClass:        "standard",
			wantMetalLBAddressRange: "192.168.1.100-192.168.1.110",
			wantHTTPSPort:           443,
		},
		{
			name: "custom HTTPS port",
			settings: provider.InfraSettings{
				StorageClass: "standard",
				HTTPSPort:    8443,
			},
			wantStorageClass: "standard",
			wantHTTPSPort:    8443,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{Domain: "test.example.com"}
			data := NewTemplateData(cfg, tt.settings)
			if data.StorageClass != tt.wantStorageClass {
				t.Errorf("StorageClass = %q, want %q", data.StorageClass, tt.wantStorageClass)
			}
			if len(data.LoadBalancerAnnotations) != tt.wantLBAnnotationCount {
				t.Errorf("LoadBalancerAnnotations count = %d, want %d", len(data.LoadBalancerAnnotations), tt.wantLBAnnotationCount)
			}
			if data.KeycloakBasePath != tt.wantKeycloakBasePath {
				t.Errorf("KeycloakBasePath = %q, want %q", data.KeycloakBasePath, tt.wantKeycloakBasePath)
			}
			if data.MetalLBAddressRange != tt.wantMetalLBAddressRange {
				t.Errorf("MetalLBAddressRange = %q, want %q", data.MetalLBAddressRange, tt.wantMetalLBAddressRange)
			}
			if data.HTTPSPort != tt.wantHTTPSPort {
				t.Errorf("HTTPSPort = %d, want %d", data.HTTPSPort, tt.wantHTTPSPort)
			}
		})
	}
}

func TestNewTemplateData_KeycloakServiceURL(t *testing.T) {
	cfg := &config.NebariConfig{Domain: "test.example.com"}
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
		Domain:    "test.example.com",
		HTTPSPort: 443,
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
	if !strings.Contains(output, "port: 443") {
		t.Errorf("expected HTTPS listener port 443 in rendered gateway, got:\n%s", output)
	}
}

func TestGatewayTemplate_WithoutAnnotations(t *testing.T) {
	data := TemplateData{
		Domain:            "test.example.com",
		HTTPSPort:         443,
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
		domain           string
		wantContextPath  string
		wantServiceURL   string
		wantExternalURL  string
	}{
		{
			name:             "empty base path passes empty context path",
			keycloakBasePath: "",
			domain:           "test.example.com",
			wantContextPath:  `value: ""`,
			wantServiceURL:   "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080",
			wantExternalURL:  "https://keycloak.test.example.com",
		},
		{
			name:             "auth base path passes /auth context path",
			keycloakBasePath: "/auth",
			domain:           "test.example.com",
			wantContextPath:  `value: "/auth"`,
			wantServiceURL:   "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080/auth",
			wantExternalURL:  "https://keycloak.test.example.com/auth",
		},
	}

	content, err := templates.ReadFile("templates/manifests/nebari-operator/deployment-patch.yaml")
	if err != nil {
		t.Fatalf("failed to read operator deployment patch: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				Domain:                  tt.domain,
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
			if !strings.Contains(output, "KEYCLOAK_EXTERNAL_URL") {
				t.Error("expected KEYCLOAK_EXTERNAL_URL env var in rendered template")
			}
			if !strings.Contains(output, tt.wantExternalURL) {
				t.Errorf("expected external URL %q in rendered template, got:\n%s", tt.wantExternalURL, output)
			}
		})
	}
}

func TestHTTPToHTTPSRedirectRoute(t *testing.T) {
	content, err := templates.ReadFile("templates/manifests/networking/routes/http-to-https-redirect.yaml")
	if err != nil {
		t.Fatalf("failed to read redirect route template: %v", err)
	}

	tests := []struct {
		name      string
		httpsPort int
		wantPort  string
	}{
		{"default port 443", 443, "port: 443"},
		{"custom port", 8443, "port: 8443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := TemplateData{
				Domain:    "test.example.com",
				HTTPSPort: tt.httpsPort,
			}

			processed, err := processTemplate("manifests/networking/routes/http-to-https-redirect.yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			output := string(processed)

			checks := []struct {
				name     string
				contains string
			}{
				{"kind", "kind: HTTPRoute"},
				{"targets http listener", "sectionName: http"},
				{"redirect filter type", "type: RequestRedirect"},
				{"redirect to https", "scheme: https"},
				{"301 status code", "statusCode: 301"},
				{"targets nebari-gateway", "name: nebari-gateway"},
				{"redirect port", tt.wantPort},
			}
			for _, c := range checks {
				if !strings.Contains(output, c.contains) {
					t.Errorf("expected %q in rendered redirect route, got:\n%s", c.contains, output)
				}
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

func TestServiceHTTPRoutes_TargetHTTPSListener(t *testing.T) {
	// Dynamically discover all route templates so new routes are automatically covered.
	routeDir := "templates/manifests/networking/routes"
	entries, err := templates.ReadDir(routeDir)
	if err != nil {
		t.Fatalf("failed to read routes directory: %v", err)
	}

	data := TemplateData{
		Domain:              "test.example.com",
		HTTPSPort:           443,
		KeycloakServiceName: "keycloak-keycloakx-http",
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		templatePath := routeDir + "/" + entry.Name()

		// The redirect route targets http; all other routes must target https.
		if entry.Name() == "http-to-https-redirect.yaml" {
			continue
		}

		t.Run(name, func(t *testing.T) {
			content, err := templates.ReadFile(templatePath)
			if err != nil {
				t.Fatalf("failed to read %s: %v", templatePath, err)
			}

			processed, err := processTemplate(templatePath, content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			output := string(processed)

			if !strings.Contains(output, "sectionName: https") {
				t.Errorf("%s should target sectionName: https, got:\n%s", name, output)
			}
			// Trailing newline distinguishes "sectionName: http" from "sectionName: https".
			if strings.Contains(output, "sectionName: http\n") {
				t.Errorf("%s should NOT target the http listener", name)
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
			cfg := &config.NebariConfig{Domain: tt.domain}
			settings := provider.InfraSettings{KeycloakBasePath: tt.keycloakBasePath}
			data := NewTemplateData(cfg, settings)

			if data.KeycloakIssuerURL != tt.wantIssuerURL {
				t.Errorf("KeycloakIssuerURL = %q, want %q", data.KeycloakIssuerURL, tt.wantIssuerURL)
			}
		})
	}
}

func TestWriteAllToGit_IncludesRedirectRoute(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := &config.NebariConfig{
		Domain: "test.example.com",
	}
	settings := provider.InfraSettings{
		StorageClass: "gp2",
	}

	mock := &mockGitClient{workDir: tmpDir}
	err := WriteAllToGit(ctx, mock, cfg, settings)
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	redirectPath := filepath.Join(tmpDir, "manifests", "networking", "routes", "http-to-https-redirect.yaml")
	if _, err := os.Stat(redirectPath); os.IsNotExist(err) {
		t.Error("WriteAllToGit did not write http-to-https-redirect.yaml")
	}

	content, err := os.ReadFile(redirectPath) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatalf("failed to read redirect route: %v", err)
	}
	output := string(content)
	if !strings.Contains(output, "statusCode: 301") {
		t.Errorf("redirect route missing statusCode: 301, got:\n%s", output)
	}
	if !strings.Contains(output, "port: 443") {
		t.Errorf("redirect route missing port: 443, got:\n%s", output)
	}
	if !strings.Contains(output, "sectionName: http") {
		t.Errorf("redirect route should target sectionName: http, got:\n%s", output)
	}
}

// mockGitClient satisfies git.Client for tests that only need WorkDir().
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

// nopWriteCloser wraps a bytes.Buffer to satisfy io.WriteCloser
type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

func TestSyncWaveOrdering(t *testing.T) {
	ctx := context.Background()

	// Read cert-manager and envoy-gateway templates
	tests := []struct {
		appName      string
		expectedWave string
	}{
		{"envoy-gateway", `sync-wave: "1"`},
		{"cert-manager", `sync-wave: "2"`},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			var buf bytes.Buffer
			err := WriteApplication(ctx, &buf, tt.appName)
			if err != nil {
				t.Fatalf("WriteApplication(%s) error: %v", tt.appName, err)
			}

			content := buf.String()
			if !strings.Contains(content, tt.expectedWave) {
				t.Errorf("%s should have %s, got:\n%s", tt.appName, tt.expectedWave, content)
			}
		})
	}
}

func TestEnvoyGatewayBeforeCertManager(t *testing.T) {
	ctx := context.Background()

	// Extract sync wave number as int for robust comparison
	// (lexicographic comparison would fail for multi-digit numbers: "9" > "10")
	getSyncWave := func(appName string) int {
		var buf bytes.Buffer
		if err := WriteApplication(ctx, &buf, appName); err != nil {
			t.Fatalf("WriteApplication(%s) error: %v", appName, err)
		}
		content := buf.String()
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, "sync-wave") {
				// Extract number from line like: argocd.argoproj.io/sync-wave: "1"
				line = strings.TrimSpace(line)
				// Find the quoted number
				start := strings.Index(line, `"`)
				end := strings.LastIndex(line, `"`)
				if start != -1 && end > start {
					numStr := line[start+1 : end]
					num, err := strconv.Atoi(numStr)
					if err != nil {
						t.Fatalf("%s has invalid sync-wave value %q: %v", appName, numStr, err)
					}
					return num
				}
			}
		}
		t.Fatalf("%s has no sync-wave annotation", appName)
		return 0
	}

	envoyWaveNum := getSyncWave("envoy-gateway")
	certWaveNum := getSyncWave("cert-manager")

	// envoy-gateway must come before cert-manager (lower wave number)
	// because cert-manager needs Gateway API CRDs that envoy-gateway installs
	if envoyWaveNum >= certWaveNum {
		t.Errorf("envoy-gateway (%d) must have a lower sync-wave than cert-manager (%d)", envoyWaveNum, certWaveNum)
	}
}

// TestKeycloakRealmConfigCM asserts the load-bearing fields of the rendered
// realm-config ConfigMap: the kcc input is what makes the realm-setup-job
// produce a valid Keycloak state. Regressions here are silent (the YAML
// renders fine but Keycloak gets misconfigured), so we check structure.
func TestKeycloakRealmConfigCM(t *testing.T) {
	data := TemplateData{
		Domain: "test.example.com",
	}

	content, err := templates.ReadFile("templates/manifests/keycloak/realm-config-cm.yaml")
	if err != nil {
		t.Fatalf("failed to read realm-config-cm.yaml template: %v", err)
	}

	processed, err := processTemplate("manifests/keycloak/realm-config-cm.yaml", content, data)
	if err != nil {
		t.Fatalf("processTemplate() error: %v", err)
	}

	output := string(processed)

	// ConfigMap shell
	for _, want := range []string{
		"kind: ConfigMap",
		"name: keycloak-realm-config",
		"namespace: keycloak",
		"realm.yaml: |",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in rendered ConfigMap, got:\n%s", want, output)
		}
	}

	// kcc realm payload — load-bearing fields the realm-setup-job depends on
	for _, want := range []string{
		"realm: nebari",
		"defaultDefaultClientScopes:",
		// kcc applies list fields as full-replace, so the built-in default
		// scopes must be present here or tokens lose email/profile/roles.
		"- basic",
		"- profile",
		"- email",
		"- roles",
		"- web-origins",
		"- acr",
		"- groups",
		"- name: argocd-admins",
		"- name: argocd-viewers",
		"username: admin",
		"$(env:REALM_ADMIN_PASSWORD)",
		"- /argocd-admins",
		"protocolMapper: oidc-group-membership-mapper",
		"claim.name: groups",
		"clientId: argocd",
		"$(env:ARGOCD_CLIENT_SECRET)",
		"https://argocd.test.example.com/auth/callback",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in rendered realm config, got:\n%s", want, output)
		}
	}

	// The argocd client must explicitly list the full set of default
	// scopes. Keycloak's realm default-defaults auto-apply only at client
	// creation, not on subsequent updates — so we need this redundancy to
	// keep kcc's replace semantics from stripping scopes on existing
	// clients during a re-run.
	if !strings.Contains(output, "        defaultClientScopes:\n          - basic") {
		t.Errorf("argocd client must explicitly list defaultClientScopes starting with built-ins, got:\n%s", output)
	}
}
