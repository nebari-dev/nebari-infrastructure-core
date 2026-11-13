package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	provider "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

const testCAPEM = `-----BEGIN CERTIFICATE-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0123456789==
-----END CERTIFICATE-----
`

// TestTrustBundleTemplate_RendersValidYAML verifies the inline PEM is indented
// correctly so the Bundle manifest parses as YAML and carries the certificate.
func TestTrustBundleTemplate_RendersValidYAML(t *testing.T) {
	data := TemplateData{TrustManagerEnabled: true, TrustBundlePEM: testCAPEM}

	content, err := templates.ReadFile("templates/manifests/security/trust-bundle/bundle.yaml")
	if err != nil {
		t.Fatalf("read bundle template: %v", err)
	}
	processed, err := processTemplate("manifests/security/trust-bundle/bundle.yaml", content, data)
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(processed, &doc); err != nil {
		t.Fatalf("rendered Bundle is not valid YAML: %v\n%s", err, processed)
	}

	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec missing or wrong type in:\n%s", processed)
	}
	sources, ok := spec["sources"].([]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("expected exactly two sources (useDefaultCAs + inLine), got %v", spec["sources"])
	}
	// System roots must be included alongside the org CA: consumers point
	// SSL_CERT_FILE-style vars at the projected file, which replaces the
	// default pool rather than extending it.
	defaultCAs, _ := sources[0].(map[string]any)
	if useDefault, _ := defaultCAs["useDefaultCAs"].(bool); !useDefault {
		t.Errorf("first source should set useDefaultCAs: true, got %v", sources[0])
	}
	src, _ := sources[1].(map[string]any)
	inLine, _ := src["inLine"].(string)
	if !strings.Contains(inLine, "BEGIN CERTIFICATE") {
		t.Errorf("inLine source did not preserve the PEM, got %q", inLine)
	}
	if _, ok := spec["target"].(map[string]any); !ok {
		t.Errorf("target block missing in:\n%s", processed)
	}
}

func TestWriteAllToGit_TrustManager(t *testing.T) {
	letsencrypt := &config.CertificateConfig{
		Type: config.CertificateTypeLetsEncrypt,
		ACME: &config.ACMEConfig{Email: "admin@example.com"},
	}
	existing := &config.CertificateConfig{
		Type:           config.CertificateTypeExisting,
		ExistingSecret: &config.ExistingSecretRef{Name: "user-tls"},
	}

	tests := []struct {
		name        string
		certificate *config.CertificateConfig
		orgCAPEM    string
		wantWritten bool
		wantRootCA  bool
		wantOrgCA   bool
	}{
		{
			name:        "selfsigned trusts the root CA",
			wantWritten: true,
			wantRootCA:  true,
		},
		{
			name:        "selfsigned with an org CA trusts both",
			orgCAPEM:    testCAPEM,
			wantWritten: true,
			wantRootCA:  true,
			wantOrgCA:   true,
		},
		{
			name:        "letsencrypt with an org CA trusts only the org CA",
			certificate: letsencrypt,
			orgCAPEM:    testCAPEM,
			wantWritten: true,
			wantOrgCA:   true,
		},
		{
			name:        "existing with an org CA trusts only the org CA",
			certificate: existing,
			orgCAPEM:    testCAPEM,
			wantWritten: true,
			wantOrgCA:   true,
		},
		{
			name:        "letsencrypt without an org CA skips trust-manager",
			certificate: letsencrypt,
		},
		{
			name:        "existing without an org CA skips trust-manager",
			certificate: existing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.NebariConfig{Domain: "test.example.com", Certificate: tt.certificate}
			if err := WriteAllToGit(context.Background(), dir, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, tt.orgCAPEM); err != nil {
				t.Fatalf("WriteAllToGit: %v", err)
			}

			for _, path := range []string{"apps/trust-manager.yaml", "apps/trust-bundle.yaml", "values/trust-manager/base.yaml"} {
				_, err := os.Stat(filepath.Join(dir, path))
				if tt.wantWritten && err != nil {
					t.Errorf("expected %s to be written: %v", path, err)
				}
				if !tt.wantWritten && !os.IsNotExist(err) {
					t.Errorf("expected %s to be skipped, stat err = %v", path, err)
				}
			}

			bundlePath := filepath.Join(dir, "manifests", "security", "trust-bundle", "bundle.yaml")
			// #nosec G304 -- the path is fixed beneath the test-owned t.TempDir.
			got, err := os.ReadFile(bundlePath)
			if !tt.wantWritten {
				if !os.IsNotExist(err) {
					t.Errorf("expected bundle manifest to be skipped, stat err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("read rendered bundle: %v", err)
			}

			var doc map[string]any
			if err := yaml.Unmarshal(got, &doc); err != nil {
				t.Fatalf("rendered Bundle is not valid YAML: %v\n%s", err, got)
			}
			spec, _ := doc["spec"].(map[string]any)
			sources, _ := spec["sources"].([]any)

			var hasDefaultCAs, hasRootCA, hasOrgCA bool
			for _, s := range sources {
				src, _ := s.(map[string]any)
				if useDefault, _ := src["useDefaultCAs"].(bool); useDefault {
					hasDefaultCAs = true
				}
				if secret, ok := src["secret"].(map[string]any); ok {
					hasRootCA = secret["name"] == rootCASecretName && secret["key"] == "ca.crt"
				}
				if inLine, ok := src["inLine"].(string); ok {
					hasOrgCA = strings.Contains(inLine, "BEGIN CERTIFICATE")
				}
			}
			if !hasDefaultCAs {
				t.Errorf("bundle must always include the default CAs, got sources %v", sources)
			}
			if hasRootCA != tt.wantRootCA {
				t.Errorf("bundle includes the root CA = %v, want %v:\n%s", hasRootCA, tt.wantRootCA, got)
			}
			if hasOrgCA != tt.wantOrgCA {
				t.Errorf("bundle includes the org CA = %v, want %v:\n%s", hasOrgCA, tt.wantOrgCA, got)
			}
		})
	}
}
