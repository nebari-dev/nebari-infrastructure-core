package argocd

import (
	"context"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDeriveProjectScopes(t *testing.T) {
	data := TemplateData{GitRepoURL: "https://git.example.com/org/repo", GitBranch: "main"}

	repos, namespaces, err := deriveProjectScopes(context.Background(), data)
	if err != nil {
		t.Fatalf("deriveProjectScopes() error = %v", err)
	}

	if !slices.IsSorted(repos) {
		t.Errorf("repos not sorted: %v", repos)
	}
	if !slices.IsSorted(namespaces) {
		t.Errorf("namespaces not sorted: %v", namespaces)
	}

	wantRepos := []string{
		"https://git.example.com/org/repo",
		"https://charts.bitnami.com/bitnami",
		"https://charts.jetstack.io",
		"https://codecentric.github.io/helm-charts",
		"https://github.com/nebari-dev/nebari-landing",
		"https://metallb.github.io/metallb",
		"https://open-telemetry.github.io/opentelemetry-helm-charts",
		"docker.io/envoyproxy",
	}
	for _, r := range wantRepos {
		if !slices.Contains(repos, r) {
			t.Errorf("deriveProjectScopes() repos missing %q; got %v", r, repos)
		}
	}
	if slices.Contains(repos, "*") {
		t.Errorf("deriveProjectScopes() repos must not contain '*'; got %v", repos)
	}

	wantNamespaces := []string{
		"argocd", "cert-manager", "envoy-gateway-system", "keycloak",
		"monitoring", "nebari-operator-system", "nebari-system",
	}
	for _, ns := range wantNamespaces {
		if !slices.Contains(namespaces, ns) {
			t.Errorf("deriveProjectScopes() namespaces missing %q; got %v", ns, namespaces)
		}
	}
	if slices.Contains(namespaces, "*") || slices.Contains(namespaces, "") {
		t.Errorf("deriveProjectScopes() namespaces must not contain '' or '*'; got %v", namespaces)
	}
}

func TestRenderProjects(t *testing.T) {
	data := TemplateData{GitRepoURL: "https://git.example.com/org/repo", GitBranch: "main"}
	objs, err := RenderProjects(context.Background(), data)
	if err != nil {
		t.Fatalf("RenderProjects() error = %v", err)
	}

	byName := map[string]map[string]interface{}{}
	for _, o := range objs {
		if o.GetKind() != "AppProject" {
			t.Errorf("unexpected kind %q", o.GetKind())
		}
		spec, _, _ := unstructuredNestedMap(o, "spec")
		byName[o.GetName()] = spec
	}

	// foundational: derived, no wildcards in sourceRepos/destinations
	f := byName["foundational"]
	if f == nil {
		t.Fatal("foundational project not rendered")
	}
	fRepos := toStringSlice(f["sourceRepos"])
	if slices.Contains(fRepos, "*") || len(fRepos) < 2 {
		t.Errorf("foundational sourceRepos wrong: %v", fRepos)
	}
	if !slices.Contains(fRepos, "https://git.example.com/org/repo") {
		t.Errorf("foundational sourceRepos missing GitRepoURL: %v", fRepos)
	}

	// nebari-apps: distinct, pack-source list, no wildcard repos
	n := byName["nebari-apps"]
	if n == nil {
		t.Fatal("nebari-apps project not rendered")
	}
	nRepos := toStringSlice(n["sourceRepos"])
	if !slices.Contains(nRepos, "https://nebari-dev.github.io/helm-repository") || slices.Contains(nRepos, "*") {
		t.Errorf("nebari-apps sourceRepos wrong: %v", nRepos)
	}

	// default: deny-all
	d := byName["default"]
	if d == nil {
		t.Fatal("default project not rendered")
	}
	if len(toStringSlice(d["sourceRepos"])) != 0 {
		t.Errorf("default sourceRepos must be empty, got %v", d["sourceRepos"])
	}

	// foundational destinations: no wildcard namespace
	for _, dest := range specList(f, "destinations") {
		m, _ := dest.(map[string]interface{})
		if ns, _ := m["namespace"].(string); ns == "*" {
			t.Errorf("foundational destinations must not contain namespace '*'")
		}
	}
	// nebari-apps: exactly one destination, namespace '*'
	nDests := specList(n, "destinations")
	if len(nDests) != 1 {
		t.Fatalf("nebari-apps must have exactly one destination, got %d", len(nDests))
	}
	if m, _ := nDests[0].(map[string]interface{}); m["namespace"] != "*" {
		t.Errorf("nebari-apps destination namespace must be '*', got %v", m["namespace"])
	}
	// default: deny-all (destinations + both whitelists empty)
	if len(specList(d, "destinations")) != 0 ||
		len(specList(d, "clusterResourceWhitelist")) != 0 ||
		len(specList(d, "namespaceResourceWhitelist")) != 0 {
		t.Errorf("default project must be deny-all (empty destinations and whitelists)")
	}
}

func unstructuredNestedMap(o *unstructured.Unstructured, key string) (map[string]interface{}, bool, error) {
	return unstructured.NestedMap(o.Object, key)
}

func toStringSlice(v interface{}) []string {
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if s, ok := i.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func specList(spec map[string]interface{}, key string) []interface{} {
	items, _ := spec[key].([]interface{})
	return items
}
