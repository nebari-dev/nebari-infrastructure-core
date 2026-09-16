package argocd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
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
		name                   string
		settings               cluster.InfraSettings
		wantStorageClass       string
		wantLBAnnotationCount  int
		wantKeycloakBasePath   string
		wantGatewayHostAddress string
		wantHTTPSPort          int
	}{
		{
			name:             "aws defaults",
			settings:         cluster.InfraSettings{StorageClass: "gp2"},
			wantStorageClass: "gp2",
			wantHTTPSPort:    443,
		},
		{
			name: "hetzner with annotations",
			settings: cluster.InfraSettings{
				StorageClass:            "hcloud-volumes",
				LoadBalancerAnnotations: map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
			},
			wantStorageClass:      "hcloud-volumes",
			wantLBAnnotationCount: 1,
			wantHTTPSPort:         443,
		},
		{
			name: "local with host-port gateway",
			settings: cluster.InfraSettings{
				StorageClass:       "standard",
				GatewayHostAddress: "127.0.0.1",
			},
			wantStorageClass:       "standard",
			wantGatewayHostAddress: "127.0.0.1",
			wantHTTPSPort:          443,
		},
		{
			name: "custom HTTPS port",
			settings: cluster.InfraSettings{
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
			data := NewTemplateData(cfg, nil, tt.settings)
			if data.StorageClass != tt.wantStorageClass {
				t.Errorf("StorageClass = %q, want %q", data.StorageClass, tt.wantStorageClass)
			}
			if len(data.LoadBalancerAnnotations) != tt.wantLBAnnotationCount {
				t.Errorf("LoadBalancerAnnotations count = %d, want %d", len(data.LoadBalancerAnnotations), tt.wantLBAnnotationCount)
			}
			if data.KeycloakBasePath != tt.wantKeycloakBasePath {
				t.Errorf("KeycloakBasePath = %q, want %q", data.KeycloakBasePath, tt.wantKeycloakBasePath)
			}
			if data.GatewayHostAddress != tt.wantGatewayHostAddress {
				t.Errorf("GatewayHostAddress = %q, want %q", data.GatewayHostAddress, tt.wantGatewayHostAddress)
			}
			if data.HTTPSPort != tt.wantHTTPSPort {
				t.Errorf("HTTPSPort = %d, want %d", data.HTTPSPort, tt.wantHTTPSPort)
			}
			if data.GatewayHTTPNodePort != cluster.GatewayHTTPNodePort || data.GatewayHTTPSNodePort != cluster.GatewayHTTPSNodePort {
				t.Errorf("gateway NodePorts = %d/%d, want %d/%d", data.GatewayHTTPNodePort, data.GatewayHTTPSNodePort, cluster.GatewayHTTPNodePort, cluster.GatewayHTTPSNodePort)
			}
		})
	}
}

func TestNewTemplateData_KeycloakServiceURL(t *testing.T) {
	cfg := &config.NebariConfig{Domain: "test.example.com"}
	settings := cluster.InfraSettings{
		StorageClass:     "hcloud-volumes",
		KeycloakBasePath: "/auth",
	}
	data := NewTemplateData(cfg, nil, settings)

	if !strings.HasSuffix(data.KeycloakServiceURL, "/auth") {
		t.Errorf("KeycloakServiceURL = %q, should end with /auth", data.KeycloakServiceURL)
	}

	// Without base path
	settings.KeycloakBasePath = ""
	data = NewTemplateData(cfg, nil, settings)
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

	appContent, err := templates.ReadFile("templates/apps/keycloak.yaml")
	if err != nil {
		t.Fatalf("failed to read keycloak template: %v", err)
	}
	baseContent, err := templates.ReadFile("templates/values/keycloak/base.yaml")
	if err != nil {
		t.Fatalf("failed to read keycloak base values template: %v", err)
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

			processedApp, err := processTemplate("apps/keycloak.yaml", appContent, data)
			if err != nil {
				t.Fatalf("processTemplate(app) error: %v", err)
			}
			processedBase, err := processTemplate("values/keycloak/base.yaml", baseContent, data)
			if err != nil {
				t.Fatalf("processTemplate(base) error: %v", err)
			}
			output := string(processedApp) + "\n" + string(processedBase)

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

// TestKeycloakTemplate_TrustBundle verifies the org CA bundle is wired into
// Keycloak only when trust-manager is enabled: the projected ConfigMap is
// mounted and KC_TRUSTSTORE_PATHS points at it so outbound TLS trusts the org CA.
func TestKeycloakTemplate_TrustBundle(t *testing.T) {
	content, err := templates.ReadFile("templates/values/keycloak/base.yaml")
	if err != nil {
		t.Fatalf("failed to read keycloak base values template: %v", err)
	}

	baseData := func() TemplateData {
		return TemplateData{
			Domain:                  "test.example.com",
			KeycloakNamespace:       "keycloak",
			KeycloakAdminSecretName: "keycloak-admin",
			GitRepoURL:              "https://github.com/example/repo",
			GitBranch:               "main",
		}
	}

	// helmValues renders the keycloak base values template and returns both the
	// raw render and its parsed form.
	helmValues := func(t *testing.T, data TemplateData) (string, map[string]any) {
		t.Helper()
		processed, err := processTemplate("values/keycloak/base.yaml", content, data)
		if err != nil {
			t.Fatalf("processTemplate() error: %v", err)
		}
		var values map[string]any
		if err := yaml.Unmarshal(processed, &values); err != nil {
			t.Fatalf("keycloakx Helm values are not valid YAML: %v\n%s", err, processed)
		}
		return string(processed), values
	}

	t.Run("mounts bundle and sets truststore path when enabled", func(t *testing.T) {
		data := baseData()
		data.TrustManagerEnabled = true
		data.TrustBundlePEM = testCAPEM

		out, values := helmValues(t, data)

		for _, want := range []string{
			"KC_TRUSTSTORE_PATHS",
			"/etc/nebari/truststore",
			"name: nebari-trust-bundle",
			"ca-certificates.crt",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected rendered template to contain %q, got:\n%s", want, out)
			}
		}

		if _, ok := values["extraVolumes"].(string); !ok {
			t.Errorf("expected extraVolumes string in Helm values, got: %#v", values["extraVolumes"])
		}
		if _, ok := values["extraVolumeMounts"].(string); !ok {
			t.Errorf("expected extraVolumeMounts string in Helm values, got: %#v", values["extraVolumeMounts"])
		}
	})

	t.Run("omits bundle wiring when disabled", func(t *testing.T) {
		out, values := helmValues(t, baseData())

		for _, unwanted := range []string{
			"KC_TRUSTSTORE_PATHS",
			"nebari-trust-bundle",
			"extraVolumes:",
			"extraVolumeMounts:",
		} {
			if strings.Contains(out, unwanted) {
				t.Errorf("did not expect %q when trust-manager disabled, got:\n%s", unwanted, out)
			}
		}
		if _, ok := values["extraVolumes"]; ok {
			t.Errorf("did not expect extraVolumes key when disabled, got: %#v", values["extraVolumes"])
		}
	})
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

	appContent, err := templates.ReadFile("templates/apps/nebari-landingpage.yaml")
	if err != nil {
		t.Fatalf("failed to read nebari-landingpage template: %v", err)
	}
	baseContent, err := templates.ReadFile("templates/values/nebari-landingpage/base.yaml")
	if err != nil {
		t.Fatalf("failed to read nebari-landingpage base values template: %v", err)
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

			processedApp, err := processTemplate("apps/nebari-landingpage.yaml", appContent, data)
			if err != nil {
				t.Fatalf("processTemplate(app) error: %v", err)
			}
			processedBase, err := processTemplate("values/nebari-landingpage/base.yaml", baseContent, data)
			if err != nil {
				t.Fatalf("processTemplate(base) error: %v", err)
			}
			output := string(processedApp) + "\n" + string(processedBase)

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

			// This skips ANY route that renders empty with the zero-value test
			// data, not just longhorn-httproute.yaml — so a conditionally
			// rendered route silently drops out of this generic https check.
			// Each such route needs its own test pinning the https-listener
			// property with its gate enabled (see
			// TestWriteAllToGit_LonghornHTTPRoute for the Longhorn one).
			if strings.TrimSpace(output) == "" {
				t.Skipf("skipping %s: empty render with default test data", name)
			}

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

// The Keycloak secret names and keys used to be literals in the templates and
// are now TemplateData fields, so that `nic outputs` and the manifests cannot
// disagree about them. An unpopulated field renders as the empty string rather
// than failing, which would leave the realm-setup Job and the Keycloak
// StatefulSet pointing at a nameless secret key. Pin the rendered strings.
func TestKeycloakSecretCoordinatesRender(t *testing.T) {
	data := NewTemplateData(&config.NebariConfig{Domain: "test.example.com"}, nil, cluster.InfraSettings{})

	tests := []struct {
		name     string
		template string
		want     []string
	}{
		{
			name:     "keycloak base values reference the admin password key",
			template: "templates/values/keycloak/base.yaml",
			want: []string{
				"name: " + KeycloakDefaultAdminSecretName,
				"key: " + KeycloakAdminPasswordKey,
			},
		},
		{
			name:     "realm setup job references both secrets and keys",
			template: "templates/manifests/keycloak/realm-setup-job.yaml",
			want: []string{
				"name: " + KeycloakDefaultAdminSecretName,
				"key: " + KeycloakAdminPasswordKey,
				"name: " + NebariRealmAdminSecretName,
				"key: " + NebariRealmAdminPasswordKey,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := templates.ReadFile(tt.template)
			if err != nil {
				t.Fatalf("read %s: %v", tt.template, err)
			}

			rendered, err := processTemplate(tt.template, content, data)
			if err != nil {
				t.Fatalf("processTemplate(%s): %v", tt.template, err)
			}

			output := string(rendered)
			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("rendered %s does not contain %q:\n%s", tt.template, want, output)
				}
			}
			// An unpopulated field renders as an empty value after the colon.
			for _, empty := range []string{"name:\n", "key:\n", "name: \n", "key: \n"} {
				if strings.Contains(output, empty) {
					t.Errorf("rendered %s has an empty secret reference (%q):\n%s", tt.template, empty, output)
				}
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
			settings := cluster.InfraSettings{KeycloakBasePath: tt.keycloakBasePath}
			data := NewTemplateData(cfg, nil, settings)

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
	settings := cluster.InfraSettings{
		StorageClass: "gp2",
	}

	err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, "")
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// Working-tree modes are not asserted here: WriteFile requests
	// git.GitOpsFileMode but the on-disk mode is masked by the ambient umask,
	// and working-tree modes are no longer an invariant the code guarantees
	// (ArgoCD reads via .git, repaired by the git client, not the working tree).
	redirectPath := filepath.Join(tmpDir, "manifests", "networking", "routes", "http-to-https-redirect.yaml")
	if _, err := os.Stat(redirectPath); os.IsNotExist(err) {
		t.Error("WriteAllToGit did not write http-to-https-redirect.yaml")
	} else if err != nil {
		t.Fatalf("stat redirect route: %v", err)
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

func TestWriteAllToGit_LonghornHTTPRoute(t *testing.T) {
	ctx := context.Background()

	t.Run("includes longhorn-httproute when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{
			StorageClass:    "longhorn",
			LonghornEnabled: true,
		}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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
			"port: 80",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("longhorn-httproute.yaml missing %q\ngot:\n%s", want, out)
			}
		}
	})

	t.Run("omits longhorn-httproute body when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{
			StorageClass:    "gp2",
			LonghornEnabled: false,
		}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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

// egOIDCIssuerPattern is the validation Envoy Gateway applies to
// SecurityPolicy's spec.oidc.provider.issuer as of v1.9.1, copied verbatim from
// the CRD (api/v1alpha1/oidc_types.go). EG enforces the same https-scheme rule a
// second time in the translator (validateOIDCIssuerURL), so a value that fails
// this is rejected at apply time AND would leave the policy Accepted: False with
// no oauth2 filter on the route. Asserted directly rather than only comparing
// against an expected literal, so that changing the template and the expected
// constant together still fails.
var egOIDCIssuerPattern = regexp.MustCompile(`^https://[^/?#@]+(/[^?#]*)?$`)

func assertValidEGIssuer(t *testing.T, issuer string) {
	t.Helper()
	if !egOIDCIssuerPattern.MatchString(issuer) {
		t.Errorf("oidc.provider.issuer %q does not satisfy the Envoy Gateway issuer constraint %s",
			issuer, egOIDCIssuerPattern)
	}
}

// longhornSecurityPolicyShape mirrors the subset of the rendered SecurityPolicy
// we assert on. It exists so tests can verify the split-URL invariant on a
// per-field basis instead of relying on strings.Contains over the whole file,
// which cannot distinguish `oidc.provider.issuer` from `jwt.providers[0].issuer`
// when they render as different lines with the same key.
type longhornSecurityPolicyShape struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		TargetRefs []struct {
			Group string `yaml:"group"`
			Kind  string `yaml:"kind"`
			Name  string `yaml:"name"`
		} `yaml:"targetRefs"`
		OIDC struct {
			Provider struct {
				Issuer                string `yaml:"issuer"`
				TokenEndpoint         string `yaml:"tokenEndpoint"`
				AuthorizationEndpoint string `yaml:"authorizationEndpoint"`
				EndSessionEndpoint    string `yaml:"endSessionEndpoint"`
			} `yaml:"provider"`
			ClientID     string `yaml:"clientID"`
			ClientSecret struct {
				Name string `yaml:"name"`
			} `yaml:"clientSecret"`
			RedirectURL           string `yaml:"redirectURL"`
			LogoutPath            string `yaml:"logoutPath"`
			ForwardAccessToken    bool   `yaml:"forwardAccessToken"`
			RefreshToken          bool   `yaml:"refreshToken"`
			PassThroughAuthHeader bool   `yaml:"passThroughAuthHeader"`
		} `yaml:"oidc"`
		JWT struct {
			Providers []struct {
				Name       string `yaml:"name"`
				Issuer     string `yaml:"issuer"`
				RemoteJWKS struct {
					URI string `yaml:"uri"`
				} `yaml:"remoteJWKS"`
			} `yaml:"providers"`
		} `yaml:"jwt"`
		Authorization struct {
			DefaultAction string `yaml:"defaultAction"`
			Rules         []struct {
				Name      string `yaml:"name"`
				Action    string `yaml:"action"`
				Principal struct {
					JWT struct {
						Provider string `yaml:"provider"`
						Claims   []struct {
							Name      string   `yaml:"name"`
							ValueType string   `yaml:"valueType"`
							Values    []string `yaml:"values"`
						} `yaml:"claims"`
					} `yaml:"jwt"`
				} `yaml:"principal"`
			} `yaml:"rules"`
		} `yaml:"authorization"`
	} `yaml:"spec"`
}

func TestWriteAllToGit_LonghornSecurityPolicy(t *testing.T) {
	ctx := context.Background()

	t.Run("includes SecurityPolicy when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{
			StorageClass:    "longhorn",
			LonghornEnabled: true,
		}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		policyPath := filepath.Join(tmpDir, "manifests", "networking", "policies", "longhorn-securitypolicy.yaml")
		content, err := os.ReadFile(policyPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn securitypolicy: %v", err)
		}

		var sp longhornSecurityPolicyShape
		if err := yaml.Unmarshal(content, &sp); err != nil {
			t.Fatalf("failed to unmarshal SecurityPolicy: %v\ngot:\n%s", err, string(content))
		}

		const (
			inClusterBase = "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080/realms/nebari"
			publicBase    = "https://keycloak.test.example.com/realms/nebari"
		)

		// Top-level shape.
		if got, want := sp.APIVersion, "gateway.envoyproxy.io/v1alpha1"; got != want {
			t.Errorf("apiVersion: got %q, want %q", got, want)
		}
		if got, want := sp.Kind, "SecurityPolicy"; got != want {
			t.Errorf("kind: got %q, want %q", got, want)
		}
		if got, want := sp.Metadata.Name, "longhorn-oidc"; got != want {
			t.Errorf("metadata.name: got %q, want %q", got, want)
		}
		if got, want := sp.Metadata.Namespace, "longhorn-system"; got != want {
			t.Errorf("metadata.namespace: got %q, want %q", got, want)
		}

		// Target: the Longhorn HTTPRoute.
		if len(sp.Spec.TargetRefs) != 1 {
			t.Fatalf("spec.targetRefs: got %d, want 1", len(sp.Spec.TargetRefs))
		}
		if tr := sp.Spec.TargetRefs[0]; tr.Kind != "HTTPRoute" || tr.Name != "longhorn" {
			t.Errorf("spec.targetRefs[0]: got %+v, want kind=HTTPRoute name=longhorn", tr)
		}

		// OIDC provider — split-URL invariant. tokenEndpoint is in-cluster
		// (back-channel); authorizationEndpoint + endSessionEndpoint are public
		// (front-channel). Swapping any two silently reintroduces the
		// private-domain OIDC-discovery bug this template exists to fix.
		// issuer is public purely to satisfy the CRD's https constraint; it is
		// inert at runtime once discovery is suppressed.
		if got, want := sp.Spec.OIDC.Provider.Issuer, publicBase; got != want {
			t.Errorf("oidc.provider.issuer: got %q, want %q (public; EG constrains this field to an https scheme)", got, want)
		}
		assertValidEGIssuer(t, sp.Spec.OIDC.Provider.Issuer)
		if got, want := sp.Spec.OIDC.Provider.TokenEndpoint, inClusterBase+"/protocol/openid-connect/token"; got != want {
			t.Errorf("oidc.provider.tokenEndpoint: got %q, want %q (in-cluster)", got, want)
		}
		if got, want := sp.Spec.OIDC.Provider.AuthorizationEndpoint, publicBase+"/protocol/openid-connect/auth"; got != want {
			t.Errorf("oidc.provider.authorizationEndpoint: got %q, want %q (public)", got, want)
		}
		if got, want := sp.Spec.OIDC.Provider.EndSessionEndpoint, publicBase+"/protocol/openid-connect/logout"; got != want {
			t.Errorf("oidc.provider.endSessionEndpoint: got %q, want %q (public)", got, want)
		}

		// OIDC client fields.
		if got, want := sp.Spec.OIDC.ClientID, "longhorn"; got != want {
			t.Errorf("oidc.clientID: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.OIDC.ClientSecret.Name, "longhorn-oidc-client-secret"; got != want {
			t.Errorf("oidc.clientSecret.name: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.OIDC.RedirectURL, "https://longhorn.test.example.com/oauth2/callback"; got != want {
			t.Errorf("oidc.redirectURL: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.OIDC.LogoutPath, "/oauth2/logout"; got != want {
			t.Errorf("oidc.logoutPath: got %q, want %q", got, want)
		}
		if !sp.Spec.OIDC.ForwardAccessToken {
			t.Errorf("oidc.forwardAccessToken: got false, want true")
		}
		// refreshToken keeps the session alive from the refresh token instead of
		// bouncing the user back through Keycloak when the access token expires.
		if !sp.Spec.OIDC.RefreshToken {
			t.Errorf("oidc.refreshToken: got false, want true")
		}
		// passThroughAuthHeader lets a request that already carries an
		// `Authorization: Bearer <token>` skip the oauth2 redirect and reach the
		// jwt block below. Without it, EG's oauth2 filter (which runs ahead of
		// jwt_authn) treats the Bearer as an unknown session and
		// bounces the request back to Keycloak — breaking scripted API access.
		// Browser flow is unaffected because browsers arrive with the oauth2
		// session cookie, not a Bearer.
		if !sp.Spec.OIDC.PassThroughAuthHeader {
			t.Errorf("oidc.passThroughAuthHeader: got false, want true")
		}

		// JWT provider — issuer MUST be public, because it has to match the
		// `iss` claim Keycloak stamps into tokens, which is KC_HOSTNAME
		// (always the public URL) regardless of which endpoint minted the token.
		if len(sp.Spec.JWT.Providers) != 1 {
			t.Fatalf("jwt.providers: got %d, want 1", len(sp.Spec.JWT.Providers))
		}
		jp := sp.Spec.JWT.Providers[0]
		if got, want := jp.Name, "keycloak"; got != want {
			t.Errorf("jwt.providers[0].name: got %q, want %q", got, want)
		}
		if got, want := jp.Issuer, publicBase; got != want {
			t.Errorf("jwt.providers[0].issuer: got %q, want %q (public — must match `iss` claim)", got, want)
		}
		if got, want := jp.RemoteJWKS.URI, inClusterBase+"/protocol/openid-connect/certs"; got != want {
			t.Errorf("jwt.providers[0].remoteJWKS.uri: got %q, want %q (in-cluster)", got, want)
		}

		// Authorization: default-deny, allow only /longhorn-admins group.
		if got, want := sp.Spec.Authorization.DefaultAction, "Deny"; got != want {
			t.Errorf("authorization.defaultAction: got %q, want %q", got, want)
		}
		if len(sp.Spec.Authorization.Rules) != 1 {
			t.Fatalf("authorization.rules: got %d, want 1", len(sp.Spec.Authorization.Rules))
		}
		rule := sp.Spec.Authorization.Rules[0]
		if rule.Name != "allow-longhorn-admins" || rule.Action != "Allow" {
			t.Errorf("authorization.rules[0]: got name=%q action=%q, want allow-longhorn-admins/Allow",
				rule.Name, rule.Action)
		}
		if rule.Principal.JWT.Provider != "keycloak" {
			t.Errorf("authorization.rules[0].principal.jwt.provider: got %q, want %q",
				rule.Principal.JWT.Provider, "keycloak")
		}
		if len(rule.Principal.JWT.Claims) != 1 {
			t.Fatalf("authorization.rules[0].principal.jwt.claims: got %d, want 1",
				len(rule.Principal.JWT.Claims))
		}
		claim := rule.Principal.JWT.Claims[0]
		if claim.Name != "groups" || claim.ValueType != "StringArray" {
			t.Errorf("authorization claim: got name=%q valueType=%q, want groups/StringArray",
				claim.Name, claim.ValueType)
		}
		if len(claim.Values) != 1 || claim.Values[0] != "/longhorn-admins" {
			t.Errorf("authorization claim.values: got %v, want [/longhorn-admins]", claim.Values)
		}

		appPath := filepath.Join(tmpDir, "apps", "securitypolicies.yaml")
		if _, err := os.Stat(appPath); err != nil {
			t.Errorf("apps/securitypolicies.yaml should be written when LonghornEnabled=true: %v", err)
		}
	})

	t.Run("renders correctly with a KeycloakBasePath override", func(t *testing.T) {
		// A non-empty KeycloakBasePath (e.g. "/auth" on Keycloak deployments
		// that keep the pre-Quarkus path prefix) has to land in the right
		// position on all four rendered URLs. Notably: `KeycloakServiceURL`
		// already embeds the base path (see writer.go), while the public URLs
		// interpolate `{{ .KeycloakBasePath }}` directly. Regressing either
		// side would mis-render only under this configuration.
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{
			StorageClass:     "longhorn",
			LonghornEnabled:  true,
			KeycloakBasePath: "/auth",
		}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		policyPath := filepath.Join(tmpDir, "manifests", "networking", "policies", "longhorn-securitypolicy.yaml")
		content, err := os.ReadFile(policyPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read longhorn securitypolicy: %v", err)
		}

		var sp longhornSecurityPolicyShape
		if err := yaml.Unmarshal(content, &sp); err != nil {
			t.Fatalf("failed to unmarshal SecurityPolicy: %v\ngot:\n%s", err, string(content))
		}

		const (
			inClusterBase = "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080/auth/realms/nebari"
			publicBase    = "https://keycloak.test.example.com/auth/realms/nebari"
		)

		if got, want := sp.Spec.OIDC.Provider.Issuer, publicBase; got != want {
			t.Errorf("oidc.provider.issuer with basePath=/auth: got %q, want %q", got, want)
		}
		assertValidEGIssuer(t, sp.Spec.OIDC.Provider.Issuer)
		if got, want := sp.Spec.OIDC.Provider.TokenEndpoint, inClusterBase+"/protocol/openid-connect/token"; got != want {
			t.Errorf("oidc.provider.tokenEndpoint with basePath=/auth: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.OIDC.Provider.AuthorizationEndpoint, publicBase+"/protocol/openid-connect/auth"; got != want {
			t.Errorf("oidc.provider.authorizationEndpoint with basePath=/auth: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.OIDC.Provider.EndSessionEndpoint, publicBase+"/protocol/openid-connect/logout"; got != want {
			t.Errorf("oidc.provider.endSessionEndpoint with basePath=/auth: got %q, want %q", got, want)
		}

		if len(sp.Spec.JWT.Providers) != 1 {
			t.Fatalf("jwt.providers with basePath=/auth: got %d, want 1", len(sp.Spec.JWT.Providers))
		}
		if got, want := sp.Spec.JWT.Providers[0].Issuer, publicBase; got != want {
			t.Errorf("jwt.providers[0].issuer with basePath=/auth: got %q, want %q", got, want)
		}
		if got, want := sp.Spec.JWT.Providers[0].RemoteJWKS.URI, inClusterBase+"/protocol/openid-connect/certs"; got != want {
			t.Errorf("jwt.providers[0].remoteJWKS.uri with basePath=/auth: got %q, want %q",
				got, want)
		}
	})

	t.Run("removes previously written SecurityPolicy templates on an enable-to-disable toggle", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}

		enabled := cluster.InfraSettings{StorageClass: "longhorn", LonghornEnabled: true}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, enabled, ""); err != nil {
			t.Fatalf("WriteAllToGit() enabled error: %v", err)
		}

		disabled := cluster.InfraSettings{StorageClass: "gp2", LonghornEnabled: false}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, disabled, ""); err != nil {
			t.Fatalf("WriteAllToGit() disabled error: %v", err)
		}

		for _, stale := range []string{
			filepath.Join(tmpDir, "apps", "securitypolicies.yaml"),
			filepath.Join(tmpDir, "manifests", "networking", "policies"),
		} {
			if _, err := os.Stat(stale); !os.IsNotExist(err) {
				t.Errorf("%s should be removed when Longhorn is toggled off, stat err: %v", stale, err)
			}
		}
	})

	t.Run("skips SecurityPolicy templates when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{
			StorageClass:    "gp2",
			LonghornEnabled: false,
		}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		policyPath := filepath.Join(tmpDir, "manifests", "networking", "policies", "longhorn-securitypolicy.yaml")
		if _, err := os.Stat(policyPath); !os.IsNotExist(err) {
			t.Errorf("longhorn-securitypolicy.yaml should not be written when LonghornEnabled=false, stat err: %v", err)
		}

		appPath := filepath.Join(tmpDir, "apps", "securitypolicies.yaml")
		if _, err := os.Stat(appPath); !os.IsNotExist(err) {
			t.Errorf("apps/securitypolicies.yaml should not be written when LonghornEnabled=false, stat err: %v", err)
		}
	})
}

func TestWriteAllToGit_GatewayHostAddress(t *testing.T) {
	ctx := context.Background()

	renderEnvoyProxy := func(t *testing.T, settings cluster.InfraSettings) string {
		t.Helper()
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
			t.Fatalf("WriteAllToGit() error: %v", err)
		}
		proxyPath := filepath.Join(tmpDir, "manifests", "networking", "envoyproxy.yaml")
		content, err := os.ReadFile(proxyPath) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("failed to read envoyproxy.yaml: %v", err)
		}
		return string(content)
	}

	t.Run("pins the Envoy service to the provider's NodePorts when GatewayHostAddress is set", func(t *testing.T) {
		out := renderEnvoyProxy(t, cluster.InfraSettings{
			StorageClass:       "standard",
			GatewayHostAddress: "127.0.0.1",
		})

		for _, want := range []string{
			"envoyService:",
			"type: NodePort",
			"type: StrategicMerge",
			"- port: 80",
			fmt.Sprintf("nodePort: %d", cluster.GatewayHTTPNodePort),
			"- port: 443",
			fmt.Sprintf("nodePort: %d", cluster.GatewayHTTPSNodePort),
		} {
			if !strings.Contains(out, want) {
				t.Errorf("envoyproxy.yaml missing %q\ngot:\n%s", want, out)
			}
		}
	})

	t.Run("targets the overridden HTTPS listener port so the patch merges into an existing port", func(t *testing.T) {
		out := renderEnvoyProxy(t, cluster.InfraSettings{
			StorageClass:       "standard",
			GatewayHostAddress: "127.0.0.1",
			HTTPSPort:          8443,
		})

		if want := fmt.Sprintf("- port: 8443\n                  nodePort: %d", cluster.GatewayHTTPSNodePort); !strings.Contains(out, want) {
			t.Errorf("envoyproxy.yaml should pin the HTTPS NodePort to listener port 8443, got:\n%s", out)
		}
		if strings.Contains(out, "- port: 443") {
			t.Errorf("envoyproxy.yaml should not reference port 443 when https_port is 8443, got:\n%s", out)
		}
	})

	t.Run("omits the service pinning when GatewayHostAddress is empty", func(t *testing.T) {
		out := renderEnvoyProxy(t, cluster.InfraSettings{
			StorageClass: "gp2",
		})

		for _, unwanted := range []string{"envoyService:", "NodePort"} {
			if strings.Contains(out, unwanted) {
				t.Errorf("envoyproxy.yaml should not contain %q for cloud providers, got:\n%s", unwanted, out)
			}
		}
	})
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

func TestWriteAllToGit_RealmSetupRegistersLonghornClient(t *testing.T) {
	ctx := context.Background()

	t.Run("realm-setup includes Longhorn client creation when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{LonghornEnabled: true}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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
			`https://longhorn.$DOMAIN/oauth2/callback\"]`,
			"name=longhorn-admins",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("realm-setup-job missing %q\nfull contents:\n%s", want, out)
			}
		}
		if strings.Contains(out, "longhorn-viewers") {
			t.Errorf("realm-setup-job unexpectedly references longhorn-viewers (group removed); content:\n%s", out)
		}
	})

	t.Run("realm-setup does NOT mention Longhorn when LonghornEnabled is false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{LonghornEnabled: false}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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

func TestWriteAllToGit_GatewayCertIncludesLonghorn(t *testing.T) {
	ctx := context.Background()

	t.Run("cert includes longhorn dnsName when LonghornEnabled is true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{LonghornEnabled: true}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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
		settings := cluster.InfraSettings{LonghornEnabled: false}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
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

func TestWriteApplication_OtelCollector_OverridesExtensionPoint(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()

	if err := WriteApplication(ctx, &buf, "opentelemetry-collector"); err != nil {
		t.Fatalf("WriteApplication(opentelemetry-collector) error: %v", err)
	}
	appContent := buf.String()

	baseRaw, err := templates.ReadFile("templates/values/opentelemetry-collector/base.yaml")
	if err != nil {
		t.Fatalf("read otel base.yaml template: %v", err)
	}
	baseContent := string(baseRaw)

	// Software packs (e.g. nebari-lgtm-pack) drop a ConfigMap named
	// `opentelemetry-collector-overrides` containing `relay.yaml`; the init
	// container resolves it (or falls back to `{}`) into an emptyDir that the
	// collector reads via an extra --config flag. This sidesteps the upstream
	// ArgoCD ignoreDifferences-during-sync bug (argoproj/argo-cd#7478) by
	// keeping the base CM and the override CM completely separate.
	// The values-shaped fragments live in values/opentelemetry-collector/base.yaml
	// since the #406 valueFiles conversion; Application-shaped fragments stay in
	// the app template.
	tests := []struct {
		name        string
		in          string // which document to search
		doc         string // human-readable label for the searched document
		fragment    string
		wantPresent bool
	}{
		// Application manifest fragments
		{"managedNamespaceMetadata block", appContent, "app template", "managedNamespaceMetadata:", true},
		{"nebari.dev/managed namespace label", appContent, "app template", "nebari.dev/managed: \"true\"", true},
		{"inline values blob (old design)", appContent, "app template", "values: |", false},
		{"ignoreDifferences (old design)", appContent, "app template", "ignoreDifferences:", false},
		{"RespectIgnoreDifferences (old design)", appContent, "app template", "RespectIgnoreDifferences=true", false},
		{"jsonPointers (old design)", appContent, "app template", "jsonPointers:", false},
		// Helm values fragments (dedented 8 from their pre-#406 indentation)
		{"extraVolumes section", baseContent, "base.yaml", "extraVolumes:", true},
		{"overrides-src volume with configmap name", baseContent, "base.yaml", "name: overrides-src\n    configMap:\n      name: opentelemetry-collector-overrides\n      optional: true", true},
		{"overrides-resolved emptyDir", baseContent, "base.yaml", "name: overrides-resolved\n    emptyDir: {}", true},
		{"initContainers section", baseContent, "base.yaml", "initContainers:", true},
		{"ensure-overrides init container", baseContent, "base.yaml", "name: ensure-overrides", true},
		{"config flag for overrides", baseContent, "base.yaml", "--config=/conf/overrides/relay.yaml", true},
		{"escaped relabel replacement", baseContent, "base.yaml", "replacement: $$1:$$2", true},
		{"bare relabel replacement (deprecated)", baseContent, "base.yaml", "replacement: $1:$2", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found := strings.Contains(tc.in, tc.fragment)
			if tc.wantPresent && !found {
				t.Errorf("%s missing fragment %q", tc.doc, tc.fragment)
			}
			if !tc.wantPresent && found {
				t.Errorf("%s contains forbidden fragment %q", tc.doc, tc.fragment)
			}
		})
	}
}

// helmValueFilesApps lists every Helm-based foundational app converted to the
// base.yaml + overlays/*.yaml valueFiles seam (issue #406), with a signature
// string expected in its rendered values/<app>/base.yaml. Extended as each
// app converts.
var helmValueFilesApps = []struct {
	app       string
	signature string
}{
	{"envoy-gateway", "controllerName: gateway.envoyproxy.io/gatewayclass-controller"},
	{"cert-manager", "installCRDs: true"},
	{"cloudnative-pg", "Operator-only install: per-database Cluster resources"},
	{"trust-manager", "The default CA package (debian ca-certificates)"},
	{"opentelemetry-collector", "repository: otel/opentelemetry-collector-k8s"},
	{"keycloak", "name: KEYCLOAK_ADMIN"},
	{"nebari-landingpage", "existingSecret: \"nebari-landing-redis\""},
}

// seamTemplateData returns TemplateData populated enough that every Helm
// app's template and base.yaml render with no unresolved placeholders.
func seamTemplateData() TemplateData {
	return TemplateData{
		Domain:                       "test.example.com",
		StorageClass:                 "gp2",
		GitRepoURL:                   "https://github.com/example/repo",
		GitBranch:                    "main",
		KeycloakNamespace:            "keycloak",
		KeycloakServiceURL:           "http://keycloak-keycloakx-http.keycloak.svc.cluster.local:8080",
		KeycloakIssuerURL:            "https://keycloak.test.example.com",
		KeycloakRealm:                "nebari",
		KeycloakAdminSecretName:      "keycloak-admin",
		KeycloakAdminSecretNamespace: "keycloak",
	}
}

func TestHelmApps_ValueFilesOverlaySeam(t *testing.T) {
	data := seamTemplateData()

	for _, tc := range helmValueFilesApps {
		t.Run(tc.app, func(t *testing.T) {
			content, err := templates.ReadFile("templates/apps/" + tc.app + ".yaml")
			if err != nil {
				t.Fatalf("read app template: %v", err)
			}
			processed, err := processTemplate("apps/"+tc.app+".yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}

			var app map[string]any
			if err := yaml.Unmarshal(processed, &app); err != nil {
				t.Fatalf("rendered Application is not valid YAML: %v\n%s", err, processed)
			}
			spec, _ := app["spec"].(map[string]any)
			sources, ok := spec["sources"].([]any)
			if !ok {
				t.Fatalf("expected spec.sources list (multi-source), got source=%#v sources=%#v",
					spec["source"], spec["sources"])
			}

			if len(sources) == 0 {
				t.Fatalf("spec.sources is empty in:\n%s", processed)
			}
			first, _ := sources[0].(map[string]any)
			if h, ok := first["helm"].(map[string]any); !ok || h["valueFiles"] == nil {
				t.Errorf("sources[0] must be the chart source carrying helm.valueFiles, got: %#v", first)
			}

			for i, s := range sources {
				m, _ := s.(map[string]any)
				if h, ok := m["helm"].(map[string]any); ok {
					if _, hasInline := h["values"]; hasInline {
						t.Errorf("sources[%d] has inline helm.values (takes precedence over valueFiles, breaks the overlay seam)", i)
					}
				}
			}

			var refSource, helmSource map[string]any
			for _, s := range sources {
				m, _ := s.(map[string]any)
				if m["ref"] == "values" {
					refSource = m
				}
				if h, ok := m["helm"].(map[string]any); ok && h["valueFiles"] != nil {
					helmSource = m
				}
			}
			if refSource == nil {
				t.Fatalf("no source with ref: values in:\n%s", processed)
			}
			if refSource["repoURL"] != data.GitRepoURL {
				t.Errorf("ref source repoURL = %v, want %v", refSource["repoURL"], data.GitRepoURL)
			}
			if refSource["targetRevision"] != data.GitBranch {
				t.Errorf("ref source targetRevision = %v, want %v", refSource["targetRevision"], data.GitBranch)
			}
			if helmSource == nil {
				t.Fatalf("no source with helm.valueFiles in:\n%s", processed)
			}

			helm := helmSource["helm"].(map[string]any)
			if helm["ignoreMissingValueFiles"] != true {
				t.Errorf("ignoreMissingValueFiles = %v, want true", helm["ignoreMissingValueFiles"])
			}
			wantFiles := []string{
				"$values/values/" + tc.app + "/base.yaml",
				"$values/values/" + tc.app + "/overlays/*.yaml",
			}
			vf, _ := helm["valueFiles"].([]any)
			if len(vf) != len(wantFiles) {
				t.Fatalf("valueFiles = %v, want %v", vf, wantFiles)
			}
			for i, want := range wantFiles {
				if vf[i] != want {
					t.Errorf("valueFiles[%d] = %v, want %q", i, vf[i], want)
				}
			}

			// base.yaml template exists, renders to non-empty valid YAML with
			// no unresolved placeholders, and carries this app's signature.
			baseRaw, err := templates.ReadFile("templates/values/" + tc.app + "/base.yaml")
			if err != nil {
				t.Fatalf("read values/%s/base.yaml template: %v", tc.app, err)
			}
			rendered, err := processTemplate("values/"+tc.app+"/base.yaml", baseRaw, data)
			if err != nil {
				t.Fatalf("render base.yaml: %v", err)
			}
			var vals map[string]any
			if err := yaml.Unmarshal(rendered, &vals); err != nil {
				t.Fatalf("rendered base.yaml is not valid YAML: %v\n%s", err, rendered)
			}
			if len(vals) == 0 {
				t.Error("rendered base.yaml is empty")
			}
			if strings.Contains(string(rendered), "{{") {
				t.Errorf("rendered base.yaml has unresolved placeholders:\n%s", rendered)
			}
			if !strings.Contains(string(rendered), tc.signature) {
				t.Errorf("rendered base.yaml missing signature %q:\n%s", tc.signature, rendered)
			}
		})
	}
}

func TestHelmApps_ValueFilesRespectGitPath(t *testing.T) {
	data := seamTemplateData()
	data.GitPath = "clusters/prod"

	for _, tc := range helmValueFilesApps {
		t.Run(tc.app, func(t *testing.T) {
			content, err := templates.ReadFile("templates/apps/" + tc.app + ".yaml")
			if err != nil {
				t.Fatalf("read app template: %v", err)
			}
			processed, err := processTemplate("apps/"+tc.app+".yaml", content, data)
			if err != nil {
				t.Fatalf("processTemplate() error: %v", err)
			}
			for _, want := range []string{
				"$values/clusters/prod/values/" + tc.app + "/base.yaml",
				"$values/clusters/prod/values/" + tc.app + "/overlays/*.yaml",
			} {
				if !strings.Contains(string(processed), want) {
					t.Errorf("rendered app missing GitPath-prefixed path %q:\n%s", want, processed)
				}
			}
		})
	}
}

func TestWriteAllToGit_GatedValuesBase(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled gates remove base.yaml but preserve overlays", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Seed a user overlay and a stale base.yaml from a previous
		// enabled-state run.
		overlayDir := filepath.Join(tmpDir, "values", "trust-manager", "overlays")
		if err := os.MkdirAll(overlayDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(overlayDir, "50-user.yaml"), []byte("user: overlay\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "values", "trust-manager", "base.yaml"), []byte("stale: true\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{StorageClass: "gp2"}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil { // trustBundlePEM="" => TrustManagerEnabled=false
			t.Fatalf("WriteAllToGit() error: %v", err)
		}

		basePath := filepath.Join(tmpDir, "values", "trust-manager", "base.yaml")
		if _, err := os.Stat(basePath); !os.IsNotExist(err) {
			t.Error("stale base.yaml should be removed when the app is disabled")
		}
		overlay, err := os.ReadFile(filepath.Join(tmpDir, "values", "trust-manager", "overlays", "50-user.yaml")) //nolint:gosec // path is t.TempDir() + constant
		if err != nil {
			t.Fatalf("user overlay was destroyed: %v", err)
		}
		if string(overlay) != "user: overlay\n" {
			t.Errorf("user overlay content changed: %q", overlay)
		}
	})

	t.Run("enabled gates write base.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		settings := cluster.InfraSettings{StorageClass: "gp2"}
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, testCAPEM); err != nil { // PEM => TrustManagerEnabled=true
			t.Fatalf("WriteAllToGit() error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "values", "trust-manager", "base.yaml")); err != nil {
			t.Errorf("expected values/trust-manager/base.yaml to be written: %v", err)
		}
	})
}

// lookupDirEntry returns the fs.DirEntry named name inside parent, failing the
// test if it is absent or not a directory. Named lookup rather than indexing so
// a change to the surrounding fixture fails as a test failure with a useful
// message instead of an index panic.
func lookupDirEntry(t *testing.T, parent, name string) fs.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == name {
			if !e.IsDir() {
				t.Fatalf("%s/%s exists but is not a directory", parent, name)
			}
			return e
		}
	}
	t.Fatalf("no directory entry %q under %s", name, parent)
	return nil
}

// TestRemoveStaleTemplate_RefusesValuesDirRecursion pins the structural guard
// that makes the os.RemoveAll footgun a no-op instead of data loss. A gate
// predicate written in the natural but unsafe prefix form
// (strings.HasPrefix(relPath, "values/<app>")) matches the values/<app>
// DIRECTORY as well as its base.yaml, which previously routed the directory
// into the recursive branch and deleted user overlays alongside it. The guard
// must refuse recursion under values/ and descend instead, so the per-file gate
// still removes base.yaml. Mutation check: dropping the guard branch in
// removeStaleTemplate silently destroys the overlay via os.RemoveAll and then
// returns fs.SkipDir, so the t.Fatalf on the removeStaleTemplate() call fires
// first and reports the failure; the overlay-survival assertion after it is
// the second, unreached witness of the same loss.
func TestRemoveStaleTemplate_RefusesValuesDirRecursion(t *testing.T) {
	tmpDir := t.TempDir()

	valuesAppDir := filepath.Join(tmpDir, "values", "metallb")
	overlayDir := filepath.Join(valuesAppDir, "overlays")
	if err := os.MkdirAll(overlayDir, 0o750); err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(overlayDir, "50-user.yaml")
	if err := os.WriteFile(overlayPath, []byte("speaker:\n  logLevel: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dirEntry := lookupDirEntry(t, filepath.Join(tmpDir, "values"), "metallb")

	// Want exactly nil. fs.SkipDir is non-nil, so this single assertion covers
	// both halves of the guarantee: no error, and no SkipDir either, which is
	// what lets the walk descend and reach base.yaml.
	if err := removeStaleTemplate("values/metallb", valuesAppDir, dirEntry); err != nil {
		t.Fatalf("removeStaleTemplate() error = %v, want nil (fs.SkipDir would mean base.yaml is never visited)", err)
	}
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("user overlay was destroyed by the recursive branch: %v", err)
	}

	// The subtree root itself: "values" has no trailing slash, so it is not
	// covered by the values/ prefix test alone. A whole-tree predicate match
	// routed through RemoveAll here would delete every base.yaml and overlay
	// in the repo at once.
	rootEntry := lookupDirEntry(t, tmpDir, "values")
	if err := removeStaleTemplate("values", filepath.Join(tmpDir, "values"), rootEntry); err != nil {
		t.Fatalf("removeStaleTemplate() on the values root error = %v, want nil", err)
	}
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("user overlay was destroyed via the bare values root: %v", err)
	}

	// A directory outside values/ is still removed recursively.
	otherDir := filepath.Join(tmpDir, "manifests", "metallb")
	if err := os.MkdirAll(otherDir, 0o750); err != nil {
		t.Fatal(err)
	}
	otherEntry := lookupDirEntry(t, filepath.Join(tmpDir, "manifests"), "metallb")
	if err := removeStaleTemplate("manifests/metallb", otherDir, otherEntry); !errors.Is(err, fs.SkipDir) {
		t.Errorf("removeStaleTemplate() for a non-values dir error = %v, want fs.SkipDir", err)
	}
	if _, err := os.Stat(otherDir); !os.IsNotExist(err) {
		t.Error("expected a non-values stale directory to be removed recursively")
	}
}

// TestWriteAllToGit_PreservesOverlays pins the core #406 invariant: nothing
// under values/<app>/overlays/ is ever written or deleted by NIC, across
// repeated regeneration runs.
//
// Scope note: this covers only the UNGATED case. envoy-gateway is never matched
// by isTrustBundlePath/isLonghornOnlyPath, so this test does not exercise the
// gated-off removal path at all. The file-versus-directory gating regression is
// pinned by TestWriteAllToGit_GatedValuesBase, and the structural guard behind
// it by TestRemoveStaleTemplate_RefusesValuesDirRecursion. Do not de-scope
// either of those on the assumption that this test covers them.
func TestWriteAllToGit_PreservesOverlays(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	overlayDir := filepath.Join(tmpDir, "values", "envoy-gateway", "overlays")
	if err := os.MkdirAll(overlayDir, 0o750); err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(overlayDir, "30-llm.yaml")
	overlayContent := []byte("config:\n  envoyGateway:\n    extensionManager: {}\n")
	if err := os.WriteFile(overlayPath, overlayContent, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.NebariConfig{Domain: "test.example.com"}
	settings := cluster.InfraSettings{StorageClass: "gp2"}

	// Bootstrap, then regen (WriteAllToGit is the shared path for both).
	for i := 0; i < 2; i++ {
		if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
			t.Fatalf("WriteAllToGit() run %d error: %v", i+1, err)
		}
	}

	got, err := os.ReadFile(overlayPath) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatalf("overlay file was removed: %v", err)
	}
	if !bytes.Equal(got, overlayContent) {
		t.Errorf("overlay file was modified:\ngot:  %q\nwant: %q", got, overlayContent)
	}

	// And base.yaml was (re)written alongside it.
	if _, err := os.Stat(filepath.Join(tmpDir, "values", "envoy-gateway", "base.yaml")); err != nil {
		t.Errorf("expected values/envoy-gateway/base.yaml to be written: %v", err)
	}
}

// TestHelmApps_SeamInvariants makes the #406 seam self-enforcing for apps
// added later: every app template with a helm block must use valueFiles
// (never inline values), be enrolled in helmValueFilesApps, and every
// values/<app> template dir must correspond to an enrolled app.
func TestHelmApps_SeamInvariants(t *testing.T) {
	enrolled := make(map[string]bool, len(helmValueFilesApps))
	for _, tc := range helmValueFilesApps {
		enrolled[tc.app] = true
	}

	entries, err := fs.ReadDir(templates, "templates/apps")
	if err != nil {
		t.Fatalf("read templates/apps: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		app := strings.TrimSuffix(e.Name(), ".yaml")
		raw, err := templates.ReadFile("templates/apps/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		content := string(raw)

		if strings.Contains(content, "values:") || strings.Contains(content, "valuesObject:") {
			t.Errorf("%s: inline helm values (values:/valuesObject:) take precedence over valueFiles and break the overlay seam - use values/%s/base.yaml + overlays (#406)", e.Name(), app)
		}
		// parameters/fileParameters sit at the TOP of ArgoCD's Helm precedence
		// order (parameters > valuesObject > values > valueFiles), so they
		// outrank even an inline values block and defeat the seam more
		// thoroughly than the two checked above. Guarding only values: and
		// valuesObject: would leave the hole exactly where the strongest
		// override mechanism lives.
		if strings.Contains(content, "parameters:") || strings.Contains(content, "fileParameters:") {
			t.Errorf("%s: helm parameters:/fileParameters: are the highest-precedence override and silently outrank every overlay file - use values/%s/base.yaml + overlays (#406)", e.Name(), app)
		}
		hasHelm := strings.Contains(content, "helm:")
		if hasHelm && !strings.Contains(content, "valueFiles:") {
			t.Errorf("%s: helm block without valueFiles - every Helm app must use the overlay seam (#406)", e.Name())
		}
		if hasHelm && !enrolled[app] {
			t.Errorf("%s: Helm app not enrolled in helmValueFilesApps - add a table row with a signature", e.Name())
		}
	}

	valueDirs, err := fs.ReadDir(templates, "templates/values")
	if err != nil {
		t.Fatalf("read templates/values: %v", err)
	}
	for _, d := range valueDirs {
		if !d.IsDir() {
			continue
		}
		if !enrolled[d.Name()] {
			t.Errorf("templates/values/%s exists but %s is not enrolled in helmValueFilesApps", d.Name(), d.Name())
		}
	}
}

// TestWriteAllToGit_WritesValuesReadme pins that the in-repo contract doc for
// values/<app>/base.yaml vs overlays/ is generated so it's always present in
// the gitops repo, not just in this source tree.
func TestWriteAllToGit_WritesValuesReadme(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := &config.NebariConfig{Domain: "test.example.com"}
	settings := cluster.InfraSettings{StorageClass: "gp2"}
	if err := WriteAllToGit(ctx, tmpDir, cfg, nil, settings, ""); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "values", "README.md")) //nolint:gosec // path is t.TempDir() + constant
	if err != nil {
		t.Fatalf("values/README.md not written: %v", err)
	}
	for _, want := range []string{"overlays/", "base.yaml", "lexical"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("values/README.md missing %q", want)
		}
	}
}

// TestFoundationalResourceDefaults pins the audited resource defaults from
// issue #457 so regressions in the embedded templates fail loudly. Each
// wanted block is matched verbatim, indentation included. Helm-app values
// live in templates/values/<app>/base.yaml (the #406 overlay seam), so the
// blocks are pinned there; the nebari-operator entry stays in its manifest.
func TestFoundationalResourceDefaults(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     []string
	}{
		{
			name:     "cert-manager controller, webhook, cainjector",
			template: "templates/values/cert-manager/base.yaml",
			want: []string{
				"resources:\n  requests:\n    cpu: 25m\n    memory: 64Mi\n  limits:\n    cpu: 200m\n    memory: 256Mi",
				"webhook:\n  resources:\n    requests:\n      cpu: 10m\n      memory: 32Mi\n    limits:\n      cpu: 100m\n      memory: 128Mi",
				"cainjector:\n  resources:\n    requests:\n      cpu: 10m\n      memory: 64Mi\n    limits:\n      cpu: 200m\n      memory: 256Mi",
			},
		},
		{
			name:     "keycloak has no CPU limit",
			template: "templates/values/keycloak/base.yaml",
			want: []string{
				"resources:\n  requests:\n    cpu: 250m\n    memory: 1Gi\n  limits:\n    memory: 2Gi",
			},
		},
		{
			name:     "envoy gateway controller",
			template: "templates/values/envoy-gateway/base.yaml",
			want: []string{
				"    resources:\n      requests:\n        cpu: 50m\n        memory: 128Mi\n      limits:\n        cpu: 500m\n        memory: 512Mi",
			},
		},
		{
			name:     "opentelemetry collector",
			template: "templates/values/opentelemetry-collector/base.yaml",
			want: []string{
				// 256Mi request, not the 128Mi the kind audit measured: the
				// agent settles at ~231Mi on EKS.
				"resources:\n  requests:\n    cpu: 50m\n    memory: 256Mi\n  limits:\n    cpu: 250m\n    memory: 512Mi",
			},
		},
		{
			name:     "nebari-operator manager",
			template: "templates/manifests/nebari-operator/deployment-patch.yaml",
			want: []string{
				"          resources:\n            requests:\n              cpu: 10m\n              memory: 64Mi\n            limits:\n              cpu: 200m\n              memory: 128Mi",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := templates.ReadFile(tt.template)
			if err != nil {
				t.Fatalf("read %s: %v", tt.template, err)
			}
			for _, w := range tt.want {
				if !strings.Contains(string(content), w) {
					t.Errorf("%s missing expected block:\n%s", tt.template, w)
				}
			}
		})
	}
}

// TestKeycloakNoCPULimit guards the deliberate absence of a Keycloak CPU
// limit: logins are bursty and throttling hurts exactly when users pile in.
func TestKeycloakNoCPULimit(t *testing.T) {
	content, err := templates.ReadFile("templates/values/keycloak/base.yaml")
	if err != nil {
		t.Fatalf("read keycloak base values template: %v", err)
	}
	if strings.Contains(string(content), "cpu: 2000m") {
		t.Error("keycloak still has a CPU limit; #457 removes it so login bursts are not throttled")
	}
}

// TestEnvoyProxyDataPlaneResources pins the data-plane proxy sizing. Without
// an EnvoyProxy resource, Envoy Gateway defaults every provisioned proxy pod
// to a silent 512Mi memory request (#456 finding 4).
func TestEnvoyProxyDataPlaneResources(t *testing.T) {
	content, err := templates.ReadFile("templates/manifests/networking/envoyproxy.yaml")
	if err != nil {
		t.Fatalf("read envoyproxy manifest: %v", err)
	}
	s := string(content)
	for _, w := range []string{
		"kind: EnvoyProxy",
		"name: nebari-proxy-config",
		"namespace: envoy-gateway-system",
		"cpu: 100m",
		"memory: 128Mi",
		"memory: 512Mi",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("envoyproxy.yaml missing %q", w)
		}
	}
	if strings.Contains(s, "limits:\n              cpu:") {
		t.Error("data-plane proxy must not have a CPU limit (#457 policy)")
	}

	gc, err := templates.ReadFile("templates/manifests/networking/gatewayclass.yaml")
	if err != nil {
		t.Fatalf("read gatewayclass manifest: %v", err)
	}
	for _, w := range []string{"parametersRef:", "kind: EnvoyProxy", "name: nebari-proxy-config"} {
		if !strings.Contains(string(gc), w) {
			t.Errorf("gatewayclass.yaml missing %q", w)
		}
	}
}
