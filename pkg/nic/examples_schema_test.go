package nic

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemasDir is the committed schema tree cmd/docgen generates. The test reads
// the committed files rather than regenerating, because those are the bytes the
// docs site and any editor fetch.
var schemasDir = filepath.Join("..", "..", "schemas")

// TestExampleConfigsMatchGeneratedSchemas validates every file under examples/
// against schemas/nebari-config.json, and each provider block within it against
// that provider's schema.
//
// The generated schemas are the primary artifact of the schema pipeline, and
// nothing else compares them to a real config: they are legal JSON Schema
// documents whether or not they describe the configs NIC accepts, so the drift
// gate passes on a schema that rejects everything. TestExampleConfigsValidate
// makes the argument for using examples/ as the corpus - they are the
// documented starting point, so a schema that disagrees with them is either a
// wrong schema or a broken example, and both are user-visible.
//
// This is a two-sided check. A schema that is too strict fails here (an
// inferred `required` on a field NIC defaults at runtime); so does a schema
// that is too loose, in the sense that a real config growing a key the schema
// closes out will fail as soon as an example uses it.
func TestExampleConfigsMatchGeneratedSchemas(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if err != nil {
		t.Fatalf("globbing examples: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no examples found; the glob or the layout changed")
	}

	topLevel := compileSchema(t, filepath.Join(schemasDir, "nebari-config.json"))

	// Provider blocks are nested under their category, and the file each one
	// is documented by is named the way cmd/docgen's schemaFileName names it:
	// cluster and dns keep bare filenames, other categories are qualified.
	categories := map[string]string{
		"cluster":    "",
		"dns":        "",
		"repository": "repository-",
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			doc := decodeYAMLAsJSON(t, path)

			if err := topLevel.Validate(doc); err != nil {
				t.Errorf("does not validate against schemas/nebari-config.json:\n%v", err)
			}

			root, ok := doc.(map[string]any)
			if !ok {
				t.Fatalf("example is %T, want a mapping", doc)
			}

			for category, prefix := range categories {
				block, ok := root[category].(map[string]any)
				if !ok {
					continue
				}
				for provider, providerCfg := range block {
					file := filepath.Join(schemasDir, "providers", prefix+provider+".json")
					if _, err := os.Stat(file); err != nil {
						t.Errorf("%s.%s has no generated schema at %s: %v", category, provider, file, err)
						continue
					}
					if err := compileSchema(t, file).Validate(providerCfg); err != nil {
						t.Errorf("%s.%s does not validate against %s:\n%v",
							category, provider, filepath.Base(file), err)
					}
				}
			}
		})
	}
}

// compileSchema loads one generated schema document.
func compileSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil {
		t.Fatalf("adding %s: %v", path, err)
	}
	schema, err := c.Compile(path)
	if err != nil {
		// A compile failure means the document itself is not valid JSON Schema
		// for the draft it declares, which is worth failing loudly on.
		t.Fatalf("compiling %s: %v", path, err)
	}
	return schema
}

// decodeYAMLAsJSON reads a YAML config into the shape the validator expects.
// The round-trip through JSON is what normalizes YAML-only types (timestamps,
// non-string keys) into the JSON data model the schema is written against.
func decodeYAMLAsJSON(t *testing.T, path string) any {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	encoded, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("re-encoding %s as JSON: %v", path, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return doc
}
