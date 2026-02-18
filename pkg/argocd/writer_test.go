package argocd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
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

func (m *mockGitClient) ValidateAuth(_ context.Context) error          { return nil }
func (m *mockGitClient) Init(_ context.Context) error                  { return nil }
func (m *mockGitClient) WorkDir() string                               { return m.workDir }
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
	err := WriteAllToGit(ctx, client, cfg)
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// The _nooverwrite_overrides.yaml should have been written as overrides.yaml
	overridesPath := filepath.Join(tmpDir, "manifests", "opentelemetry-collector", "overrides.yaml")
	content, err := os.ReadFile(overridesPath)
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
	err := WriteAllToGit(ctx, client, cfg)
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// Verify the file still has the custom content
	content, err := os.ReadFile(overridesPath)
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

	err := WriteAllToGit(ctx, client, cfg)
	if err != nil {
		t.Fatalf("WriteAllToGit() error: %v", err)
	}

	// Verify the OTel app template was rendered with multi-source config
	otelAppPath := filepath.Join(tmpDir, "apps", "opentelemetry-collector.yaml")
	content, err := os.ReadFile(otelAppPath)
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
	baseContent, err := os.ReadFile(baseValuesPath)
	if err != nil {
		t.Fatalf("values.yaml should exist in manifests: %v", err)
	}
	if !strings.Contains(string(baseContent), "otel/opentelemetry-collector-k8s") {
		t.Error("base values.yaml should contain the collector image config")
	}
}
