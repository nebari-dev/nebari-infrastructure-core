// Package imageinventory supports the opt-in deployment inventory tests. It
// renders charts offline (no Kubernetes connection) with NIC's actual values.
package imageinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/distribution/reference"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// Inventory records both scan targets and their deployment provenance.
type Inventory struct {
	Images          map[string][]string `json:"images"`
	Charts          map[string]string   `json:"charts"`
	Notes           []string            `json:"notes"`
	postgresDefault string
}

// New skips network-dependent inventory generation during ordinary unit tests.
func New(t *testing.T) *Inventory {
	t.Helper()
	if os.Getenv("NIC_IMAGE_INVENTORY_DIR") == "" {
		t.Skip("set NIC_IMAGE_INVENTORY_DIR to render the deployed image inventory")
	}
	return &Inventory{Images: map[string][]string{}, Charts: map[string]string{}}
}

// Add requires explicit tags or digests; an unresolved template must never scan
// an unrelated :latest image and appear to succeed.
func (i *Inventory) Add(image, source string) error {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil || reference.IsNameOnly(named) || strings.Contains(image, "{{") {
		return fmt.Errorf("unresolved image %q in %s", image, source)
	}
	if !slices.Contains(i.Images[image], source) {
		i.Images[image] = append(i.Images[image], source)
		slices.Sort(i.Images[image])
	}
	return nil
}

// Save writes one independent shard per package, avoiding concurrent writers.
func (i *Inventory) Save(t *testing.T, name string) {
	t.Helper()
	if len(i.Images) == 0 {
		t.Fatal("empty inventory: " + name)
	}
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // Output directory is explicitly chosen by the local/CI caller, never deployment input.
	if err := os.MkdirAll(os.Getenv("NIC_IMAGE_INVENTORY_DIR"), 0o750); err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // Same caller-owned output directory; shard names are literal test identifiers.
	if err := os.WriteFile(filepath.Join(os.Getenv("NIC_IMAGE_INVENTORY_DIR"), name+".json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Read reads a checked-in or freshly rendered file.
func Read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name) //nolint:gosec // Test helper reads repository files or its own temporary renders.
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Fetch retrieves pinned upstream manifests and source, with bounded I/O.
func Fetch(t *testing.T, url string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Checkout is used only for a chart stored in a pinned Git source. No hooks or
// repository scripts are executed. Rendering uses Helm's Go SDK below.
func Checkout(t *testing.T, repo, revision string) string {
	t.Helper()
	if !strings.HasPrefix(repo, "https://github.com/") || strings.HasPrefix(revision, "-") {
		t.Fatal("unsupported Git chart source")
	}
	dir := filepath.Join(t.TempDir(), "source")
	cmd := exec.CommandContext(t.Context(), "git", "clone", "--quiet", "--depth=1", "--branch", revision, "--", repo, dir) //nolint:gosec // Reviewed template source; HTTPS GitHub URL and revision are validated above, no shell.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone chart: %v: %s", err, out)
	}
	return dir
}

// GoString reads an upstream literal default. Fail if upstream changes the
// expression: guessing a controller-created image would conceal lost coverage.
func GoString(t *testing.T, data []byte, name string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "upstream.go", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	ast.Inspect(f, func(node ast.Node) bool {
		v, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for index, ident := range v.Names {
			if ident.Name == name && index < len(v.Values) {
				lit, ok := v.Values[index].(*ast.BasicLit)
				if !ok {
					t.Fatalf("review non-literal upstream default %s", name)
				}
				value, err = strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		return true
	})
	if value == "" {
		t.Fatalf("upstream default %s is missing", name)
	}
	return value
}

// ModuleFile reads the source of the exact Go module version NIC builds with.
func ModuleFile(t *testing.T, module, rel string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-m", "-json", module) //nolint:gosec // Module is a literal in the inventory test; no shell or deployment input.
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var info struct{ Dir string }
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	if info.Dir == "" || !filepath.IsLocal(rel) {
		t.Fatal("invalid module source path")
	}
	return Read(t, filepath.Join(info.Dir, rel))
}

// Chart renders a repository/OCI chart using the same chart version and values
// the install path supplies. Render includes install hooks and init containers.
func (i *Inventory) Chart(t *testing.T, source, repo, name, version, release, namespace string, values map[string]any) *chart.Chart {
	t.Helper()
	settings := cli.New()
	settings.RepositoryCache = t.TempDir()
	settings.RepositoryConfig = filepath.Join(t.TempDir(), "repositories.yaml")
	opts := action.ChartPathOptions{Version: version}
	if strings.HasPrefix(repo, "https://") {
		opts.RepoURL = repo
	}
	client, err := registry.NewClient(registry.ClientOptWriter(io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	if opts.RepoURL == "" {
		// Record the manifest digest of the exact OCI chart bytes rendered.
		// Tags can be republished (notably the Nebari chart catalog).
		ref := strings.TrimPrefix(repo, "oci://") + "/" + name + ":" + strings.ReplaceAll(version, "+", "_")
		pulled, err := client.Pull(ref)
		if err != nil {
			t.Fatal(err)
		}
		if i.Charts == nil {
			i.Charts = map[string]string{}
		}
		i.Charts[ref] = pulled.Manifest.Digest
		chartPath := filepath.Join(t.TempDir(), "chart.tgz")
		if err := os.WriteFile(chartPath, pulled.Chart.Data, 0o600); err != nil {
			t.Fatal(err)
		}
		return i.LocalChart(t, source, chartPath, release, namespace, values)
	}
	installer := action.NewInstall(&action.Configuration{RegistryClient: client})
	installer.ChartPathOptions = opts
	installer.SetRegistryClient(client)
	chartPath, err := installer.LocateChart(name, settings)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	return i.LocalChart(t, source, chartPath, release, namespace, values)
}

// LocalChart also handles charts from Argo CD Git sources.
func (i *Inventory) LocalChart(t *testing.T, source, chartPath, release, namespace string, values map[string]any) *chart.Chart {
	t.Helper()
	c, err := loader.Load(chartPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := action.CheckDependencies(c, c.Metadata.Dependencies); err != nil {
		// Argo CD builds dependencies for Git-hosted charts too. A lockfile
		// fixes transitive versions; never silently choose new versions.
		if c.Lock == nil {
			t.Fatalf("%s: missing dependencies and Chart.lock: %v", source, err)
		}
		settings := cli.New()
		client, regErr := registry.NewClient(registry.ClientOptWriter(io.Discard))
		if regErr != nil {
			t.Fatal(regErr)
		}
		manager := downloader.Manager{Out: io.Discard, ChartPath: chartPath, Getters: getter.All(settings), RegistryClient: client, RepositoryConfig: filepath.Join(t.TempDir(), "repositories.yaml"), RepositoryCache: t.TempDir()}
		if err := manager.Build(); err != nil {
			t.Fatal(err)
		}
		c, err = loader.Load(chartPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := chartutil.ProcessDependencies(c, values); err != nil {
		t.Fatal(err)
	}
	caps := chartutil.DefaultCapabilities.Copy()
	caps.KubeVersion = chartutil.KubeVersion{Version: "v1.34.0", Major: "1", Minor: "34"}
	vals, err := chartutil.ToRenderValues(c, values, chartutil.ReleaseOptions{Name: release, Namespace: namespace, Revision: 1, IsInstall: true}, caps)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := engine.Render(c, vals)
	if err != nil {
		t.Fatalf("render %s: %v", source, err)
	}
	for name, manifest := range rendered {
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			if err := i.Manifest([]byte(manifest), source+" -> "+name); err != nil {
				t.Fatal(err)
			}
		}
	}
	return c
}

// Manifest walks actual rendered resources, including CR imageName fields,
// controller image flags/env vars and NVIDIA ClusterPolicy image triples.
func (i *Inventory) Manifest(data []byte, source string) error {
	decoder := kyaml.NewYAMLOrJSONDecoder(strings.NewReader(string(data)), 4096)
	for {
		var doc map[string]any
		if err := decoder.Decode(&doc); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		if doc["kind"] == "CustomResourceDefinition" {
			continue
		}
		if doc["kind"] == "Kustomization" {
			return fmt.Errorf("%s: Kustomize image sources need inventory support before deployment", source)
		}
		apiVersion, _ := doc["apiVersion"].(string)
		if doc["kind"] == "Cluster" && strings.HasPrefix(apiVersion, "postgresql.cnpg.io/") {
			spec, _ := doc["spec"].(map[string]any)
			if spec["imageCatalogRef"] != nil {
				return fmt.Errorf("%s: resolve the CNPG image catalog before adding this source", source)
			}
			if image, _ := spec["imageName"].(string); image == "" {
				if i.postgresDefault == "" {
					return fmt.Errorf("%s: unresolved CNPG default Postgres image", source)
				}
				if err := i.Add(i.postgresDefault, source+" -> CloudNativePG operator default"); err != nil {
					return err
				}
			}
		}
		metadata, _ := doc["metadata"].(map[string]any)
		annotations, _ := metadata["annotations"].(map[string]any)
		hook, _ := annotations["helm.sh/hook"].(string)
		if hook == "test" || hook == "test-success" || hook == "test-failure" {
			continue
		}
		if err := i.walk(doc, source); err != nil {
			return err
		}
	}
}

var imageInText = regexp.MustCompile(`(?:[a-z0-9.-]+(?::[0-9]+)?/)+[a-z0-9._/-]+(?::[a-zA-Z0-9._-]+(?:@sha256:[a-f0-9]{64})?|@sha256:[a-f0-9]{64})`)

func (i *Inventory) walk(value any, source string) error {
	switch v := value.(type) {
	case map[string]any:
		if enabled, ok := v["enabled"].(bool); ok && !enabled {
			return nil
		}
		if v["name"] == "POSTGRES_IMAGE_NAME" {
			value, ok := v["value"].(string)
			if !ok || value == "" {
				return fmt.Errorf("%s: resolve POSTGRES_IMAGE_NAME before scanning its Cluster", source)
			}
			i.postgresDefault = value
		}
		if value, ok := v["POSTGRES_IMAGE_NAME"].(string); ok {
			i.postgresDefault = value
		}
		// The GPU operator materializes these into pods after Helm has run.
		repo, _ := v["repository"].(string)
		version, _ := v["version"].(string)
		img, _ := v["image"].(string)
		if repo != "" && version != "" && img != "" {
			if err := i.Add(repo+"/"+img+":"+version, source); err != nil {
				return err
			}
		}
		for key, child := range v {
			if (key == "image" || key == "imageName") && repo == "" {
				if s, ok := child.(string); ok && s != "" {
					if err := i.Add(s, source); err != nil {
						return err
					}
					continue
				}
			}
			if err := i.walk(child, source); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := i.walk(child, source); err != nil {
				return err
			}
		}
	case string:
		// Longhorn supplies CSI/engine images through flags and environment
		// variables; Envoy embeds controller configuration inside a ConfigMap.
		for _, match := range imageInText.FindAllString(v, -1) {
			if err := i.Add(match, source); err != nil {
				return err
			}
		}
	}
	return nil
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

// Applications discovers chart sources after NIC's own template rendering. New
// charts are automatically included; unsupported source types fail explicitly.
func (i *Inventory) Applications(t *testing.T, dir, gitURL string) {
	t.Helper()
	apps, err := filepath.Glob(filepath.Join(dir, "apps", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range apps {
		var app application
		if err := yaml.Unmarshal(Read(t, filename), &app); err != nil {
			t.Fatal(err)
		}
		sources := app.Spec.Sources
		if app.Spec.Source.RepoURL != "" {
			sources = append(sources, app.Spec.Source)
		}
		for _, src := range sources {
			if src.Helm == nil {
				if src.RepoURL != gitURL {
					t.Fatalf("unhandled application source: %+v", src)
				}
				continue
			}
			values := map[string]any{}
			if len(src.Helm.Values)+len(src.Helm.ValuesObject)+len(src.Helm.Parameters)+len(src.Helm.FileParameters) > 0 {
				t.Fatalf("%s: unsupported Helm override; NIC uses valueFiles, update inventory support before adding another precedence layer", filename)
			}
			for _, valueFile := range src.Helm.ValueFiles {
				if !strings.HasPrefix(valueFile, "$values/") {
					t.Fatalf("unhandled values source: %s", valueFile)
				}
				rel := strings.TrimPrefix(valueFile, "$values/")
				if !filepath.IsLocal(rel) {
					t.Fatal("values path escapes render directory")
				}
				matches, err := filepath.Glob(filepath.Join(dir, rel))
				if err != nil {
					t.Fatal(err)
				}
				if len(matches) == 0 && !strings.Contains(rel, "*") {
					t.Fatalf("missing values: %s", rel)
				}
				for _, match := range matches {
					var overlay map[string]any
					if err := yaml.Unmarshal(Read(t, match), &overlay); err != nil {
						t.Fatal(err)
					}
					values = chartutil.CoalesceTables(overlay, values)
				}
			}
			source := "pkg/argocd/templates/apps/" + filepath.Base(filename)
			if src.Chart != "" {
				c := i.Chart(t, source, src.RepoURL, src.Chart, src.TargetRevision, src.Helm.ReleaseName, app.Spec.Destination.Namespace, values)
				if c.Name() == "cloudnative-pg" && i.postgresDefault == "" {
					version := c.Metadata.AppVersion
					effective, err := chartutil.CoalesceValues(c, values)
					if err != nil {
						t.Fatal(err)
					}
					if image, ok := effective["image"].(map[string]any); ok {
						if tag, _ := image["tag"].(string); tag != "" {
							version = tag
						}
					}
					url := "https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/v" + strings.TrimPrefix(version, "v") + "/pkg/versions/versions.go"
					i.postgresDefault = GoString(t, Fetch(t, url), "DefaultImageName")
				}
				if c.Name() == "gateway-helm" {
					// Envoy's data plane is spawned by the controller and is
					// absent from Helm's pod specs. Its default is versioned
					// with the gateway, not with NIC's Kubernetes version.
					url := "https://raw.githubusercontent.com/envoyproxy/gateway/" + c.Metadata.AppVersion + "/api/v1alpha1/shared_types.go"
					img := GoString(t, Fetch(t, url), "DefaultEnvoyProxyImage")
					var proxy struct {
						Spec struct {
							Provider struct {
								Kubernetes struct {
									EnvoyDeployment struct {
										Container struct {
											Image           string
											ImageRepository string
										}
									}
								}
							}
						}
					}
					if err := yaml.Unmarshal(Read(t, filepath.Join(dir, "manifests/networking/envoyproxy.yaml")), &proxy); err != nil {
						t.Fatal(err)
					}
					container := proxy.Spec.Provider.Kubernetes.EnvoyDeployment.Container
					if container.Image != "" {
						img = container.Image
					} else if container.ImageRepository != "" {
						tagged, _, _ := strings.Cut(img, "@")
						colon := strings.LastIndex(tagged, ":")
						if colon < 0 {
							t.Fatal("review Envoy default without tag")
						}
						img = container.ImageRepository + tagged[colon:]
					}
					if err := i.Add(img, source+" -> "+url); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if !filepath.IsLocal(src.Path) {
					t.Fatalf("unsupported chart path: %s", src.Path)
				}
				root := Checkout(t, src.RepoURL, src.TargetRevision)
				i.LocalChart(t, source, filepath.Join(root, src.Path), src.Helm.ReleaseName, app.Spec.Destination.Namespace, values)
			}
		}
	}
}
