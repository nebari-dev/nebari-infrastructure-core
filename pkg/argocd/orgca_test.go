package argocd

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// entryByName finds a volume / volumeMount / env entry by its "name" key.
func entryByName(t *testing.T, list any, name string) map[string]any {
	t.Helper()
	items, ok := list.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", list)
	}
	for _, it := range items {
		if it["name"] == name {
			return it
		}
	}
	return nil
}

func TestAddOrgCAMount(t *testing.T) {
	values := map[string]any{}
	addOrgCAMount(context.Background(), values)

	repoServer, ok := values["repoServer"].(map[string]any)
	if !ok {
		t.Fatal("repoServer should be a map[string]any")
	}

	// Volumes: org-ca (configMap) + combined-ca (emptyDir).
	if v := entryByName(t, repoServer["volumes"], "org-ca"); v == nil {
		t.Fatal("org-ca volume missing")
	} else {
		cm, ok := v["configMap"].(map[string]any)
		if !ok || cm["name"] != "argocd-org-ca" {
			t.Errorf("org-ca volume should reference configMap argocd-org-ca, got %v", v["configMap"])
		}
	}
	if v := entryByName(t, repoServer["volumes"], "combined-ca"); v == nil {
		t.Fatal("combined-ca volume missing")
	} else if _, ok := v["emptyDir"].(map[string]any); !ok {
		t.Errorf("combined-ca volume should be an emptyDir, got %v", v)
	}

	// repo-server reads the combined bundle read-only.
	m := entryByName(t, repoServer["volumeMounts"], "combined-ca")
	if m == nil {
		t.Fatal("combined-ca volumeMount missing")
	}
	if m["mountPath"] != "/etc/ssl/certs-combined" || m["readOnly"] != true {
		t.Errorf("combined-ca mount = %v, want mountPath=/etc/ssl/certs-combined readOnly=true", m)
	}

	// Init container.
	init := entryByName(t, repoServer["initContainers"], "combine-ca-bundle")
	if init == nil {
		t.Fatal("combine-ca-bundle initContainer missing")
	}
	if !strings.Contains(init["image"].(string), ".Values.repoServer.image") {
		t.Errorf("init image should resolve from the chart's repo-server image, got %v", init["image"])
	}
	cmd, ok := init["command"].([]any)
	if !ok || len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
		t.Fatalf("init command should be [sh -c <script>], got %v", init["command"])
	}
	script := cmd[2].(string)
	for _, want := range []string{
		"/etc/ssl/certs/ca-certificates.crt",    // system bundle source
		"/etc/nebari/org-ca/ca.crt",             // mounted org CA
		"/etc/ssl/certs-combined/ca-bundle.crt", // combined output
	} {
		if !strings.Contains(script, want) {
			t.Errorf("init script missing %q; script=%q", want, script)
		}
	}
	if _, ok := init["securityContext"].(map[string]any); !ok {
		t.Error("init container should set a securityContext")
	}

	// Env: all three vars point at the combined bundle (literal path).
	const wantPath = "/etc/ssl/certs-combined/ca-bundle.crt"
	env := map[string]any{}
	for _, e := range repoServer["env"].([]map[string]any) {
		env[e["name"].(string)] = e["value"]
	}
	for _, name := range []string{"SSL_CERT_FILE", "GIT_SSL_CAINFO", "CURL_CA_BUNDLE"} {
		if env[name] != wantPath {
			t.Errorf("env %s = %v, want %s", name, env[name], wantPath)
		}
	}
}

// addOrgCAMount must preserve existing repoServer keys and coexist with the
// local-gitops mount (both append to repoServer.volumes).
func TestAddOrgCAMountPreservesAndCoexists(t *testing.T) {
	values := map[string]any{
		"repoServer": map[string]any{"replicas": 2},
	}
	addLocalGitopsMount(context.Background(), values, "/tmp/gitops")
	addOrgCAMount(context.Background(), values)

	repoServer := values["repoServer"].(map[string]any)
	if repoServer["replicas"] != 2 {
		t.Errorf("existing replicas key was clobbered, got %v", repoServer["replicas"])
	}
	for _, name := range []string{"local-gitops", "org-ca", "combined-ca"} {
		if entryByName(t, repoServer["volumes"], name) == nil {
			t.Errorf("volume %q missing after both mounts applied", name)
		}
	}
}

func TestCreateOrUpdateConfigMap(t *testing.T) {
	const ns = "argocd"
	newCM := func(data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd-org-ca", Namespace: ns},
			Data:       map[string]string{"ca.crt": data},
		}
	}

	t.Run("creates when absent", func(t *testing.T) {
		client := fake.NewSimpleClientset() //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests
		if err := createOrUpdateConfigMap(context.Background(), client, newCM("CA-A")); err != nil {
			t.Fatalf("createOrUpdateConfigMap: %v", err)
		}
		got, err := client.CoreV1().ConfigMaps(ns).Get(context.Background(), "argocd-org-ca", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("expected ConfigMap to exist: %v", err)
		}
		if got.Data["ca.crt"] != "CA-A" {
			t.Errorf("data = %q, want CA-A", got.Data["ca.crt"])
		}
	})

	t.Run("updates when data changed (rotation)", func(t *testing.T) {
		existing := newCM("OLD-CA")
		client := fake.NewSimpleClientset(existing) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests
		if err := createOrUpdateConfigMap(context.Background(), client, newCM("NEW-CA")); err != nil {
			t.Fatalf("createOrUpdateConfigMap: %v", err)
		}
		got, _ := client.CoreV1().ConfigMaps(ns).Get(context.Background(), "argocd-org-ca", metav1.GetOptions{})
		if got.Data["ca.crt"] != "NEW-CA" {
			t.Errorf("rotated CA not applied: data = %q, want NEW-CA", got.Data["ca.crt"])
		}
	})

	t.Run("no-op when data unchanged", func(t *testing.T) {
		existing := newCM("CA-A")
		client := fake.NewSimpleClientset(existing) //nolint:staticcheck // SA1019: NewSimpleClientset is deprecated but still functional for tests
		if err := createOrUpdateConfigMap(context.Background(), client, newCM("CA-A")); err != nil {
			t.Fatalf("createOrUpdateConfigMap: %v", err)
		}
		got, _ := client.CoreV1().ConfigMaps(ns).Get(context.Background(), "argocd-org-ca", metav1.GetOptions{})
		if got.Data["ca.crt"] != "CA-A" {
			t.Errorf("data = %q, want CA-A", got.Data["ca.crt"])
		}
	})
}
