package main

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// readSchema decodes a generated schema file into a generic map.
func readSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshalling %s: %v", path, err)
	}
	return doc
}

func TestGenerateSchemasWritesRegisteredProviders(t *testing.T) {
	outDir := t.TempDir()

	if err := generateSchemas(context.Background(), "../..", outDir, "", "v1.2.3"); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}

	manifest := readSchema(t, filepath.Join(outDir, "manifest.json"))

	if got := manifest["version"]; got != "v1.2.3" {
		t.Errorf("manifest version = %v, want v1.2.3", got)
	}
	if got := manifest["top_level"]; got != "nebari-config.json" {
		t.Errorf("manifest top_level = %v, want nebari-config.json", got)
	}
	if manifest["_comment"] != manifestComment {
		t.Errorf("manifest _comment = %v, want the generated-file marker", manifest["_comment"])
	}

	// Every provider the manifest advertises must have a file behind it - the
	// docs site fetches manifest.json first and then follows those names.
	for _, key := range []string{"providers", "dns", "repository"} {
		names, ok := manifest[key].([]any)
		if !ok {
			t.Fatalf("manifest %s is %T, want a list", key, manifest[key])
		}
		if len(names) == 0 {
			t.Errorf("manifest %s is empty; the registry should always yield at least one", key)
		}
		group := key
		if group == "providers" {
			group = "cluster" // "providers" is the cluster list under its legacy name
		}
		for _, n := range names {
			path := filepath.Join(outDir, "providers", schemaFileName(group, n.(string)))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("manifest lists %s/%v but %s is missing: %v", key, n, path, err)
			}
		}
	}
}

// The whole point of routing through the registry rather than a hard-coded
// list is that a newly registered provider cannot be silently missing from the
// output. Assert the manifest and the registry agree.
func TestGenerateSchemasCoversEveryRegisteredProvider(t *testing.T) {
	outDir := t.TempDir()

	if err := generateSchemas(context.Background(), "../..", outDir, "", ""); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}

	manifest := readSchema(t, filepath.Join(outDir, "manifest.json"))

	entries, err := os.ReadDir(filepath.Join(outDir, "providers"))
	if err != nil {
		t.Fatalf("reading providers dir: %v", err)
	}

	want := len(manifest["providers"].([]any)) +
		len(manifest["dns"].([]any)) +
		len(manifest["repository"].([]any))
	if len(entries) != want {
		t.Errorf("wrote %d provider schemas but the manifest lists %d", len(entries), want)
	}
}

func TestGenerateSchemasProviderFilterSkipsTopLevel(t *testing.T) {
	outDir := t.TempDir()

	if err := generateSchemas(context.Background(), "../..", outDir, "aws", ""); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "providers", "aws.json")); err != nil {
		t.Errorf("aws.json not written under a -schema-providers=aws run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "providers", "hetzner.json")); !os.IsNotExist(err) {
		t.Errorf("hetzner.json written despite the filter (err = %v)", err)
	}

	// A partial run must not touch the manifest or top-level schema: neither
	// can be regenerated correctly from a subset of providers, and rewriting
	// them would drop the filtered-out entries.
	for _, name := range []string{"manifest.json", "nebari-config.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s rewritten by a filtered run (err = %v)", name, err)
		}
	}
}

// The enum/default constraints the markdown emitter reads out of `jsonschema`
// tags must also land in the schema. This is the shared contract between the
// two emitters; see constraintSuffix.
func TestGenerateSchemasCarriesTagConstraints(t *testing.T) {
	outDir := t.TempDir()

	if err := generateSchemas(context.Background(), "../..", outDir, "", ""); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}

	doc := readSchema(t, filepath.Join(outDir, "providers", "aws.json"))
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("aws.json has no $defs")
	}

	// $defs keys are package-qualified (e.g. "aws.Taint") so same-named types
	// from different packages don't collide.
	var taint map[string]any
	for name, def := range defs {
		if strings.HasSuffix(name, ".Taint") {
			taint, _ = def.(map[string]any)
		}
	}
	if taint == nil {
		t.Fatalf("no *.Taint definition in aws.json $defs (keys: %v)", slices.Sorted(maps.Keys(defs)))
	}

	props := taint["properties"].(map[string]any)
	effect := props["effect"].(map[string]any)
	enum, ok := effect["enum"].([]any)
	if !ok {
		t.Fatalf("taint.effect has no enum: %v", effect)
	}
	want := []string{"NO_SCHEDULE", "NO_EXECUTE", "PREFER_NO_SCHEDULE"}
	if len(enum) != len(want) {
		t.Fatalf("taint.effect enum = %v, want %v", enum, want)
	}
	for i, v := range want {
		if enum[i] != v {
			t.Errorf("taint.effect enum[%d] = %v, want %s", i, enum[i], v)
		}
	}
}

// Field descriptions come from godoc, which invopop/jsonschema only associates
// with a type when it is handed a *module-relative* package path - it uses the
// same string as the directory to walk and as the import-path suffix. Hand it
// an absolute path and every schema still generates, still validates, and
// silently carries no descriptions at all. Nothing else in the pipeline
// notices, so assert on a description directly.
func TestGenerateSchemasIncludesGodocDescriptions(t *testing.T) {
	outDir := t.TempDir()

	if err := generateSchemas(context.Background(), "../..", outDir, "", ""); err != nil {
		t.Fatalf("generateSchemas: %v", err)
	}

	doc := readSchema(t, filepath.Join(outDir, "providers", "local.json"))
	defs := doc["$defs"].(map[string]any)

	var described int
	for name, def := range defs {
		d, ok := def.(map[string]any)
		if !ok {
			continue
		}
		if desc, _ := d["description"].(string); desc != "" {
			described++
			continue
		}
		t.Logf("%s has no description", name)
	}

	if described == 0 {
		t.Errorf("no type in local.json carries a godoc description; AddGoComments was given paths it could not match")
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]struct{}
	}{
		{"empty means all", "", nil},
		{"single", "aws", map[string]struct{}{"aws": {}}},
		{"trims whitespace", " aws , hetzner ", map[string]struct{}{"aws": {}, "hetzner": {}}},
		{"drops empty entries", "aws,,hetzner,", map[string]struct{}{"aws": {}, "hetzner": {}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFilter(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseFilter(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestAcceptsNilFilterAcceptsEverything(t *testing.T) {
	if !accepts(nil, "anything") {
		t.Error("a nil filter must accept every provider")
	}
	if accepts(map[string]struct{}{"aws": {}}, "hetzner") {
		t.Error("a filter must reject providers it doesn't list")
	}
}

func TestCollectPackagePathsSkipsTestdataAndTestOnlyDirs(t *testing.T) {
	root := t.TempDir()

	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("real/config.go", "package real\n")
	mustWrite("testonly/thing_test.go", "package testonly\n")
	mustWrite("real/testdata/fixture.go", "package fixture\n")

	paths, err := collectPackagePaths(root)
	if err != nil {
		t.Fatalf("collectPackagePaths: %v", err)
	}

	want := []string{filepath.Join(root, "real")}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("collectPackagePaths = %v, want %v (test-only dirs and testdata are excluded)", paths, want)
	}
}

// cluster/local and repository/local share a provider name, so a flat
// providers/<name>.json layout would have one silently overwrite the other -
// the same collision the markdown pages have.
func TestSchemaFileNameQualifiesNonBareGroups(t *testing.T) {
	tests := []struct {
		group, name, want string
	}{
		{"cluster", "local", "local.json"},
		{"cluster", "aws", "aws.json"},
		{"dns", "cloudflare", "cloudflare.json"},
		{"repository", "local", "repository-local.json"},
		{"repository", "existing", "repository-existing.json"},
	}

	seen := map[string]string{}
	for _, tt := range tests {
		got := schemaFileName(tt.group, tt.name)
		if got != tt.want {
			t.Errorf("schemaFileName(%q, %q) = %q, want %q", tt.group, tt.name, got, tt.want)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("%s is claimed by both %s and %s/%s", got, prev, tt.group, tt.name)
		}
		seen[got] = tt.group + "/" + tt.name
	}
}
