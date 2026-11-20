// Package quickstart renders the config and software pack Applications that
// `nic quickstart` deploys: a local kind cluster with Nebi and the Data
// Science Pack installed on top.
//
// The pack Applications are written into the GitOps repository's apps/
// directory, where nebari-root syncs them. That directory belongs to NIC's
// foundational apps and is not where packs should live long term; it is used
// here because it needs no change to how NIC lays out the repository.
package quickstart

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/git"
)

const (
	// ProjectName is the project_name of the quickstart cluster. It is fixed so
	// re-running the quickstart reuses the same kind cluster and GitOps repository.
	ProjectName = "nebari-quickstart"
	// DefaultDomain resolves every subdomain to 127.0.0.1 through public DNS, so
	// no /etc/hosts edits are needed.
	DefaultDomain = "nebari.lvh.me"

	packsDir   = "templates/packs"
	appsDir    = "apps"
	commitMsg  = "Add quickstart software packs"
	rootApp    = "nebari-root"
	argocdNS   = "argocd"
	refreshKey = "argocd.argoproj.io/refresh"
)

//go:embed templates
var templates embed.FS

// Options are the values the quickstart config is rendered from.
type Options struct {
	Domain    string
	HTTPPort  int
	HTTPSPort int
}

// RenderConfig renders the NIC config for the quickstart cluster.
func RenderConfig(opts Options) ([]byte, error) {
	data := struct {
		Options
		ProjectName string
	}{Options: opts, ProjectName: ProjectName}
	return render("templates/config.yaml.tmpl", data)
}

// RenderPacks renders the pack Applications for domain, keyed by file name.
func RenderPacks(domain string) (map[string][]byte, error) {
	entries, err := fs.ReadDir(templates, packsDir)
	if err != nil {
		return nil, fmt.Errorf("read pack templates: %w", err)
	}
	packs := make(map[string][]byte, len(entries))
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		content, err := render(path.Join(packsDir, e.Name()), struct{ Domain string }{domain})
		if err != nil {
			return nil, err
		}
		packs[name] = content
	}
	return packs, nil
}

// PackNames returns the names of the packs the quickstart installs.
func PackNames() ([]string, error) {
	entries, err := fs.ReadDir(templates, packsDir)
	if err != nil {
		return nil, fmt.Errorf("read pack templates: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml.tmpl"))
	}
	return names, nil
}

// InstallPacks writes packs into the apps/ directory of the local GitOps
// repository at repoDir and commits them. Re-running with the same packs
// leaves the repository unchanged.
func InstallPacks(ctx context.Context, repoDir string, packs map[string][]byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "quickstart.InstallPacks")
	defer span.End()

	span.SetAttributes(
		attribute.String("repo_dir", repoDir),
		attribute.Int("pack_count", len(packs)),
	)

	client := git.NewClient("", "")
	if err := client.Init(ctx, repoDir); err != nil {
		span.RecordError(err)
		return fmt.Errorf("open GitOps repository: %w", err)
	}

	for name, content := range packs {
		dest := filepath.Join(client.WorkDir(), appsDir, name)
		if err := os.WriteFile(dest, content, git.GitOpsFileMode); err != nil {
			span.RecordError(err)
			return fmt.Errorf("write %s: %w", dest, err)
		}
	}

	if err := client.Commit(ctx, commitMsg); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// RefreshRoot asks ArgoCD to re-read the GitOps repository for nebari-root
// now, instead of at its next poll, so the packs start syncing right away.
func RefreshRoot(ctx context.Context, kubeconfig []byte) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "quickstart.RefreshRoot")
	defer span.End()

	client, err := argocd.NewDynamicClient(kubeconfig)
	if err != nil {
		span.RecordError(err)
		return err
	}
	patch := fmt.Appendf(nil, `{"metadata":{"annotations":{%q:"normal"}}}`, refreshKey)
	if _, err := client.Resource(argocd.ApplicationGVR).Namespace(argocdNS).
		Patch(ctx, rootApp, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		span.RecordError(err)
		return fmt.Errorf("refresh %s: %w", rootApp, err)
	}
	return nil
}

func render(name string, data any) ([]byte, error) {
	raw, err := templates.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(path.Base(name)).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}
