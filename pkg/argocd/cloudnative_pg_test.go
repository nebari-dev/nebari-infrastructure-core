package argocd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	provider "github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// TestCloudNativePGTemplate_PinsChartAndTarget verifies the app template is
// valid YAML and pins the chart, repo, version, and destination the design
// specifies. ServerSideApply is asserted because CNPG's CRDs overflow the
// client-side last-applied-configuration annotation.
func TestCloudNativePGTemplate_PinsChartAndTarget(t *testing.T) {
	content, err := templates.ReadFile("templates/apps/cloudnative-pg.yaml")
	if err != nil {
		t.Fatalf("read cloudnative-pg template: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("cloudnative-pg app is not valid YAML: %v\n%s", err, content)
	}

	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec missing or wrong type in:\n%s", content)
	}
	source, _ := spec["source"].(map[string]any)
	if source["chart"] != "cloudnative-pg" {
		t.Errorf("chart = %v, want cloudnative-pg", source["chart"])
	}
	if source["repoURL"] != "https://cloudnative-pg.github.io/charts" {
		t.Errorf("repoURL = %v, want the cloudnative-pg chart repo", source["repoURL"])
	}
	if source["targetRevision"] != "0.29.0" {
		t.Errorf("targetRevision = %v, want pinned chart version 0.29.0", source["targetRevision"])
	}
	dest, _ := spec["destination"].(map[string]any)
	if dest["namespace"] != "cnpg-system" {
		t.Errorf("destination namespace = %v, want cnpg-system", dest["namespace"])
	}
	if !strings.Contains(string(content), "ServerSideApply=true") {
		t.Error("cloudnative-pg app must sync with ServerSideApply=true (CNPG CRDs overflow client-side apply)")
	}
	if !strings.Contains(string(content), "app.kubernetes.io/part-of: nebari-foundational") {
		t.Error("cloudnative-pg app missing nebari-foundational label")
	}
}

// TestWriteAllToGit_CloudNativePGAlwaysWritten pins that the CNPG operator is
// unconditional foundational infrastructure: the app file is emitted for every
// GitOps bootstrap, with no config gating, like postgresql.yaml and
// keycloak.yaml.
func TestWriteAllToGit_CloudNativePGAlwaysWritten(t *testing.T) {
	appPath := func(dir string) string {
		return filepath.Join(dir, "apps", "cloudnative-pg.yaml")
	}

	dir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "test.example.com"}
	if err := WriteAllToGit(context.Background(), &mockGitClient{workDir: dir}, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, ""); err != nil {
		t.Fatalf("WriteAllToGit: %v", err)
	}
	got, err := os.ReadFile(appPath(dir))
	if err != nil {
		t.Fatalf("expected cloudnative-pg app to always be written: %v", err)
	}
	if !strings.Contains(string(got), "chart: cloudnative-pg") {
		t.Errorf("rendered app missing chart reference, got:\n%s", got)
	}
}
