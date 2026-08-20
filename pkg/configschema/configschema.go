// Package configschema generates schema documents from Go config types.
// Today that means JSON Schema, for editor LSPs and the docs-site renderer;
// Format exists so a second format (a fully-commented YAML reference, the
// Helm values.yaml analogue) can be added without changing Generate's
// signature. Field descriptions come from godoc comments on the source
// struct, extracted at call time from the package source via
// invopop/jsonschema's AddGoComments.
package configschema

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/invopop/jsonschema"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// modulePath is the base import path passed to Reflector.AddGoComments.
// It must match the module path in go.mod for invopop/jsonschema to
// associate parsed comments with the right Go types.
const modulePath = "github.com/nebari-dev/nebari-infrastructure-core"

// Format identifies which schema-document format Generate should produce.
type Format int

const (
	// FormatJSON produces a JSON Schema document.
	FormatJSON Format = iota
)

// String returns the format name for span attributes and error messages.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	default:
		return "unknown"
	}
}

// Options controls Generate's behavior. PackagePaths is required: without
// at least one path, no field descriptions can be extracted from godoc.
type Options struct {
	// Title set on the schema root (e.g. "AWS provider configuration").
	// Optional; the type's own godoc becomes the description automatically.
	Title string

	// PackagePaths are filesystem paths to Go packages whose source
	// should be parsed for field godoc. Each path is passed through
	// to invopop/jsonschema's Reflector.AddGoComments. At least one
	// path is required for descriptions to land in the output.
	PackagePaths []string

	// InlineMaps repairs types whose only field is a `yaml:",inline"` map,
	// keyed by the type's $defs name (e.g. "config.ClusterConfig").
	//
	// invopop does not expand an inline field of map kind: it reports the
	// field as embedded and then declines to walk it, because it is not a
	// struct. The type therefore reflects as an object with no properties,
	// which AllowAdditionalProperties: false turns into a schema that
	// nothing can satisfy. Callers that know what keys such a map accepts
	// declare them here; Generate rewrites the definition to describe the
	// map instead of an empty struct.
	InlineMaps map[string]InlineMap
}

// InlineMap describes the keys an inline map accepts, so a struct that
// carries one can be given a schema that matches what the map really holds.
// The zero value permits any key any number of times.
type InlineMap struct {
	// AllowedKeys restricts the map's keys to this set. Empty means any key.
	AllowedKeys []string

	// ExactlyOne requires that precisely one key be present. Use it for a
	// one-of-many block ("only one provider can be configured at a time")
	// where the enclosing field is itself optional: the constraint applies
	// only once the block appears.
	ExactlyOne bool
}

// Generate renders the schema for the given type in the requested format.
//
// For FormatJSON, the output is a JSON Schema document produced by
// invopop/jsonschema with godoc descriptions extracted from the packages
// in opts.PackagePaths.
func Generate(ctx context.Context, t reflect.Type, format Format, opts Options) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "configschema.Generate")
	defer span.End()

	span.SetAttributes(
		attribute.String("format", format.String()),
		attribute.String("type", t.String()),
		attribute.Int("package_paths", len(opts.PackagePaths)),
	)

	r := newReflector()
	for _, path := range opts.PackagePaths {
		// AddGoComments uses the path as both the directory to walk and the
		// suffix of the import path it associates the parsed comments with.
		// An absolute path walks the right files and matches no import path,
		// so every schema still generates and validates while carrying no
		// descriptions at all - a silent failure with no other signal.
		// Refuse it rather than emit that.
		if filepath.IsAbs(path) {
			err := fmt.Errorf("PackagePaths must be module-relative, got absolute path %s: "+
				"AddGoComments matches them against import paths, so an absolute path "+
				"silently yields schemas with no descriptions", path)
			span.RecordError(err)
			return nil, err
		}
		if err := r.AddGoComments(modulePath, path); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("AddGoComments(%s): %w", path, err)
		}
	}

	schema := r.ReflectFromType(t)
	applyInlineMaps(schema, opts.InlineMaps)
	if opts.Title != "" {
		schema.Title = opts.Title
	}

	switch format {
	case FormatJSON:
		out, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("marshal JSON Schema: %w", err)
		}
		// json.MarshalIndent does not append a trailing newline; add one so
		// the committed file is POSIX-friendly and `git diff` is clean.
		return append(out, '\n'), nil
	default:
		err := fmt.Errorf("unknown format: %v", format)
		span.RecordError(err)
		return nil, err
	}
}

// applyInlineMaps rewrites each named definition into a schema for the map its
// inline field actually holds. The definition arrives with no properties and
// additionalProperties: false - see Options.InlineMaps for why - so the
// property-level constraints are set here rather than merged with anything.
// A name with no matching definition is ignored: the type may simply not be
// reachable from the root being generated.
func applyInlineMaps(schema *jsonschema.Schema, inlineMaps map[string]InlineMap) {
	for name, spec := range inlineMaps {
		def, ok := schema.Definitions[name]
		if !ok {
			continue
		}

		// The map's values are the provider blocks themselves. They are
		// described by their own schema documents, so constrain the kind
		// here and leave the contents to those. The reflected (empty)
		// property set is dropped rather than left to marshal as `{}`,
		// which reads as "this object has no fields".
		def.Properties = nil
		def.AdditionalProperties = &jsonschema.Schema{Type: "object"}

		if len(spec.AllowedKeys) > 0 {
			allowed := make([]any, len(spec.AllowedKeys))
			for i, k := range spec.AllowedKeys {
				allowed[i] = k
			}
			def.PropertyNames = &jsonschema.Schema{Enum: allowed}
		}

		if spec.ExactlyOne {
			one := uint64(1)
			def.MinProperties = &one
			def.MaxProperties = &one
		}
	}
}

// newReflector constructs the Reflector with options tuned for nebari-config.
// Centralized so JSON and future YAML paths share identical settings.
func newReflector() *jsonschema.Reflector {
	return &jsonschema.Reflector{
		// Read yaml tags (not the json default) — the source-of-truth tags
		// on every Config field are yaml: ones, including the `,omitempty`
		// hints used for required-field inference.
		FieldNameTag: "yaml",
		// Avoid an explosion of $ref/$defs for one-off anonymous types.
		Anonymous: true,
		// Close every object. This is deliberately *stricter* than the
		// runtime decoder, which ignores keys it does not know: a typo'd
		// key parses, validates and then silently does nothing, which is
		// the failure the schema is most useful for catching. Any object
		// that legitimately accepts free-form keys has to say so - see
		// Options.InlineMaps.
		AllowAdditionalProperties: false,
		// Package-qualify $defs keys for named struct types so collisions
		// across packages (e.g. aws.Config + longhorn.Config) don't merge
		// into a single entry. Composite types (slices, maps) fall back
		// to invopop's default by returning "" — they get inlined rather
		// than landing in $defs as "map[string]string" etc.
		Namer: func(t reflect.Type) string {
			if t.Kind() == reflect.Struct && t.Name() != "" {
				return t.String()
			}
			return ""
		},
	}
}
