package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/provider"
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
	if !ok || len(sources) != 1 {
		t.Fatalf("expected exactly one source, got %v", spec["sources"])
	}
	src, _ := sources[0].(map[string]any)
	inLine, _ := src["inLine"].(string)
	if !strings.Contains(inLine, "BEGIN CERTIFICATE") {
		t.Errorf("inLine source did not preserve the PEM, got %q", inLine)
	}
	if _, ok := spec["target"].(map[string]any); !ok {
		t.Errorf("target block missing in:\n%s", processed)
	}
}

func TestWriteAllToGit_TrustManager(t *testing.T) {
	appPath := func(dir string) string {
		return filepath.Join(dir, "apps", "trust-manager.yaml")
	}
	bundlePath := func(dir string) string {
		return filepath.Join(dir, "manifests", "security", "trust-bundle", "bundle.yaml")
	}

	t.Run("skipped when no trust bundle", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		if err := WriteAllToGit(context.Background(), &mockGitClient{workDir: dir}, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}); err != nil {
			t.Fatalf("WriteAllToGit: %v", err)
		}
		if _, err := os.Stat(appPath(dir)); !os.IsNotExist(err) {
			t.Errorf("expected trust-manager app to be skipped, stat err = %v", err)
		}
		if _, err := os.Stat(bundlePath(dir)); !os.IsNotExist(err) {
			t.Errorf("expected trust-bundle manifest to be skipped, stat err = %v", err)
		}
	})

	t.Run("written when trust bundle set", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.NebariConfig{
			Domain:      "test.example.com",
			TrustBundle: &config.TrustBundleConfig{Inline: testCAPEM},
		}
		if err := WriteAllToGit(context.Background(), &mockGitClient{workDir: dir}, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}); err != nil {
			t.Fatalf("WriteAllToGit: %v", err)
		}
		if _, err := os.Stat(appPath(dir)); err != nil {
			t.Errorf("expected trust-manager app to be written: %v", err)
		}
		got, err := os.ReadFile(bundlePath(dir))
		if err != nil {
			t.Fatalf("read rendered bundle: %v", err)
		}
		if !strings.Contains(string(got), "BEGIN CERTIFICATE") {
			t.Errorf("rendered bundle missing PEM, got:\n%s", got)
		}
	})
}
