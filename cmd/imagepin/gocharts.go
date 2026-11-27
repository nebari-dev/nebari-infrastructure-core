package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sigs.k8s.io/kind/pkg/apis/config/defaults"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster/aws"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/storage/longhorn"
)

// Images NIC deploys from Go code rather than through Argo CD: charts it
// installs with Helm's Go SDK, a DaemonSet it applies directly, and kind's
// default node image.
//
// Exported names (versions, Argo CD's values) are used directly. Unexported
// constants (chart repositories and names, the iSCSI manifest) are read from
// the Go source with go/parser; they are plain literals, so this is exact. An
// unexported function can't be run from here, so a chart whose values come
// from one is rendered with its own defaults; each such case is recorded as a
// note in the inventory. If any constant is renamed or stops being a literal,
// this fails rather than silently dropping the chart.

const modulePath = "github.com/nebari-dev/nebari-infrastructure-core"

// awsPkg is where NIC defines the charts it installs on EKS.
const awsPkg = "pkg/providers/cluster/aws"

// goChart is one chart NIC installs from Go code.
type goChart struct {
	defined              string // where NIC defines it
	repo, chart, version string
	release, namespace   string
	values               map[string]any
	note                 string // a limit of this render, if any
}

func (r *renderer) goInstalled() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	c := constReader{root: root}
	charts := goCharts(&c)
	iscsi := c.get("pkg/storage/longhorn", "iscsiDaemonSetYAML")
	if c.err != nil {
		return c.err
	}

	for _, gc := range charts {
		path, err := r.fetchRepoChart(gc.repo, gc.chart, gc.version)
		if err != nil {
			return fmt.Errorf("%s: %w", gc.defined, err)
		}
		t := &target{
			label: gc.defined + " -> " + gc.chart + " " + gc.version, chartPath: path,
			release: gc.release, namespace: gc.namespace, values: gc.values,
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
		if gc.note != "" {
			r.inv.notes = append(r.inv.notes, gc.note)
		}
	}

	// NIC applies this DaemonSet directly; its images are NIC's choice.
	if err := r.inv.manifest([]byte(iscsi), origin{source: "pkg/storage/longhorn/iscsi.go (iscsiDaemonSetYAML)", nicManifest: true}); err != nil {
		return err
	}

	// kind's default node image, used when a local deployment sets no node_image.
	r.inv.add(defaults.Image, origin{source: "sigs.k8s.io/kind defaults.Image (default node image when node_image is unset)", nicManifest: true})
	return nil
}

// goCharts lists the charts NIC installs with Helm's Go SDK. Each entry's
// defined is the file that calls action.NewInstall for it, and every such
// call under pkg/ needs an entry here: TestGoCharts_CoverEveryInstallSite
// fails otherwise, so a new Go-installed chart can't drop out of the
// inventory and the scan unnoticed. Check c.err after calling.
func goCharts(c *constReader) []goChart {
	argo := argocd.DefaultConfig()
	awsCfg := &aws.Config{} // NIC's defaults: no user overrides
	return []goChart{
		{
			defined: "pkg/argocd/helm.go",
			repo:    c.get("pkg/argocd", "repoURL"), chart: chartBase(c.get("pkg/argocd", "chartName")),
			version: argo.Version, release: argo.ReleaseName, namespace: argo.Namespace,
			values: argo.Values, // exported: NIC's exact values
		},
		{
			defined: "pkg/storage/longhorn/install.go",
			repo:    c.get("pkg/storage/longhorn", "chartRepoURL"), chart: chartBase(c.get("pkg/storage/longhorn", "chartName")),
			version: longhorn.ChartVersion, release: longhorn.ReleaseName, namespace: longhorn.Namespace,
			note: "Longhorn: rendered with chart defaults; NIC's values (buildHelmValues) set scheduling and settings, not images.",
		},
		{
			defined: "pkg/providers/cluster/aws/aws_load_balancer_controller.go",
			repo:    c.get(awsPkg, "lbcRepoURL"), chart: chartBase(c.get(awsPkg, "lbcChartName")),
			version: awsCfg.LoadBalancerControllerChartVersion(),
			release: c.get(awsPkg, "lbcReleaseName"), namespace: c.get(awsPkg, "lbcNamespace"),
			values: map[string]any{"clusterName": "imagepin"}, // required by the chart; doesn't affect images
			note:   "AWS load balancer controller: rendered with chart defaults plus a placeholder clusterName; NIC's values set cluster settings, not images.",
		},
		{
			defined: "pkg/providers/cluster/aws/cluster_autoscaler.go",
			repo:    c.get(awsPkg, "caRepoURL"), chart: chartBase(c.get(awsPkg, "caChartName")),
			version: awsCfg.ClusterAutoscalerChartVersion(),
			release: c.get(awsPkg, "caReleaseName"), namespace: c.get(awsPkg, "caNamespace"),
			// The chart requires a discovery target; placeholders don't affect images.
			values: map[string]any{"cloudProvider": "aws", "awsRegion": "us-east-1", "autoDiscovery": map[string]any{"clusterName": "imagepin"}},
			note:   "Cluster autoscaler: the scanned image is the chart's default; a deployment runs the tag for its own Kubernetes version (v<version>.0).",
		},
		{
			defined: "pkg/providers/cluster/aws/gpu_operator.go",
			repo:    c.get(awsPkg, "gpuOperatorRepoURL"), chart: chartBase(c.get(awsPkg, "gpuOperatorChartName")),
			version: c.get(awsPkg, "gpuOperatorChartVersion"),
			release: c.get(awsPkg, "gpuOperatorReleaseName"), namespace: c.get(awsPkg, "gpuOperatorNamespace"),
			// Mirrors the image-affecting part of NIC's gpuOperatorHelmValues
			// (unexported, so it can't be called): the driver and container
			// toolkit are disabled. Keep in step with that function.
			values: map[string]any{
				"driver":  map[string]any{"enabled": false},
				"toolkit": map[string]any{"enabled": false},
			},
			note: "GPU operator (installed only with GPU node groups): driver and toolkit disabled as in NIC's gpuOperatorHelmValues, mirrored here; other components the ClusterPolicy enables are listed even where they only run on matching nodes (MIG, vGPU, sandbox workloads).",
		},
	}
}

// chartBase turns the Helm CLI form "repo/chart" into the chart name.
func chartBase(name string) string { return name[strings.LastIndex(name, "/")+1:] }

// constReader reads string constants from a package's Go source. The first
// error sticks, so a run of get calls can be checked once.
type constReader struct {
	root string
	err  error
}

func (c *constReader) get(pkgDir, name string) string {
	if c.err != nil {
		return ""
	}
	v, err := constString(filepath.Join(c.root, pkgDir), name)
	if err != nil {
		c.err = fmt.Errorf("read %s.%s: %w", pkgDir, name, err)
	}
	return v
}

// constString returns the value of the string constant name declared in the
// non-test Go files of dir. It must be a plain string literal.
func constString(dir, name string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			return "", err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if id.Name != name {
						continue
					}
					if i >= len(vs.Values) {
						return "", fmt.Errorf("constant has no value of its own")
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return "", fmt.Errorf("not a plain string literal")
					}
					return strconv.Unquote(lit.Value)
				}
			}
		}
	}
	return "", fmt.Errorf("constant not found")
}

// declaresModule reports whether a go.mod declares module path. It compares
// whole lines with surrounding space trimmed, so a go.mod with Windows line
// endings still matches.
func declaresModule(gomod []byte, path string) bool {
	for _, line := range strings.Split(string(gomod), "\n") {
		if strings.TrimSpace(line) == "module "+path {
			return true
		}
	}
	return false
}

// moduleRoot finds the NIC repository root from the working directory, so the
// tool reads the source it was run from.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // Walking up to this repository's own go.mod.
		if err == nil && declaresModule(data, modulePath) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("run imagepin from inside the %s repository", modulePath)
		}
		dir = parent
	}
}
