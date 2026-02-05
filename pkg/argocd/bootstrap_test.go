package argocd

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestPluralizeKind(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		// Special cases
		{"GatewayClass", "gatewayclasses"},
		{"IngressClass", "ingressclasses"},
		{"StorageClass", "storageclasses"},
		{"PriorityClass", "priorityclasses"},

		// Words ending in 's', 'x', 'z', 'ch', 'sh' -> add 'es'
		{"Ingress", "ingresses"},
		{"NetworkPolicy", "networkpolicies"}, // y preceded by consonant
		{"HTTPProxy", "httpproxies"},         // y preceded by consonant

		// Words ending in 'y' preceded by vowel -> just add 's'
		{"Gateway", "gateways"},
		{"ServiceEntry", "serviceentries"}, // entry ends in y preceded by consonant -> ies

		// Default: just add 's'
		{"Deployment", "deployments"},
		{"Service", "services"},
		{"ConfigMap", "configmaps"},
		{"Secret", "secrets"},
		{"Namespace", "namespaces"},
		{"Pod", "pods"},
		{"Node", "nodes"},
		{"Application", "applications"},
		{"AppProject", "appprojects"},

		// Edge cases
		{"Batch", "batches"}, // ends in ch
		{"Mesh", "meshes"},   // ends in sh
		{"VirtualService", "virtualservices"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := pluralizeKind(tt.kind)
			if result != tt.expected {
				t.Errorf("pluralizeKind(%q) = %q, want %q", tt.kind, result, tt.expected)
			}
		})
	}
}

func TestArgoCDProjectManifest(t *testing.T) {
	// Test that the embedded manifest is valid
	if argoCDProjectManifest == "" {
		t.Fatal("argoCDProjectManifest should not be empty")
	}

	// Check it contains expected fields
	if !strings.Contains(argoCDProjectManifest, "kind: AppProject") {
		t.Error("argoCDProjectManifest should contain 'kind: AppProject'")
	}
	if !strings.Contains(argoCDProjectManifest, "apiVersion: argoproj.io/v1alpha1") {
		t.Error("argoCDProjectManifest should contain ArgoCD API version")
	}
	if !strings.Contains(argoCDProjectManifest, "name: foundational") {
		t.Error("argoCDProjectManifest should contain 'name: foundational'")
	}
	if !strings.Contains(argoCDProjectManifest, "namespace: argocd") {
		t.Error("argoCDProjectManifest should contain 'namespace: argocd'")
	}
}

func TestRootAppOfAppsTemplate(t *testing.T) {
	// Test that the template parses correctly
	tmpl, err := template.New("test").Parse(rootAppOfAppsTemplate)
	if err != nil {
		t.Fatalf("failed to parse rootAppOfAppsTemplate: %v", err)
	}

	tests := []struct {
		name string
		data struct {
			GitRepoURL string
			GitBranch  string
			GitPath    string
		}
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "basic repo without path",
			data: struct {
				GitRepoURL string
				GitBranch  string
				GitPath    string
			}{
				GitRepoURL: "https://github.com/example/repo.git",
				GitBranch:  "main",
				GitPath:    "",
			},
			wantContains: []string{
				"kind: Application",
				"name: nebari-root",
				"namespace: argocd",
				"repoURL: https://github.com/example/repo.git",
				"targetRevision: main",
				"path: apps",
				"project: foundational",
			},
			wantNotContains: []string{
				"path: /apps", // should not have leading slash
			},
		},
		{
			name: "repo with path",
			data: struct {
				GitRepoURL string
				GitBranch  string
				GitPath    string
			}{
				GitRepoURL: "https://github.com/example/repo.git",
				GitBranch:  "develop",
				GitPath:    "clusters/prod",
			},
			wantContains: []string{
				"repoURL: https://github.com/example/repo.git",
				"targetRevision: develop",
				"path: clusters/prod/apps",
			},
		},
		{
			name: "SSH repo URL",
			data: struct {
				GitRepoURL string
				GitBranch  string
				GitPath    string
			}{
				GitRepoURL: "git@github.com:example/repo.git",
				GitBranch:  "main",
				GitPath:    "",
			},
			wantContains: []string{
				"repoURL: git@github.com:example/repo.git",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tmpl.Execute(&buf, tt.data)
			if err != nil {
				t.Fatalf("failed to execute template: %v", err)
			}

			content := buf.String()

			for _, want := range tt.wantContains {
				if !strings.Contains(content, want) {
					t.Errorf("template output should contain %q\nGot:\n%s", want, content)
				}
			}

			for _, notWant := range tt.wantNotContains {
				if strings.Contains(content, notWant) {
					t.Errorf("template output should not contain %q\nGot:\n%s", notWant, content)
				}
			}
		})
	}
}

func TestRootAppOfAppsTemplate_SyncPolicy(t *testing.T) {
	tmpl, err := template.New("test").Parse(rootAppOfAppsTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	data := struct {
		GitRepoURL string
		GitBranch  string
		GitPath    string
	}{
		GitRepoURL: "https://github.com/example/repo.git",
		GitBranch:  "main",
		GitPath:    "",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	content := buf.String()

	// Check sync policy settings
	syncPolicyChecks := []string{
		"syncPolicy:",
		"automated:",
		"prune: true",
		"selfHeal: true",
		"CreateNamespace=true",
	}

	for _, check := range syncPolicyChecks {
		if !strings.Contains(content, check) {
			t.Errorf("template should contain %q for sync policy", check)
		}
	}
}

func TestRootAppOfAppsTemplate_Finalizers(t *testing.T) {
	tmpl, err := template.New("test").Parse(rootAppOfAppsTemplate)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	data := struct {
		GitRepoURL string
		GitBranch  string
		GitPath    string
	}{
		GitRepoURL: "https://github.com/example/repo.git",
		GitBranch:  "main",
		GitPath:    "",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	content := buf.String()

	// Check finalizer is set for cascade deletion
	if !strings.Contains(content, "resources-finalizer.argocd.argoproj.io") {
		t.Error("template should include ArgoCD resources finalizer")
	}
}
