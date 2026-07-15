package argocd

import (
	"context"
	"slices"
	"testing"
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
