package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	// AddGoComments walks recursively, so the whole tree is covered by its
	// root. Passing each package separately parsed the same files once per
	// ancestor and made the per-directory skips below look load-bearing when
	// they were not: a syntax error under any testdata/ still failed the run,
	// attributed to the package that "skipped" it.
	pkgPaths := []string{pkgRoot}

	client, err := nic.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("build nic client: %w", err)
	}
	types := client.RegisteredConfigTypes(ctx)

	filter := parseFilter(providersFlag)
	emitTopLevel := len(filter) == 0

	categories := categoryTable(types)

	names := make(map[string][]string, len(categories))
	for _, c := range categories {
		names[c.group] = sortedKeys(c.types)
	}

	// A filter entry that matches nothing writes no file and would otherwise
	// exit 0, having printed the full provider list as though it had
	// regenerated it. A typo'd -schema-providers must fail, not look clean.
	if err := checkFilterMatches(filter, names); err != nil {
		return err
	}

	if err := checkSchemaNameCollisions(names); err != nil {
		return err
	}

	if emitTopLevel {
		// The provider names come from the registry, so the top-level schema
		// describes exactly the providers this build ships - see inlineMaps.
		if err := writeSchema(ctx, outDir, "nebari-config.json",
			reflect.TypeFor[config.NebariConfig](),
			"Nebari config", pkgPaths, inlineMaps(categories, names)); err != nil {
			return err
		}
	}

	for _, c := range categories {
		for _, name := range names[c.group] {
			if !accepts(filter, c.group, name) {
				continue
			}
			if err := writeSchema(ctx, outDir, filepath.Join("providers", schemaFileName(c.group, name)),
				c.types[name],
				fmt.Sprintf("%s %s configuration", name, c.label), pkgPaths, nil); err != nil {
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

func writeSchema(ctx context.Context, outDir, relPath string, t reflect.Type, title string, pkgPaths []string, maps map[string]configschema.InlineMap) error {
	data, err := configschema.Generate(ctx, t, configschema.FormatJSON, configschema.Options{
		Title:        title,
		PackagePaths: pkgPaths,
		InlineMaps:   maps,
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

// accepts reports whether the filter selects this provider. A bare name selects
// it in every category; qualifying it with the category ("repository/local")
// selects only that one, since cluster/local and repository/local are different
// providers that a bare "local" cannot tell apart.
func accepts(filter map[string]struct{}, group, name string) bool {
	if filter == nil {
		return true
	}
	if _, ok := filter[name]; ok {
		return true
	}
	_, ok := filter[group+"/"+name]
	return ok
}

// category pairs a registry provider category with what the emitters need to
// know about it: the group name used in filenames and the manifest, a label
// for schema titles, the $defs name of the wrapper type in the top-level
// schema, and the provider config types themselves.
type category struct {
	group   string
	label   string
	defName string
	types   map[string]reflect.Type
}

// categoryTable describes every category in ConfigTypes. It is a literal list
// because each entry carries naming that cannot be derived from the type, but
// it must stay exhaustive: a category missing here emits no schema, and CI
// cannot see the gap. The drift gate compares the regenerated tree against the
// committed one, so it catches a schema that stops being emitted - but only for
// a category that was emitted once, leaving a tracked file to go missing. A
// category that was never in this table has no committed file to diff against,
// which is how the repository category shipped undocumented.
// TestCategoryTableCoversConfigTypes compares this list against ConfigTypes'
// fields so an addition there cannot be silently forgotten here.
func categoryTable(types *nic.ConfigTypes) []category {
	return []category{
		{"cluster", "cluster provider", "config.ClusterConfig", types.Cluster},
		{"dns", "DNS provider", "config.DNSConfig", types.DNS},
		{"repository", "repository provider", "config.RepositoryConfig", types.Repository},
	}
}

// inlineMaps describes each category wrapper in the top-level schema.
//
// cluster, dns and repository each hold their providers in a single
// `yaml:",inline"` map, which invopop reflects as an object with no properties;
// closed, that schema rejects every real config. What the validator actually
// enforces is that the block names one registered provider and no more than one
// (pkg/config's "no provider is configured" / "only one ... at a time" errors),
// so that is what the schema says here, with the names taken from the registry
// rather than a list maintained alongside it.
func inlineMaps(categories []category, names map[string][]string) map[string]configschema.InlineMap {
	out := make(map[string]configschema.InlineMap, len(categories))
	for _, c := range categories {
		out[c.defName] = configschema.InlineMap{
			AllowedKeys: names[c.group],
			ExactlyOne:  true,
		}
	}
	return out
}

// checkFilterMatches reports filter entries that match no registered provider.
func checkFilterMatches(filter map[string]struct{}, names map[string][]string) error {
	if len(filter) == 0 {
		return nil
	}

	known := make(map[string]struct{})
	var flat []string
	for group, list := range names {
		for _, n := range list {
			known[n] = struct{}{}
			known[group+"/"+n] = struct{}{}
			flat = append(flat, group+"/"+n)
		}
	}

	var unmatched []string
	for f := range filter {
		if _, ok := known[f]; !ok {
			unmatched = append(unmatched, f)
		}
	}
	if len(unmatched) == 0 {
		return nil
	}
	sort.Strings(unmatched)
	sort.Strings(flat)
	return fmt.Errorf("-schema-providers: no registered provider named %s; known providers are %s "+
		"(a name may be qualified with its category, e.g. repository/local)",
		strings.Join(unmatched, ", "), strings.Join(flat, ", "))
}

// checkSchemaNameCollisions fails when two providers would write the same file.
// schemaFileName qualifies categories outside bareNameGroups, but cluster and
// dns are both bare, so a DNS provider sharing a cluster provider's name would
// overwrite its schema - exit 0, no warning, with the manifest advertising both.
// The markdown emitter guards its own filenames (checkOutputNameCollisions);
// this is the same guard over the schema filenames.
func checkSchemaNameCollisions(names map[string][]string) error {
	claimed := make(map[string]string)
	groups := make([]string, 0, len(names))
	for group := range names {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	for _, group := range groups {
		for _, name := range names[group] {
			file := schemaFileName(group, name)
			owner := group + "/" + name
			if prev, ok := claimed[file]; ok {
				return fmt.Errorf("schema filename collision: %s is claimed by both %s and %s; "+
					"remove one from bareNameGroups so its category qualifies the filename", file, prev, owner)
			}
			claimed[file] = owner
		}
	}
	return nil
}
