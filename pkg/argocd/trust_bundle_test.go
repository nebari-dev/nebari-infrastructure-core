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
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
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
	appPath := func(dir string) string {
		return filepath.Join(dir, "apps", "trust-manager.yaml")
	}
	bundlePath := func(dir string) string {
		return filepath.Join(dir, "manifests", "security", "trust-bundle", "bundle.yaml")
	}

	t.Run("skipped when no trust bundle", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		if err := WriteAllToGit(context.Background(), dir, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, ""); err != nil {
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
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		if err := WriteAllToGit(context.Background(), dir, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, testCAPEM); err != nil {
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

	// Operators need a visible signal that the bundle was actually wired into
	// trust-manager; the orchestrator owns the "nothing configured" message, so
	// this layer stays quiet in the no-op case.
	captureTrustBundleUpdates := func(t *testing.T, pemText string) []status.Update {
		t.Helper()
		var updates []status.Update
		ctx, cleanup := status.StartHandler(context.Background(), func(u status.Update) {
			if u.Resource == "trust-bundle" {
				updates = append(updates, u)
			}
		})
		cfg := &config.NebariConfig{Domain: "test.example.com"}
		if err := WriteAllToGit(ctx, t.TempDir(), cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, pemText); err != nil {
			t.Fatalf("WriteAllToGit: %v", err)
		}
		cleanup()
		return updates
	}

	t.Run("reports the bundle being wired into trust-manager", func(t *testing.T) {
		updates := captureTrustBundleUpdates(t, testCAPEM)
		if len(updates) != 1 {
			t.Fatalf("got %d trust-bundle status updates, want 1: %+v", len(updates), updates)
		}
		if updates[0].Level != status.LevelInfo {
			t.Errorf("level = %q, want %q", updates[0].Level, status.LevelInfo)
		}
		if !strings.Contains(updates[0].Message, "trust-manager") {
			t.Errorf("message should name trust-manager, got %q", updates[0].Message)
		}
	})

	t.Run("silent when no trust bundle", func(t *testing.T) {
		if updates := captureTrustBundleUpdates(t, ""); len(updates) != 0 {
			t.Errorf("expected no trust-bundle status updates, got %+v", updates)
		}
	})
}
