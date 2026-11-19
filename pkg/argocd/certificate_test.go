package argocd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

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
		wantTrustManager bool
		wantLetsEncrypt  bool
		wantGatewayCert  bool
		wantOperatorName string
		wantACMEEmail    string
		wantACMEServer   string
	}{
		{
			name:             "default uses the root CA",
			wantSelfSigned:   true,
			wantRootCA:       true,
			wantTrustManager: true,
			wantGatewayCert:  true,
			wantOperatorName: certificateIssuerCA,
		},
		{
			name:             "selfsigned uses the root CA",
			certificate:      &config.CertificateConfig{Type: config.CertificateTypeSelfSigned},
			wantSelfSigned:   true,
			wantRootCA:       true,
			wantTrustManager: true,
			wantGatewayCert:  true,
			wantOperatorName: certificateIssuerCA,
		},
		{
			name: "letsencrypt uses letsencrypt for public certificates and runs the internal CA",
			certificate: &config.CertificateConfig{
				Type: config.CertificateTypeLetsEncrypt,
				ACME: &config.ACMEConfig{Email: "admin@example.com"},
			},
			wantSelfSigned:   true,
			wantRootCA:       true,
			wantTrustManager: true,
			wantGatewayCert:  true,
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
			wantRootCA:       true,
			wantTrustManager: true,
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
				{name: "trust-manager application", path: "apps/trust-manager.yaml", want: tt.wantTrustManager},
				{name: "trust bundle application", path: "apps/trust-bundle.yaml", want: tt.wantTrustManager},
				{name: "trust bundle manifest", path: "manifests/security/trust-bundle/bundle.yaml", want: tt.wantTrustManager},
				{name: "gateway certificate", path: gatewayCertificatePath, want: tt.wantGatewayCert},
				{name: "certificates application", path: certificatesAppPath, want: tt.wantGatewayCert},
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

			if tt.wantGatewayCert {
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

func TestWriteAllToGit_RejectsUnknownCertificateType(t *testing.T) {
	cfg := &config.NebariConfig{
		Domain:      "example.com",
		Certificate: &config.CertificateConfig{Type: "vault"},
	}
	err := WriteAllToGit(context.Background(), t.TempDir(), cfg, nil, cluster.InfraSettings{}, "")
	if err == nil {
		t.Fatal("WriteAllToGit() succeeded for an unknown certificate type, want an error")
	}
	if !strings.Contains(err.Error(), `"vault"`) {
		t.Errorf("error %q does not name the certificate type", err)
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
		"name: " + certificateIssuerSelfSigned,
	} {
		if !strings.Contains(string(rootCA), want) {
			t.Errorf("root CA certificate missing %q:\n%s", want, rootCA)
		}
	}

	// The name constraints are what stop the root from vouching for other
	// sites, so check their structure, not just that the strings appear.
	var rootCADoc struct {
		Spec struct {
			NameConstraints *struct {
				Critical  bool `json:"critical"`
				Permitted struct {
					DNSDomains []string `json:"dnsDomains"`
				} `json:"permitted"`
			} `json:"nameConstraints"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(rootCA, &rootCADoc); err != nil {
		t.Fatalf("parse root CA certificate: %v", err)
	}
	constraints := rootCADoc.Spec.NameConstraints
	if constraints == nil {
		t.Fatalf("root CA certificate has no nameConstraints:\n%s", rootCA)
	}
	if !constraints.Critical {
		t.Error("root CA nameConstraints are not critical, so clients that don't support them would ignore them")
	}
	wantDomains := []string{"nebari.example.com", "svc", "svc.cluster.local"}
	if got := constraints.Permitted.DNSDomains; !slices.Equal(got, wantDomains) {
		t.Errorf("root CA permitted dnsDomains = %q, want %q", got, wantDomains)
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

// renderedPermittedDNSDomains parses a rendered root CA Certificate manifest
// and returns spec.nameConstraints.permitted.dnsDomains.
func renderedPermittedDNSDomains(t *testing.T, manifest []byte) []string {
	t.Helper()
	var cert struct {
		Spec struct {
			NameConstraints struct {
				Permitted struct {
					DNSDomains []string `json:"dnsDomains"`
				} `json:"permitted"`
			} `json:"nameConstraints"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(manifest, &cert); err != nil {
		t.Fatalf("parse root CA certificate: %v", err)
	}
	return cert.Spec.NameConstraints.Permitted.DNSDomains
}

func TestRootCANameConstraints_ServiceNames(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "nebari.example.com"}
	if err := WriteAllToGit(context.Background(), tmpDir, cfg, nil, cluster.InfraSettings{}, ""); err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}
	// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
	manifest, err := os.ReadFile(filepath.Join(tmpDir, rootCACertificatePath))
	if err != nil {
		t.Fatalf("read root CA certificate: %v", err)
	}
	permitted := renderedPermittedDNSDomains(t, manifest)

	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	now := time.Now()
	rootTmpl := &x509.Certificate{
		SerialNumber:                big.NewInt(1),
		Subject:                     pkix.Name{CommonName: "test root"},
		NotBefore:                   now.Add(-time.Hour),
		NotAfter:                    now.Add(time.Hour),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		KeyUsage:                    x509.KeyUsageCertSign,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         permitted,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root certificate: %v", err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)

	tests := []struct {
		name   string
		wantOK bool
	}{
		{name: "webhook.pack.svc", wantOK: true},
		{name: "webhook.pack.svc.cluster.local", wantOK: true},
		{name: "app.nebari.example.com", wantOK: true},
		{name: "example.com", wantOK: false},
		{name: "10-0-0-1.pack.pod.cluster.local", wantOK: false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatalf("generate leaf key: %v", err)
			}
			leafTmpl := &x509.Certificate{
				SerialNumber: big.NewInt(int64(i + 2)),
				Subject:      pkix.Name{CommonName: tt.name},
				NotBefore:    now.Add(-time.Hour),
				NotAfter:     now.Add(time.Hour),
				KeyUsage:     x509.KeyUsageDigitalSignature,
				ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				DNSNames:     []string{tt.name},
			}
			leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, root, &leafKey.PublicKey, rootKey)
			if err != nil {
				t.Fatalf("create leaf certificate: %v", err)
			}
			leaf, err := x509.ParseCertificate(leafDER)
			if err != nil {
				t.Fatalf("parse leaf certificate: %v", err)
			}
			_, err = leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: tt.name})
			if gotOK := err == nil; gotOK != tt.wantOK {
				t.Errorf("Verify(%q) error = %v, wantOK %v (permitted %q)", tt.name, err, tt.wantOK, permitted)
			}
		})
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
