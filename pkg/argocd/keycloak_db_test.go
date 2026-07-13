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

// TestKeycloakDBClusterTemplate_PinsShape verifies the CNPG Cluster template
// is valid YAML and pins the identity and bootstrap the design specifies:
// database "keycloak" owned by user "keycloak", 10Gi storage, in the keycloak
// namespace. CNPG generates the app credentials in Secret "keycloak-db-app".
func TestKeycloakDBClusterTemplate_PinsShape(t *testing.T) {
	content, err := templates.ReadFile("templates/manifests/keycloak/keycloak-db-cluster.yaml")
	if err != nil {
		t.Fatalf("read keycloak-db-cluster template: %v", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("keycloak-db-cluster is not valid YAML: %v\n%s", err, content)
	}

	if doc["apiVersion"] != "postgresql.cnpg.io/v1" || doc["kind"] != "Cluster" {
		t.Errorf("want postgresql.cnpg.io/v1 Cluster, got %v %v", doc["apiVersion"], doc["kind"])
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta["name"] != "keycloak-db" {
		t.Errorf("name = %v, want keycloak-db", meta["name"])
	}
	if meta["namespace"] != "keycloak" {
		t.Errorf("namespace = %v, want keycloak", meta["namespace"])
	}
	spec, _ := doc["spec"].(map[string]any)
	storage, _ := spec["storage"].(map[string]any)
	if storage["size"] != "10Gi" {
		t.Errorf("storage size = %v, want 10Gi (matches the Bitnami PVC)", storage["size"])
	}
	bootstrap, _ := spec["bootstrap"].(map[string]any)
	initdb, _ := bootstrap["initdb"].(map[string]any)
	if initdb["database"] != "keycloak" || initdb["owner"] != "keycloak" {
		t.Errorf("initdb database/owner = %v/%v, want keycloak/keycloak", initdb["database"], initdb["owner"])
	}
	if !strings.Contains(string(content), "app.kubernetes.io/part-of: nebari-foundational") {
		t.Error("keycloak-db-cluster missing nebari-foundational label")
	}
}

// TestWriteAllToGit_KeycloakDBCluster verifies the manifest is rendered on
// every bootstrap with the provider storage class substituted.
func TestWriteAllToGit_KeycloakDBCluster(t *testing.T) {
	clusterPath := func(dir string) string {
		return filepath.Join(dir, "manifests", "keycloak", "keycloak-db-cluster.yaml")
	}

	dir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "test.example.com"}
	if err := WriteAllToGit(context.Background(), &mockGitClient{workDir: dir}, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, ""); err != nil {
		t.Fatalf("WriteAllToGit: %v", err)
	}
	got, err := os.ReadFile(clusterPath(dir))
	if err != nil {
		t.Fatalf("expected keycloak-db-cluster manifest to be written: %v", err)
	}
	if !strings.Contains(string(got), `storageClass: "gp2"`) {
		t.Errorf("rendered manifest missing substituted storage class, got:\n%s", got)
	}
	if strings.Contains(string(got), "{{") {
		t.Errorf("rendered manifest contains unprocessed template syntax:\n%s", got)
	}
}

// TestWriteAllToGit_KeycloakUsesCNPG verifies the rendered keycloak app
// connects to the CNPG cluster: host keycloak-db-rw, password from the
// CNPG-generated keycloak-db-app Secret (a secretKeyRef, never a literal),
// and no residue of the retired Bitnami wiring.
func TestWriteAllToGit_KeycloakUsesCNPG(t *testing.T) {
	keycloakPath := func(dir string) string {
		return filepath.Join(dir, "apps", "keycloak.yaml")
	}

	dir := t.TempDir()
	cfg := &config.NebariConfig{Domain: "test.example.com"}
	if err := WriteAllToGit(context.Background(), &mockGitClient{workDir: dir}, cfg, nil, provider.InfraSettings{StorageClass: "gp2"}, ""); err != nil {
		t.Fatalf("WriteAllToGit: %v", err)
	}
	raw, err := os.ReadFile(keycloakPath(dir))
	if err != nil {
		t.Fatalf("read rendered keycloak app: %v", err)
	}
	got := string(raw)

	if !strings.Contains(got, "value: keycloak-db-rw.keycloak.svc.cluster.local") {
		t.Error("KC_DB_URL_HOST does not point at the CNPG keycloak-db-rw service")
	}
	if !strings.Contains(got, "name: keycloak-db-app") {
		t.Error("KC_DB_PASSWORD does not reference the CNPG-generated keycloak-db-app Secret")
	}
	if strings.Contains(got, "keycloak-postgresql-credentials") {
		t.Error("rendered keycloak app still references the retired keycloak-postgresql-credentials Secret")
	}
	if strings.Contains(got, "postgresql.keycloak.svc") {
		t.Error("rendered keycloak app still points at the retired Bitnami postgresql service")
	}
}

// TestApplications_NoBitnamiPostgresql pins the retirement of the Bitnami
// postgresql app: fresh bootstraps must not emit it. Existing gitops repos
// keep their committed copy (the writer never deletes committed files).
func TestApplications_NoBitnamiPostgresql(t *testing.T) {
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}
	for _, app := range apps {
		if app == "postgresql" {
			t.Error("Applications() still lists the retired Bitnami postgresql app")
		}
	}
}
