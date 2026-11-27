package argocd

import (
	"bytes"
	"context"
	"path"
	"testing"
	"text/template"

	"github.com/goccy/go-yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
)

// TestApplications_RetryIndefinitely verifies every foundational Application
// retries failed syncs without a limit. Argo CD does not retry a failed sync of
// the same commit once the limit is reached, so an app that loses a race with
// another app's webhook or CRD would stay broken until someone syncs it by
// hand. Each retry must also use the latest revision: an operation stays open
// between retries and auto-sync skips an app with an open operation, so without
// refresh a retry keeps applying the failed commit and never picks up a fix.
// See https://github.com/nebari-dev/nebari-infrastructure-core/issues/450
func TestApplications_RetryIndefinitely(t *testing.T) {
	rendered := map[string][]byte{}

	// Render every app NIC can deploy, as listed by Applications(), directly,
	// so templates that WriteAllToGit skips for this config are still checked.
	apps, err := Applications()
	if err != nil {
		t.Fatalf("Applications() error: %v", err)
	}
	if len(apps) == 0 {
		t.Fatal("Applications() returned no apps, so only the root app would be checked")
	}
	data := NewTemplateData(context.Background(), &config.NebariConfig{Domain: "test.example.com"}, nil, cluster.InfraSettings{StorageClass: "gp2"})
	for _, app := range apps {
		name := path.Join("templates/apps", app+".yaml")
		content, err := templates.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		out, err := processTemplate(name, content, data)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		rendered[name] = out
	}

	// The root App-of-Apps is applied from bootstrap.go, not from the templates.
	tmpl, err := template.New("root-app").Parse(rootAppOfAppsTemplate)
	if err != nil {
		t.Fatalf("parse root template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ GitRepoURL, GitBranch, GitPath string }{"https://github.com/example/repo.git", "main", ""}); err != nil {
		t.Fatalf("render root template: %v", err)
	}
	rendered["bootstrap.go rootAppOfAppsTemplate"] = buf.Bytes()

	for name, content := range rendered {
		t.Run(name, func(t *testing.T) {
			var app struct {
				Kind string `yaml:"kind"`
				Spec struct {
					SyncPolicy struct {
						Retry *struct {
							Limit   *int64 `yaml:"limit"`
							Refresh bool   `yaml:"refresh"`
						} `yaml:"retry"`
					} `yaml:"syncPolicy"`
				} `yaml:"spec"`
			}
			if err := yaml.Unmarshal(content, &app); err != nil {
				t.Fatalf("rendered manifest is not valid YAML: %v\n%s", err, content)
			}
			if app.Kind != "Application" {
				t.Fatalf("kind = %q, want Application", app.Kind)
			}
			retry := app.Spec.SyncPolicy.Retry
			if retry == nil || retry.Limit == nil {
				t.Fatalf("spec.syncPolicy.retry.limit is not set")
			}
			if *retry.Limit >= 0 {
				t.Errorf("spec.syncPolicy.retry.limit = %d, want a negative value (retry indefinitely)", *retry.Limit)
			}
			if !retry.Refresh {
				t.Errorf("spec.syncPolicy.retry.refresh is not true, so retries keep applying the failed revision")
			}
		})
	}
}
