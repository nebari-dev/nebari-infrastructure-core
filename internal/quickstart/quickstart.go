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
	"errors"
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

// AppsDir is the directory of the GitOps repository the packs are written to.
const AppsDir = "apps"

const (
	// ProjectName is the project_name of the quickstart cluster. It is fixed so
	// re-running the quickstart reuses the same kind cluster and GitOps repository.
	ProjectName = "nebari-quickstart"
	// DefaultDomain resolves every subdomain to 127.0.0.1 through public DNS, so
	// no /etc/hosts edits are needed.
	DefaultDomain = "nebari.lvh.me"

	packsDir   = "templates/packs"
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

const (
	defaultHTTPPort  = 80
	defaultHTTPSPort = 443
)

// Validate checks the ports, so a bad flag fails before the config is written.
// The local provider runs the same checks, but only inside Deploy, after the
// file exists. Remove this once `nic validate` runs the cluster provider's
// checks (https://github.com/nebari-dev/nebari-infrastructure-core/issues/520).
func (o Options) Validate() error {
	for _, p := range []struct {
		flag string
		port int
	}{
		{"--http-port", o.HTTPPort},
		{"--https-port", o.HTTPSPort},
	} {
		if p.port < 0 || p.port > 65535 {
			return fmt.Errorf("%s must be between 1 and 65535, got %d", p.flag, p.port)
		}
	}

	httpPort, httpsPort := o.HTTPPort, o.HTTPSPort
	if httpPort == 0 {
		httpPort = defaultHTTPPort
	}
	if httpsPort == 0 {
		httpsPort = defaultHTTPSPort
	}
	if httpPort == httpsPort {
		return fmt.Errorf("--http-port and --https-port must differ, both are %d", httpPort)
	}
	return nil
}

// RenderConfig validates opts and renders the NIC config for the quickstart
// cluster.
func RenderConfig(opts Options) ([]byte, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
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
// repository at repoDir and commits them. A pack whose file already exists is
// left alone, so a rerun keeps the user's edits. A deleted pack file is
// written again, since nothing records that it was deleted.
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
		dest := filepath.Join(client.WorkDir(), AppsDir, name)
		// The pack files are the user's to edit, so a rerun only writes the
		// ones that don't exist yet.
		written, err := writeIfAbsent(dest, content)
		if err != nil {
			span.RecordError(err)
			return fmt.Errorf("write %s: %w", dest, err)
		}
		span.SetAttributes(attribute.Bool("pack."+name+".written", written))
	}

	if err := client.Commit(ctx, commitMsg); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// writeIfAbsent writes content to path only if the file doesn't exist, and
// reports whether it wrote it. O_EXCL makes the check and the create one step.
func writeIfAbsent(path string, content []byte) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, git.GitOpsFileMode) //nolint:gosec // path is inside the GitOps repo
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return false, err
	}
	return true, f.Close()
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
