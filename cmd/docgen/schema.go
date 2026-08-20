package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/configschema"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// generateSchemas writes JSON Schema documents for nebari-config.yaml and each
// registered provider's Config struct into outDir:
//
//	schemas/
//	  manifest.json
//	  nebari-config.json
//	  providers/
//	    <name>.json    (one per registered cluster + DNS provider)
//
// Unlike the markdown emitters, which read the source with go/ast, this one
// reflects on the live types: the provider list comes from the nic registry
// (pkg/nic/registry.go) via (*nic.Client).RegisteredConfigTypes, which reads
// each provider's config type through the optional cluster.ConfigTyped /
// dns.ConfigTyped capability. There is no parallel hard-coded list -
// registering a provider that self-describes its config type extends the
// output automatically. Field descriptions still come from godoc, which
// invopop/jsonschema reads out of the packages under pkgRoot.
//
// providersFlag narrows the run to a comma-separated subset of provider names;
// when set, the top-level schema and manifest are left alone, since neither can
// be regenerated correctly from a partial provider list. version is stamped
// into manifest.json.
//
// rootDir must be the module root. Package paths are collected relative to it
// because invopop/jsonschema's AddGoComments uses the path it is handed as both
// the directory to walk and the suffix of the Go import path it associates the
// comments with (gopath.Join(base, dir)). An absolute path therefore produces
// import paths that match nothing, and every field silently loses its
// description - schemas that look fine until you read them.
func generateSchemas(ctx context.Context, rootDir, outDir, providersFlag, version string) error {
	// outDir is resolved against the *caller's* working directory before the
	// chdir below, so a relative -root and a relative -schema-output cannot
	// compound into rootDir/rootDir/schemas.
	outDir, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve schema output dir %s: %w", outDir, err)
	}

	// Relative package paths only resolve on disk from the module root, so pin
	// the working directory for the duration. docgen is a single-goroutine
	// build tool; there is nothing else in the process to disturb.
	restore, err := pushDir(rootDir)
	if err != nil {
		return err
	}
	defer restore()

	return generateSchemasInRoot(ctx, outDir, providersFlag, version)
}

// pushDir changes the working directory to dir and returns a function that
// restores the previous one.
func pushDir(dir string) (func(), error) {
	prev, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getwd: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return nil, fmt.Errorf("chdir %s: %w", dir, err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			panic(fmt.Sprintf("restoring working directory %s: %v", prev, err))
		}
	}, nil
}

func generateSchemasInRoot(ctx context.Context, outDir, providersFlag, version string) error {
	const pkgRoot = "pkg"

	if err := os.MkdirAll(filepath.Join(outDir, "providers"), 0o750); err != nil {
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

	// One entry per registry category. Driven off ConfigTypes rather than
	// written out per category, so adding a category is a single line here
	// instead of a fourth near-identical loop that is easy to forget.
	categories := []struct {
		group string
		label string
		types map[string]reflect.Type
	}{
		{"cluster", "cluster provider", types.Cluster},
		{"dns", "DNS provider", types.DNS},
		{"repository", "repository provider", types.Repository},
	}

	if emitTopLevel {
		if err := writeSchema(ctx, outDir, "nebari-config.json",
			reflect.TypeFor[config.NebariConfig](),
			"Nebari config", pkgPaths); err != nil {
			return err
		}
	}

	names := make(map[string][]string, len(categories))
	for _, c := range categories {
		names[c.group] = sortedKeys(c.types)
		for _, name := range names[c.group] {
			if !accepts(filter, name) {
				continue
			}
			if err := writeSchema(ctx, outDir, filepath.Join("providers", schemaFileName(c.group, name)),
				c.types[name],
				fmt.Sprintf("%s %s configuration", name, c.label), pkgPaths); err != nil {
				return err
			}
		}
	}

	if emitTopLevel {
		if err := writeManifest(outDir, version, names); err != nil {
			return err
		}
	}

	fmt.Printf("Schemas generated successfully in %s (cluster: %v, dns: %v, repository: %v)\n",
		outDir, names["cluster"], names["dns"], names["repository"])
	return nil
}

// schemaFileName mirrors the markdown emitter's page-naming rule: cluster and
// dns providers keep bare filenames so the docs site's existing URLs stay
// valid, and any later category is qualified with its group. Without this,
// cluster/local and repository/local both write providers/local.json and one
// silently overwrites the other.
func schemaFileName(group, name string) string {
	if bareNameGroups[group] {
		return name + ".json"
	}
	return group + "-" + name + ".json"
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
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", full, err)
	}
	return nil
}

// manifest is the shape of schemas/manifest.json. The docs site fetches
// this first to discover what schemas exist, then fetches each referenced
// file. Adding a provider extends its category's list automatically.
//
// "providers" holds the cluster providers rather than every provider; it
// predates the other categories and keeps its name so the docs site's existing
// consumer doesn't break. Names here are provider names, not filenames - see
// schemaFileName for how a category qualifies them on disk.
type manifest struct {
	Comment    string   `json:"_comment"`
	Version    string   `json:"version,omitempty"`
	Providers  []string `json:"providers"`
	DNS        []string `json:"dns"`
	Repository []string `json:"repository"`
	TopLevel   string   `json:"top_level"`
}

// manifestComment marks the schemas/ tree as generated. JSON has no comment
// syntax, so it rides as a "_comment" field the docs consumer ignores.
const manifestComment = "Generated by cmd/docgen (make docs); do not edit by hand."

func writeManifest(outDir, version string, names map[string][]string) error {
	m := manifest{
		Comment:    manifestComment,
		Version:    version,
		Providers:  names["cluster"],
		DNS:        names["dns"],
		Repository: names["repository"],
		TopLevel:   "nebari-config.json",
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(outDir, "manifest.json"), data, 0o600)
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

func sortedKeys(m map[string]reflect.Type) []string {
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
