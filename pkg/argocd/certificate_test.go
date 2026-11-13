package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

func TestNewTemplateData_Certificate(t *testing.T) {
	settings := cluster.InfraSettings{}

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
	if err := WriteAllToGit(ctx, tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
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
	if err := WriteAllToGit(ctx, tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
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

func TestWriteAllToGit_SelectsCertificateIssuer(t *testing.T) {
	tests := []struct {
		name             string
		certificate      *config.CertificateConfig
		wantSelfSigned   bool
		wantRootCA       bool
		wantLetsEncrypt  bool
		wantOperatorName string
		wantACMEEmail    string
		wantACMEServer   string
	}{
		{
			name:             "default uses the root CA",
			wantSelfSigned:   true,
			wantRootCA:       true,
			wantOperatorName: certificateIssuerCA,
		},
		{
			name:             "selfsigned uses the root CA",
			certificate:      &config.CertificateConfig{Type: config.CertificateTypeSelfSigned},
			wantSelfSigned:   true,
			wantRootCA:       true,
			wantOperatorName: certificateIssuerCA,
		},
		{
			name: "letsencrypt uses only letsencrypt",
			certificate: &config.CertificateConfig{
				Type: config.CertificateTypeLetsEncrypt,
				ACME: &config.ACMEConfig{Email: "admin@example.com"},
			},
			wantLetsEncrypt:  true,
			wantOperatorName: certificateIssuerLetsEncrypt,
			wantACMEEmail:    "admin@example.com",
			wantACMEServer:   "https://acme-v02.api.letsencrypt.org/directory",
		},
		{
			name: "existing keeps selfsigned for operator managed certificates",
			certificate: &config.CertificateConfig{
				Type:           config.CertificateTypeExisting,
				ExistingSecret: &config.ExistingSecretRef{Name: "user-tls"},
			},
			wantSelfSigned:   true,
			wantOperatorName: certificateIssuerSelfSigned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			for _, path := range []string{selfSignedIssuerPath, letsencryptIssuerPath, rootCACertificatePath, caIssuerPath} {
				fullPath := filepath.Join(tmpDir, path)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0750); err != nil {
					t.Fatalf("create stale issuer directory: %v", err)
				}
				if err := os.WriteFile(fullPath, []byte("stale"), 0600); err != nil {
					t.Fatalf("create stale issuer manifest: %v", err)
				}
			}
			cfg := &config.NebariConfig{Domain: "example.com", Certificate: tt.certificate}
			if err := WriteAllToGit(context.Background(), tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
				t.Fatalf("WriteAllToGit() error: %v", err)
			}

			paths := []struct {
				name string
				path string
				want bool
			}{
				{name: "selfsigned issuer", path: selfSignedIssuerPath, want: tt.wantSelfSigned},
				{name: "root CA certificate", path: rootCACertificatePath, want: tt.wantRootCA},
				{name: "CA issuer", path: caIssuerPath, want: tt.wantRootCA},
				{name: "letsencrypt issuer", path: letsencryptIssuerPath, want: tt.wantLetsEncrypt},
				{name: "cluster issuers application", path: "apps/cluster-issuers.yaml", want: true},
			}
			for _, path := range paths {
				_, err := os.Stat(filepath.Join(tmpDir, path.path))
				if path.want && err != nil {
					t.Errorf("expected %s to be rendered: %v", path.name, err)
				}
				if !path.want && !os.IsNotExist(err) {
					t.Errorf("expected %s to be skipped, stat error = %v", path.name, err)
				}
			}

			if tt.wantLetsEncrypt {
				// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
				issuer, err := os.ReadFile(filepath.Join(tmpDir, letsencryptIssuerPath))
				if err != nil {
					t.Fatalf("read letsencrypt issuer: %v", err)
				}
				for _, want := range []string{
					"email: " + tt.wantACMEEmail,
					"server: " + tt.wantACMEServer,
				} {
					if !strings.Contains(string(issuer), want) {
						t.Errorf("letsencrypt issuer missing %q:\n%s", want, issuer)
					}
				}
			}

			// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
			operatorValues, err := os.ReadFile(filepath.Join(tmpDir, "values", "nebari-operator", "base.yaml"))
			if err != nil {
				t.Fatalf("read operator values: %v", err)
			}
			wantIssuerValue := `TLS_CLUSTER_ISSUER_NAME: "` + tt.wantOperatorName + `"`
			if !strings.Contains(string(operatorValues), wantIssuerValue) {
				t.Errorf("operator values missing %q", wantIssuerValue)
			}

			if tt.certificate == nil || tt.certificate.Type != config.CertificateTypeExisting {
				// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
				gatewayCert, err := os.ReadFile(filepath.Join(tmpDir, gatewayCertificatePath))
				if err != nil {
					t.Fatalf("read gateway certificate: %v", err)
				}
				wantGatewayIssuer := "name: " + tt.wantOperatorName
				if !strings.Contains(string(gatewayCert), wantGatewayIssuer) {
					t.Errorf("gateway certificate missing issuer %q:\n%s", wantGatewayIssuer, gatewayCert)
				}
			}
		})
	}
}

func TestWriteAllToGit_RootCA(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "nebari.example.com"}
	if err := WriteAllToGit(context.Background(), tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
	rootCA, err := os.ReadFile(filepath.Join(tmpDir, rootCACertificatePath))
	if err != nil {
		t.Fatalf("read root CA certificate: %v", err)
	}
	for _, want := range []string{
		"kind: Certificate",
		"name: " + rootCASecretName,
		"namespace: cert-manager",
		"isCA: true",
		"secretName: " + rootCASecretName,
		`- "nebari.example.com"`,
		"name: " + certificateIssuerSelfSigned,
	} {
		if !strings.Contains(string(rootCA), want) {
			t.Errorf("root CA certificate missing %q:\n%s", want, rootCA)
		}
	}

	// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
	caIssuer, err := os.ReadFile(filepath.Join(tmpDir, caIssuerPath))
	if err != nil {
		t.Fatalf("read CA issuer: %v", err)
	}
	for _, want := range []string{
		"kind: ClusterIssuer",
		"name: " + certificateIssuerCA,
		"secretName: " + rootCASecretName,
	} {
		if !strings.Contains(string(caIssuer), want) {
			t.Errorf("CA issuer missing %q:\n%s", want, caIssuer)
		}
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
	if err := WriteAllToGit(ctx, tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	rgPath := filepath.Join(tmpDir, "manifests", "networking", "gateway-tls-referencegrant.yaml")
	if _, err := os.Stat(rgPath); os.IsNotExist(err) {
		t.Error("expected ReferenceGrant to be rendered for cross-namespace existing_secret")
	}
}
