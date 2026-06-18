package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
)

func TestNewTemplateData_Certificate(t *testing.T) {
	settings := provider.InfraSettings{}

	tests := []struct {
		name              string
		cert              *config.CertificateConfig
		wantSecretName    string
		wantSecretNS      string
		wantCrossNS       bool
		wantExisting      bool
		wantIssuerNonZero bool
	}{
		{
			name:              "no certificate uses defaults + selfsigned issuer",
			cert:              nil,
			wantSecretName:    "nebari-gateway-tls",
			wantSecretNS:      "envoy-gateway-system",
			wantCrossNS:       false,
			wantExisting:      false,
			wantIssuerNonZero: true,
		},
		{
			name:           "existing files source",
			cert:           &config.CertificateConfig{Type: "existing", Files: &config.CertFiles{CertFile: "/c", KeyFile: "/k"}},
			wantSecretName: "nebari-gateway-tls",
			wantSecretNS:   "envoy-gateway-system",
			wantCrossNS:    false,
			wantExisting:   true,
		},
		{
			name:           "existing files with custom secret name",
			cert:           &config.CertificateConfig{Type: "existing", SecretName: "custom-tls", Env: &config.CertEnv{CertEnv: "A", KeyEnv: "B"}},
			wantSecretName: "custom-tls",
			wantSecretNS:   "envoy-gateway-system",
			wantExisting:   true,
		},
		{
			name:           "existing_secret same namespace",
			cert:           &config.CertificateConfig{Type: "existing", ExistingSecret: &config.ExistingSecretRef{Name: "user-tls"}},
			wantSecretName: "user-tls",
			wantSecretNS:   "envoy-gateway-system",
			wantCrossNS:    false,
			wantExisting:   true,
		},
		{
			name:           "existing_secret cross namespace",
			cert:           &config.CertificateConfig{Type: "existing", ExistingSecret: &config.ExistingSecretRef{Name: "user-tls", Namespace: "user-ns"}},
			wantSecretName: "user-tls",
			wantSecretNS:   "user-ns",
			wantCrossNS:    true,
			wantExisting:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{Domain: "test.example.com", Certificate: tt.cert}
			data := NewTemplateData(cfg, nil, settings)

			if data.GatewayTLSSecretName != tt.wantSecretName {
				t.Errorf("GatewayTLSSecretName = %q, want %q", data.GatewayTLSSecretName, tt.wantSecretName)
			}
			if data.GatewayTLSSecretNamespace != tt.wantSecretNS {
				t.Errorf("GatewayTLSSecretNamespace = %q, want %q", data.GatewayTLSSecretNamespace, tt.wantSecretNS)
			}
			if data.GatewayTLSCrossNamespace != tt.wantCrossNS {
				t.Errorf("GatewayTLSCrossNamespace = %v, want %v", data.GatewayTLSCrossNamespace, tt.wantCrossNS)
			}
			if data.UseExistingCertificate != tt.wantExisting {
				t.Errorf("UseExistingCertificate = %v, want %v", data.UseExistingCertificate, tt.wantExisting)
			}
			if tt.wantIssuerNonZero && data.CertificateIssuer == "" {
				t.Error("CertificateIssuer should be set for non-existing types")
			}
		})
	}
}

func TestGatewayTemplate_CertificateRefName(t *testing.T) {
	content, err := templates.ReadFile("templates/manifests/networking/gateway.yaml")
	if err != nil {
		t.Fatalf("failed to read gateway template: %v", err)
	}

	t.Run("same namespace omits namespace field", func(t *testing.T) {
		data := TemplateData{
			Domain:                    "test.example.com",
			HTTPSPort:                 443,
			GatewayTLSSecretName:      "user-tls",
			GatewayTLSSecretNamespace: "envoy-gateway-system",
			GatewayTLSCrossNamespace:  false,
		}
		processed, err := processTemplate("manifests/networking/gateway.yaml", content, data)
		if err != nil {
			t.Fatalf("processTemplate() error: %v", err)
		}
		output := string(processed)
		if !strings.Contains(output, "name: user-tls") {
			t.Errorf("expected certificateRef name user-tls, got:\n%s", output)
		}
		// The certificateRef namespace is indented under the ref (12 spaces);
		// the Gateway's own metadata.namespace (2 spaces) is unrelated.
		if strings.Contains(output, "            namespace:") {
			t.Errorf("same-namespace ref should not set namespace on certificateRefs, got:\n%s", output)
		}
	})

	t.Run("cross namespace sets namespace field", func(t *testing.T) {
		data := TemplateData{
			Domain:                    "test.example.com",
			HTTPSPort:                 443,
			GatewayTLSSecretName:      "user-tls",
			GatewayTLSSecretNamespace: "user-ns",
			GatewayTLSCrossNamespace:  true,
		}
		processed, err := processTemplate("manifests/networking/gateway.yaml", content, data)
		if err != nil {
			t.Fatalf("processTemplate() error: %v", err)
		}
		output := string(processed)
		if !strings.Contains(output, "name: user-tls") {
			t.Errorf("expected certificateRef name user-tls, got:\n%s", output)
		}
		if !strings.Contains(output, "namespace: user-ns") {
			t.Errorf("cross-namespace ref should set namespace user-ns, got:\n%s", output)
		}
	})
}

func TestReferenceGrantTemplate(t *testing.T) {
	content, err := templates.ReadFile("templates/manifests/networking/gateway-tls-referencegrant.yaml")
	if err != nil {
		t.Fatalf("failed to read referencegrant template: %v", err)
	}
	data := TemplateData{
		GatewayTLSSecretName:      "user-tls",
		GatewayTLSSecretNamespace: "user-ns",
		GatewayTLSCrossNamespace:  true,
	}
	processed, err := processTemplate("manifests/networking/gateway-tls-referencegrant.yaml", content, data)
	if err != nil {
		t.Fatalf("processTemplate() error: %v", err)
	}
	output := string(processed)
	if !strings.Contains(output, "kind: ReferenceGrant") {
		t.Errorf("expected kind: ReferenceGrant, got:\n%s", output)
	}
	if !strings.Contains(output, "namespace: user-ns") {
		t.Errorf("ReferenceGrant should live in user-ns, got:\n%s", output)
	}
	if !strings.Contains(output, "name: user-tls") {
		t.Errorf("ReferenceGrant should grant access to user-tls, got:\n%s", output)
	}
	if !strings.Contains(output, "namespace: envoy-gateway-system") {
		t.Errorf("ReferenceGrant from clause should reference envoy-gateway-system, got:\n%s", output)
	}
}

func TestWriteAllToGit_SkipsCertManagerForExisting(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := &config.NebariConfig{
		Domain: "test.example.com",
		Certificate: &config.CertificateConfig{
			Type:           "existing",
			ExistingSecret: &config.ExistingSecretRef{Name: "user-tls"},
		},
	}
	mock := &mockGitClient{workDir: tmpDir}
	if err := WriteAllToGit(ctx, mock, cfg, nil, provider.InfraSettings{}); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	certPath := filepath.Join(tmpDir, "manifests", "security", "certificates", "gateway-certificate.yaml")
	if _, err := os.Stat(certPath); !os.IsNotExist(err) {
		t.Errorf("expected gateway-certificate.yaml to be skipped for type=existing, but it exists")
	}

	// The certificates Application must also be skipped, otherwise it would sync
	// an empty directory (allowEmpty: false) and report as failed.
	appPath := filepath.Join(tmpDir, "apps", "certificates.yaml")
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("expected apps/certificates.yaml to be skipped for type=existing, but it exists")
	}

	// Same-namespace existing_secret should not emit a ReferenceGrant.
	rgPath := filepath.Join(tmpDir, "manifests", "networking", "gateway-tls-referencegrant.yaml")
	if _, err := os.Stat(rgPath); !os.IsNotExist(err) {
		t.Errorf("expected no ReferenceGrant for same-namespace secret, but it exists")
	}
}

func TestWriteAllToGit_RendersCertManagerForSelfSigned(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := &config.NebariConfig{Domain: "test.example.com"}
	mock := &mockGitClient{workDir: tmpDir}
	if err := WriteAllToGit(ctx, mock, cfg, nil, provider.InfraSettings{}); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	certPath := filepath.Join(tmpDir, "manifests", "security", "certificates", "gateway-certificate.yaml")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("expected gateway-certificate.yaml to be rendered for selfsigned default")
	}
	appPath := filepath.Join(tmpDir, "apps", "certificates.yaml")
	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		t.Error("expected apps/certificates.yaml to be rendered for selfsigned default")
	}
}

func TestWriteAllToGit_RendersReferenceGrantCrossNamespace(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := &config.NebariConfig{
		Domain: "test.example.com",
		Certificate: &config.CertificateConfig{
			Type:           "existing",
			ExistingSecret: &config.ExistingSecretRef{Name: "user-tls", Namespace: "user-ns"},
		},
	}
	mock := &mockGitClient{workDir: tmpDir}
	if err := WriteAllToGit(ctx, mock, cfg, nil, provider.InfraSettings{}); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	rgPath := filepath.Join(tmpDir, "manifests", "networking", "gateway-tls-referencegrant.yaml")
	if _, err := os.Stat(rgPath); os.IsNotExist(err) {
		t.Error("expected ReferenceGrant to be rendered for cross-namespace existing_secret")
	}
}
