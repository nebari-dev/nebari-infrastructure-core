package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"sigs.k8s.io/yaml"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/repository"
)

// placeholderGitURL stands in for the user's GitOps repository. Application
// sources pointing here are NIC's own manifests, which are walked directly.
const placeholderGitURL = "https://example.com/imagepin.git"

type renderer struct {
	inv         *inventory
	work        string // temporary directory; removed by close()
	kubeVersion string
	targets     []*target
}

// target is one Helm source of one Argo CD Application: a chart fetched at its
// pinned version, plus the NIC values Argo CD would pass to it. Keeping it
// lets later steps re-render the same chart with changed values.
type target struct {
	label     string // where it comes from, for reports
	chartPath string // local copy of the chart
	release   string
	namespace string
	values    map[string]any // NIC's merged values, never modified
	files     []valueFile    // the values files, in Argo CD's order
}

// valueFile is one NIC values file as rendered, with the source template it
// came from, so a change can be reported against the file to edit.
type valueFile struct {
	source string // e.g. pkg/argocd/templates/values/keycloak/base.yaml
	values map[string]any
}

// run renders everything and fills inv. Call close() on the result.
func run(ctx context.Context, inv *inventory, kubeVersion string) (*renderer, error) {
	work, err := os.MkdirTemp("", "imagepin-")
	if err != nil {
		return nil, err
	}
	r := &renderer{inv: inv, work: work, kubeVersion: kubeVersion}

	gitops := filepath.Join(work, "gitops")
	if err := renderNIC(ctx, gitops); err != nil {
		r.close()
		return nil, fmt.Errorf("render NIC templates: %w", err)
	}

	// NIC's own manifests (Jobs, CNPG Clusters, ...), as NIC renders them.
	err = filepath.WalkDir(filepath.Join(gitops, "manifests"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml")) {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // Reads our own temporary render.
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(gitops, path)
		return inv.manifest(data, origin{source: manifestPrefix + filepath.ToSlash(rel), nicManifest: true})
	})
	if err != nil {
		r.close()
		return nil, fmt.Errorf("walk NIC manifests: %w", err)
	}

	if err := r.applications(ctx, gitops); err != nil {
		r.close()
		return nil, err
	}

	inv.notes = append(inv.notes,
		"Charts installed from Go code rather than Argo CD (Argo CD itself, Longhorn, AWS load balancer controller, cluster autoscaler, GPU operator) and kind/k3s node images.",
		"Images a controller creates at run time from its own built-in defaults, when NIC's values don't set them.",
	)
	return r, nil
}

const manifestPrefix = "pkg/argocd/templates/"

func (r *renderer) close() { _ = os.RemoveAll(r.work) } //nolint:gosec // r.work is our own os.MkdirTemp directory.

// renderNIC writes NIC's GitOps output exactly as a deploy would, with every
// optional component this listing should see turned on.
func renderNIC(ctx context.Context, dir string) error {
	cfg := &config.NebariConfig{
		Domain: "imagepin.example.com",
		Backups: &config.BackupsConfig{Longhorn: &config.LonghornBackupConfig{
			S3: &config.S3BackupTarget{Bucket: "imagepin", Region: "us-east-1"},
			Schedules: config.BackupSchedules{
				Snapshot: config.ScheduleConfig{Cron: "0 * * * *", Retain: 2, Concurrency: 1},
				Backup:   config.ScheduleConfig{Cron: "0 0 * * *", Retain: 2, Concurrency: 1},
			},
		}},
	}
	src := repository.RemoteSource{URL: placeholderGitURL, Branch: "main"}
	settings := cluster.InfraSettings{StorageClass: "longhorn", LonghornEnabled: true}
	return argocd.WriteAllToGit(ctx, dir, cfg, src, settings, "imagepin-placeholder-ca")
}

type application struct {
	Spec struct {
		Source      appSource   `json:"source"`
		Sources     []appSource `json:"sources"`
		Destination struct {
			Namespace string `json:"namespace"`
		} `json:"destination"`
	} `json:"spec"`
}

type appSource struct {
	RepoURL        string `json:"repoURL"`
	Chart          string `json:"chart"`
	Path           string `json:"path"`
	TargetRevision string `json:"targetRevision"`
	Helm           *struct {
		ReleaseName    string          `json:"releaseName"`
		ValueFiles     []string        `json:"valueFiles"`
		Values         json.RawMessage `json:"values"`
		ValuesObject   json.RawMessage `json:"valuesObject"`
		Parameters     json.RawMessage `json:"parameters"`
		FileParameters json.RawMessage `json:"fileParameters"`
	} `json:"helm"`
}

// applications fetches and renders every Helm source of every rendered Argo
// CD Application. Source types it doesn't understand are errors, not skips,
// so a new kind of source can't silently drop out of the listing.
func (r *renderer) applications(ctx context.Context, gitops string) error {
	apps, err := filepath.Glob(filepath.Join(gitops, "apps", "*.yaml"))
	if err != nil {
		return err
	}
	for _, file := range apps {
		data, err := os.ReadFile(file) //nolint:gosec // Reads our own temporary render.
		if err != nil {
			return err
		}
		var app application
		if err := yaml.Unmarshal(data, &app); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		sources := app.Spec.Sources
		if app.Spec.Source.RepoURL != "" {
			sources = append(sources, app.Spec.Source)
		}
		appLabel := manifestPrefix + "apps/" + filepath.Base(file)

		for _, src := range sources {
			if src.Helm == nil {
				if src.RepoURL != placeholderGitURL {
					return fmt.Errorf("%s: unhandled non-Helm source %s", appLabel, src.RepoURL)
				}
				continue // NIC's own manifests, walked in run().
			}
			if len(src.Helm.Values)+len(src.Helm.ValuesObject)+len(src.Helm.Parameters)+len(src.Helm.FileParameters) > 0 {
				return fmt.Errorf("%s: inline Helm values/parameters are not supported; NIC uses valueFiles", appLabel)
			}
			values, files, err := loadValues(gitops, src.Helm.ValueFiles)
			if err != nil {
				return fmt.Errorf("%s: %w", appLabel, err)
			}

			var chartPath, chartLabel string
			switch {
			case src.Chart != "":
				chartPath, err = r.fetchRepoChart(src.RepoURL, src.Chart, src.TargetRevision)
				chartLabel = src.Chart + " " + src.TargetRevision
			case src.Path != "":
				chartPath, err = r.fetchGitChart(ctx, src.RepoURL, src.TargetRevision, src.Path)
				chartLabel = src.Path + "@" + src.TargetRevision
			default:
				err = errors.New("helm source has neither chart nor path")
			}
			if err != nil {
				return fmt.Errorf("%s: %w", appLabel, err)
			}

			t := &target{
				label: appLabel + " -> " + chartLabel, chartPath: chartPath,
				release: src.Helm.ReleaseName, namespace: app.Spec.Destination.Namespace,
				values: values, files: files,
			}
			r.targets = append(r.targets, t)

			docs, err := r.renderDocs(t, t.values)
			if err != nil {
				return fmt.Errorf("%s: %w", t.label, err)
			}
			nicValues := stringLeaves(t.values, map[string]bool{})
			for _, name := range sortedKeys(docs) {
				if err := r.inv.manifest([]byte(docs[name]), origin{source: t.label + " -> " + name, nicValues: nicValues}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// loadValues merges valueFiles in order, later files winning, the way Argo CD
// does. Glob entries (overlays/*.yaml) may match nothing.
func loadValues(gitops string, files []string) (map[string]any, []valueFile, error) {
	values := map[string]any{}
	var loaded []valueFile
	for _, vf := range files {
		rel, ok := strings.CutPrefix(vf, "$values/")
		if !ok {
			return nil, nil, fmt.Errorf("unhandled values source %q", vf)
		}
		if !filepath.IsLocal(rel) {
			return nil, nil, fmt.Errorf("values path %q escapes the render directory", rel)
		}
		matches, err := filepath.Glob(filepath.Join(gitops, rel))
		if err != nil {
			return nil, nil, err
		}
		if len(matches) == 0 && !strings.Contains(rel, "*") {
			return nil, nil, fmt.Errorf("missing values file %s", rel)
		}
		for _, m := range matches {
			data, err := os.ReadFile(m) //nolint:gosec // Reads our own temporary render.
			if err != nil {
				return nil, nil, err
			}
			var overlay map[string]any
			if err := yaml.Unmarshal(data, &overlay); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", rel, err)
			}
			mrel, _ := filepath.Rel(gitops, m)
			loaded = append(loaded, valueFile{source: manifestPrefix + filepath.ToSlash(mrel), values: overlay})
			values = chartutil.CoalesceTables(deepCopy(overlay), values)
		}
	}
	return values, loaded, nil
}

// fetchRepoChart downloads a chart from an HTTPS Helm repository or an OCI
// registry, at the pinned version, and returns its local path.
func (r *renderer) fetchRepoChart(repo, name, version string) (string, error) {
	client, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(repo, "https://") {
		// OCI registry. Helm OCI tags use '_' in place of '+'.
		ref := strings.TrimPrefix(repo, "oci://") + "/" + name + ":" + strings.ReplaceAll(version, "+", "_")
		pulled, err := client.Pull(ref)
		if err != nil {
			return "", fmt.Errorf("pull %s: %w", ref, err)
		}
		path := filepath.Join(r.work, "oci-"+name+".tgz")
		if err := os.WriteFile(path, pulled.Chart.Data, 0o600); err != nil {
			return "", err
		}
		r.inv.charts = append(r.inv.charts, fmt.Sprintf("%s (chart digest %s)", ref, pulled.Manifest.Digest))
		return path, nil
	}

	settings := cli.New()
	settings.RepositoryCache = filepath.Join(r.work, "repo-cache")
	settings.RepositoryConfig = filepath.Join(r.work, "repositories.yaml")
	installer := action.NewInstall(&action.Configuration{RegistryClient: client})
	installer.ChartPathOptions = action.ChartPathOptions{RepoURL: repo, Version: version}
	installer.SetRegistryClient(client)
	path, err := installer.LocateChart(name, settings)
	if err != nil {
		return "", fmt.Errorf("fetch %s %s from %s: %w", name, version, repo, err)
	}
	r.inv.charts = append(r.inv.charts, fmt.Sprintf("%s %s from %s", name, version, repo))
	return path, nil
}

// fetchGitChart clones a chart kept in a Git repository at the pinned
// revision. No hooks or repository scripts run; rendering uses Helm's Go SDK.
func (r *renderer) fetchGitChart(ctx context.Context, repo, revision, path string) (string, error) {
	if !strings.HasPrefix(repo, "https://github.com/") || strings.HasPrefix(revision, "-") {
		return "", fmt.Errorf("unsupported Git chart source %s@%s", repo, revision)
	}
	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("unsupported chart path %q", path)
	}
	dir := filepath.Join(r.work, "git-"+filepath.Base(path))
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--depth=1", "--branch", revision, "--", repo, dir) //nolint:gosec // HTTPS GitHub URL and revision validated above; no shell.
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("clone %s@%s: %v: %s", repo, revision, err, out)
	}
	r.inv.charts = append(r.inv.charts, fmt.Sprintf("%s from %s@%s", path, repo, revision))
	return filepath.Join(dir, path), nil
}

// renderDocs renders a target's chart offline, the way `helm template` would,
// with the given values. It returns the rendered YAML files by name. Helm
// validates the values against the chart's values.schema.json, if it has one.
func (r *renderer) renderDocs(t *target, values map[string]any) (map[string]string, error) {
	c, err := loader.Load(t.chartPath)
	if err != nil {
		return nil, err
	}
	if err := action.CheckDependencies(c, c.Metadata.Dependencies); err != nil {
		// Argo CD builds dependencies for Git-hosted charts. Only build from
		// a lockfile, so transitive versions are never silently chosen.
		if c.Lock == nil {
			return nil, fmt.Errorf("missing dependencies and no Chart.lock: %w", err)
		}
		if c, err = r.buildDependencies(t.chartPath); err != nil {
			return nil, err
		}
	}

	vals := deepCopy(values) // Helm may modify the map it is given.
	if err := chartutil.ProcessDependencies(c, vals); err != nil {
		return nil, err
	}
	caps := chartutil.DefaultCapabilities.Copy()
	kv, err := chartutil.ParseKubeVersion(r.kubeVersion)
	if err != nil {
		return nil, err
	}
	caps.KubeVersion = *kv
	renderVals, err := chartutil.ToRenderValues(c, vals, chartutil.ReleaseOptions{
		Name: t.release, Namespace: t.namespace, Revision: 1, IsInstall: true,
	}, caps)
	if err != nil {
		return nil, err
	}
	rendered, err := engine.Render(c, renderVals)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	docs := map[string]string{}
	for name, doc := range rendered {
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			docs[name] = doc
		}
	}
	return docs, nil
}

func (r *renderer) buildDependencies(chartPath string) (*chart.Chart, error) {
	client, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		return nil, err
	}
	m := downloader.Manager{
		Out:              io.Discard,
		ChartPath:        chartPath,
		Getters:          getter.All(cli.New()),
		RegistryClient:   client,
		RepositoryConfig: filepath.Join(r.work, "repositories.yaml"),
		RepositoryCache:  filepath.Join(r.work, "repo-cache"),
	}
	if err := m.Build(); err != nil {
		return nil, fmt.Errorf("build chart dependencies: %w", err)
	}
	return loader.Load(chartPath)
}

// deepCopy copies a values tree. Values come from YAML via JSON, so a JSON
// round trip preserves them exactly.
func deepCopy(m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err) // unreachable for values decoded from YAML
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
