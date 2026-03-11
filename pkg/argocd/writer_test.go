package argocd

import (
	"bytes"
	"context"
	"io"
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
			wantKBP:  "/auth", // defaults to /auth when not set by provider
		},
		{
			name: "hetzner with annotations and keycloak path",
			settings: provider.InfraSettings{
				StorageClass:            "hcloud-volumes",
				LoadBalancerAnnotations: map[string]string{"load-balancer.hetzner.cloud/location": "ash"},
				KeycloakBasePath:        "/auth",
			},
			wantSC:  "hcloud-volumes",
			wantLBA: 1,
			wantKBP: "/auth",
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
			wantKBP:  "/auth", // defaults to /auth when not set by provider
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

func TestNewTemplateData_KeycloakAuthURL(t *testing.T) {
	cfg := &config.NebariConfig{Provider: "hetzner", Domain: "test.example.com"}

	// When KeycloakBasePath is explicitly set, it should be honoured.
	settings := provider.InfraSettings{
		StorageClass:     "hcloud-volumes",
		KeycloakBasePath: "/auth",
	}
	data := NewTemplateData(cfg, settings)
	if !strings.HasSuffix(data.KeycloakAuthURL, "/auth") {
		t.Errorf("KeycloakAuthURL = %q, should end with /auth", data.KeycloakAuthURL)
	}

	// When KeycloakBasePath is empty (provider omits it), we default to /auth
	// because all NIC-managed Keycloak deployments use --http-relative-path=/auth.
	settings.KeycloakBasePath = ""
	data = NewTemplateData(cfg, settings)
	if !strings.HasSuffix(data.KeycloakAuthURL, "/auth") {
		t.Errorf("KeycloakAuthURL = %q, should end with /auth (default)", data.KeycloakAuthURL)
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

// nopWriteCloser wraps a bytes.Buffer to satisfy io.WriteCloser
type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}
