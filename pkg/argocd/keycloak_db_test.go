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
	// The PostgreSQL image must be pinned. Left unset, the operator picks its
	// own default, so the Postgres major version would shift on a
	// cloudnative-pg chart bump rather than on a deliberate change here.
	imageName, _ := spec["imageName"].(string)
	if !strings.HasPrefix(imageName, "ghcr.io/cloudnative-pg/postgresql:") {
		t.Errorf("imageName = %q, want an explicitly pinned ghcr.io/cloudnative-pg/postgresql tag", imageName)
	}
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
	// The Cluster must sync in an earlier wave than the keycloakx chart's
	// wave-0 resources. In a shared wave, a webhook-not-ready apply failure
	// leaves a server-side-applied StatefulSet that the sync then waits on
	// forever, because its keycloak-db-app Secret only this Cluster can
	// generate (issue #537).
	annotations, _ := meta["annotations"].(map[string]any)
	if wave, _ := annotations["argocd.argoproj.io/sync-wave"].(string); wave != "-1" {
		t.Errorf("sync-wave annotation = %q, want \"-1\" (must apply before the wave-0 StatefulSet)", wave)
	}
	resources, _ := spec["resources"].(map[string]any)
	requests, _ := resources["requests"].(map[string]any)
	limits, _ := resources["limits"].(map[string]any)
	if requests["memory"] != "512Mi" || limits["memory"] != "1Gi" {
		t.Errorf("resources requests/limits = %v/%v, want memory 512Mi/1Gi (carried over from the Bitnami sizing)", requests, limits)
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

// TestWriteAllToGit_KeycloakDBCredentialsFromSecret pins both database
// credentials to the CNPG-generated keycloak-db-app Secret. A literal username
// works only as long as it happens to equal initdb.owner in
// keycloak-db-cluster.yaml; if either side changes, Keycloak fails to
// authenticate with an opaque Postgres error that points nowhere near the
// cause. CNPG owns the credential material, so both keys are read from it.
func TestWriteAllToGit_KeycloakDBCredentialsFromSecret(t *testing.T) {
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

	env := keycloakExtraEnv(t, raw)
	for _, tc := range []struct{ name, key string }{
		{"KC_DB_USERNAME", "username"},
		{"KC_DB_PASSWORD", "password"},
	} {
		v, ok := env[tc.name]
		if !ok {
			t.Errorf("%s not set in the rendered keycloak extraEnv", tc.name)
			continue
		}
		if _, literal := v["value"]; literal {
			t.Errorf("%s is a literal value; it must come from the keycloak-db-app Secret", tc.name)
			continue
		}
		valueFrom, _ := v["valueFrom"].(map[string]any)
		ref, _ := valueFrom["secretKeyRef"].(map[string]any)
		if ref["name"] != "keycloak-db-app" || ref["key"] != tc.key {
			t.Errorf("%s secretKeyRef = %v, want name keycloak-db-app key %s", tc.name, ref, tc.key)
		}
	}
}

// keycloakExtraEnv digs the Keycloak chart's extraEnv out of a rendered
// Application and returns it keyed by variable name, so assertions can tell a
// literal apart from a secretKeyRef instead of matching indented substrings.
func keycloakExtraEnv(t *testing.T, rendered []byte) map[string]map[string]any {
	t.Helper()

	var app struct {
		Spec struct {
			Sources []struct {
				Chart string `yaml:"chart"`
				Helm  struct {
					Values string `yaml:"values"`
				} `yaml:"helm"`
			} `yaml:"sources"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(rendered, &app); err != nil {
		t.Fatalf("rendered keycloak app is not valid YAML: %v", err)
	}

	var values string
	for _, s := range app.Spec.Sources {
		if s.Chart == "keycloakx" {
			values = s.Helm.Values
		}
	}
	if values == "" {
		t.Fatal("rendered keycloak app has no keycloakx source with helm values")
	}

	var helmValues struct {
		ExtraEnv string `yaml:"extraEnv"`
	}
	if err := yaml.Unmarshal([]byte(values), &helmValues); err != nil {
		t.Fatalf("keycloakx helm values are not valid YAML: %v\n%s", err, values)
	}

	var envList []map[string]any
	if err := yaml.Unmarshal([]byte(helmValues.ExtraEnv), &envList); err != nil {
		t.Fatalf("keycloakx extraEnv is not a valid env list: %v\n%s", err, helmValues.ExtraEnv)
	}

	env := make(map[string]map[string]any, len(envList))
	for _, e := range envList {
		name, _ := e["name"].(string)
		env[name] = e
	}
	return env
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
