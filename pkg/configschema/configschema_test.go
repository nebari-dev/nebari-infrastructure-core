package configschema

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// reflectTypeOf keeps the tests reading as values rather than reflect calls.
func reflectTypeOf(v any) reflect.Type { return reflect.TypeOf(v) }

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Sample exercises the reflector settings: a yaml-tagged field, an optional
// one, and a named struct that should land in $defs under a package-qualified
// key. Exported deliberately - AddGoComments collects doc comments for
// exported types only, so an unexported fixture would silently carry no
// descriptions and make TestGenerateIncludesGodocDescriptions untestable.
type Sample struct {
	// Name identifies the thing.
	Name string `yaml:"name"`

	// Nickname is optional.
	Nickname string `yaml:"nickname,omitempty"`

	Nested Nested `yaml:"Nested,omitempty"`
}

type Nested struct {
	Value string `yaml:"value,omitempty"`
}

// InlineHolder mirrors the shape that made the top-level schema unsatisfiable:
// a struct whose only field is an inline map. invopop reports the field as
// embedded and then declines to walk it, because it is not a struct.
type InlineHolder struct {
	Providers map[string]any `yaml:",inline"`
}

func generate(t *testing.T, v any, opts Options) map[string]any {
	t.Helper()

	data, err := Generate(context.Background(), reflectTypeOf(v), FormatJSON, opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshalling generated schema: %v", err)
	}
	return doc
}

func TestGenerateReadsYAMLTagsAndInfersRequired(t *testing.T) {
	doc := generate(t, Sample{}, Options{Title: "Sample"})

	if got := doc["title"]; got != "Sample" {
		t.Errorf("title = %v, want Sample", got)
	}

	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("no $defs in %v", doc)
	}

	// The Namer package-qualifies named struct types so same-named types from
	// different packages cannot merge into one entry.
	def, ok := defs["configschema.Sample"].(map[string]any)
	if !ok {
		t.Fatalf("$defs has no configschema.Sample (keys: %v)", keysOf(defs))
	}

	props, ok := def["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Sample has no properties: %v", def)
	}
	// Field names come from the yaml tag, not the Go field or a json tag.
	for _, want := range []string{"name", "nickname", "Nested"} {
		if _, ok := props[want]; !ok {
			t.Errorf("property %q missing (got %v)", want, keysOf(props))
		}
	}

	// Required is inferred from the absence of `,omitempty`.
	required, _ := def["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("required = %v, want [name]", required)
	}

	if closed, ok := def["additionalProperties"]; !ok || closed != false {
		t.Errorf("additionalProperties = %v, want false", closed)
	}
}

func TestGenerateIncludesGodocDescriptions(t *testing.T) {
	// PackagePaths are module-relative, so they only resolve on disk from the
	// module root - which is exactly why callers must pin the working
	// directory (see cmd/docgen's pushDir).
	t.Chdir("../..")

	doc := generate(t, Sample{}, Options{PackagePaths: []string{"pkg/configschema"}})

	defs := doc["$defs"].(map[string]any)
	def := defs["configschema.Sample"].(map[string]any)
	props := def["properties"].(map[string]any)
	name := props["name"].(map[string]any)

	if desc, _ := name["description"].(string); !strings.Contains(desc, "identifies the thing") {
		t.Errorf("godoc description did not reach the schema, got %q", desc)
	}
}

// An absolute PackagePaths entry walks the right files and matches no import
// path, so every schema generates and validates while carrying no descriptions
// at all. Nothing else signals it, so Generate refuses it.
func TestGenerateRejectsAbsolutePackagePaths(t *testing.T) {
	_, err := Generate(context.Background(), reflectTypeOf(Sample{}), FormatJSON, Options{
		PackagePaths: []string{"/tmp/pkg/configschema"},
	})
	if err == nil {
		t.Fatal("an absolute package path must be an error, not silently description-less output")
	}
	if !strings.Contains(err.Error(), "module-relative") {
		t.Errorf("the error should say what is wrong with the path, got: %v", err)
	}
}

// Without InlineMaps, a struct carrying only an inline map reflects as a closed
// object with no properties - a schema nothing can satisfy.
func TestGenerateInlineMapWithoutOverrideIsUnsatisfiable(t *testing.T) {
	doc := generate(t, InlineHolder{}, Options{})

	defs := doc["$defs"].(map[string]any)
	def := defs["configschema.InlineHolder"].(map[string]any)

	props, _ := def["properties"].(map[string]any)
	if len(props) != 0 {
		t.Errorf("expected the inline map to reflect as no properties, got %v", keysOf(props))
	}
	if def["additionalProperties"] != false {
		t.Errorf("expected the empty object to be closed, got %v", def["additionalProperties"])
	}
}

func TestGenerateInlineMapsDescribesTheMap(t *testing.T) {
	doc := generate(t, InlineHolder{}, Options{
		InlineMaps: map[string]InlineMap{
			"configschema.InlineHolder": {AllowedKeys: []string{"aws", "local"}, ExactlyOne: true},
		},
	})

	defs := doc["$defs"].(map[string]any)
	def := defs["configschema.InlineHolder"].(map[string]any)

	if _, ok := def["properties"]; ok {
		t.Errorf("the empty reflected property set should be dropped, got %v", def["properties"])
	}
	if ap, ok := def["additionalProperties"].(map[string]any); !ok || ap["type"] != "object" {
		t.Errorf("additionalProperties = %v, want a schema of type object", def["additionalProperties"])
	}
	if def["minProperties"] != float64(1) || def["maxProperties"] != float64(1) {
		t.Errorf("ExactlyOne should pin min/max to 1, got min=%v max=%v",
			def["minProperties"], def["maxProperties"])
	}

	names, ok := def["propertyNames"].(map[string]any)
	if !ok {
		t.Fatalf("no propertyNames: %v", def)
	}
	enum, _ := names["enum"].([]any)
	if len(enum) != 2 || enum[0] != "aws" || enum[1] != "local" {
		t.Errorf("propertyNames enum = %v, want [aws local]", enum)
	}
}

// A name that matches no definition is ignored: the type may not be reachable
// from the root being generated.
func TestGenerateInlineMapsIgnoresUnknownNames(t *testing.T) {
	doc := generate(t, InlineHolder{}, Options{
		InlineMaps: map[string]InlineMap{"configschema.doesNotExist": {ExactlyOne: true}},
	})
	if _, ok := doc["$defs"]; !ok {
		t.Error("an unmatched InlineMaps entry must not prevent generation")
	}
}

func TestFormatString(t *testing.T) {
	if got := FormatJSON.String(); got != "json" {
		t.Errorf("FormatJSON.String() = %q, want json", got)
	}
	if got := Format(99).String(); got != "unknown" {
		t.Errorf("Format(99).String() = %q, want unknown", got)
	}
}

func TestGenerateRejectsUnknownFormat(t *testing.T) {
	_, err := Generate(context.Background(), reflectTypeOf(Sample{}), Format(99), Options{})
	if err == nil {
		t.Fatal("an unknown format must be an error")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("unexpected error: %v", err)
	}
}
