// schemagen emits JSON Schema documents for nebari-config.yaml and each
// registered provider's Config struct. It is an internal build/CI tool,
// not a user-facing subcommand of nic.
//
// Output layout (default `-out ./schemas`):
//
//	schemas/
//	  manifest.json
//	  nebari-config.json
//	  providers/
//	    <name>.json    (one per registered cluster + DNS provider)
//
// The provider list is sourced from the nic registry (pkg/nic/registry.go)
// via (*nic.Client).RegisteredConfigTypes; there is no parallel hard-coded
// list. Adding a new provider to the registry automatically extends the
// schemagen output on the next CI run.
//
// Invocation: `make schemas` or `go run ./cmd/schemagen -out ./schemas`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/configschema"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

func main() {
	var (
		outDir    string
		providers string
		pkgRoot   string
		version   string
	)
	flag.StringVar(&outDir, "out", "./schemas", "output directory for generated schema files")
	flag.StringVar(&providers, "providers", "", "comma-separated subset to regenerate (default: all registered)")
	flag.StringVar(&pkgRoot, "pkg-root", "./pkg", "root directory whose Go packages are scanned for field godoc")
	flag.StringVar(&version, "version", "", "version string stamped into manifest.json (default: empty)")
	flag.Parse()

	ctx := context.Background()
	if err := run(ctx, outDir, providers, pkgRoot, version); err != nil {
		slog.Error("schemagen failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, outDir, providersFlag, pkgRoot, version string) error {
	if err := os.MkdirAll(filepath.Join(outDir, "providers"), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	pkgPaths, err := collectPackagePaths(pkgRoot)
	if err != nil {
		return fmt.Errorf("collect package paths under %s: %w", pkgRoot, err)
	}

	client, err := nic.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("build nic client: %w", err)
	}
	types := client.RegisteredConfigTypes(ctx)

	filter := parseFilter(providersFlag)
	emitTopLevel := len(filter) == 0

	clusterNames := sortedKeys(types.Cluster)
	dnsNames := sortedKeys(types.DNS)

	if emitTopLevel {
		if err := writeSchema(ctx, outDir, "nebari-config.json",
			reflect.TypeFor[config.NebariConfig](),
			"Nebari config", pkgPaths); err != nil {
			return err
		}
	}

	for _, name := range clusterNames {
		if !accepts(filter, name) {
			continue
		}
		if err := writeSchema(ctx, outDir, filepath.Join("providers", name+".json"),
			types.Cluster[name],
			fmt.Sprintf("%s cluster provider configuration", name), pkgPaths); err != nil {
			return err
		}
	}

	for _, name := range dnsNames {
		if !accepts(filter, name) {
			continue
		}
		if err := writeSchema(ctx, outDir, filepath.Join("providers", name+".json"),
			types.DNS[name],
			fmt.Sprintf("%s DNS provider configuration", name), pkgPaths); err != nil {
			return err
		}
	}

	if emitTopLevel {
		if err := writeManifest(outDir, version, clusterNames, dnsNames); err != nil {
			return err
		}
	}

	fmt.Printf("schemagen wrote schemas under %s\n", outDir)
	fmt.Printf("  cluster providers: %v\n", clusterNames)
	fmt.Printf("  dns providers:     %v\n", dnsNames)
	return nil
}

func writeSchema(ctx context.Context, outDir, relPath string, t reflect.Type, title string, pkgPaths []string) error {
	data, err := configschema.Generate(ctx, t, configschema.FormatJSON, configschema.Options{
		Title:        title,
		PackagePaths: pkgPaths,
	})
	if err != nil {
		return fmt.Errorf("generate %s: %w", relPath, err)
	}
	full := filepath.Join(outDir, relPath)
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", full, err)
	}
	return nil
}

// manifest is the shape of schemas/manifest.json. The docs site fetches
// this first to discover what schemas exist, then fetches each referenced
// file. Adding a new provider extends Providers/DNS automatically.
type manifest struct {
	Version   string   `json:"version,omitempty"`
	Providers []string `json:"providers"`
	DNS       []string `json:"dns"`
	TopLevel  string   `json:"top_level"`
}

func writeManifest(outDir, version string, cluster, dns []string) error {
	m := manifest{
		Version:   version,
		Providers: cluster,
		DNS:       dns,
		TopLevel:  "nebari-config.json",
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(outDir, "manifest.json"), data, 0o644)
}

// collectPackagePaths walks root and returns every subdirectory that
// contains at least one non-test .go file. These paths are passed to
// configschema.Generate as Options.PackagePaths so invopop/jsonschema
// can pick up godoc comments wherever the type tree leads.
func collectPackagePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
			return fs.SkipDir
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
				paths = append(paths, path)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseFilter(raw string) map[string]struct{} {
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for name := range strings.SplitSeq(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func accepts(filter map[string]struct{}, name string) bool {
	if filter == nil {
		return true
	}
	_, ok := filter[name]
	return ok
}
