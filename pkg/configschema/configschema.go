// Package configschema generates schema documents from Go config types.
// Two output formats are supported: JSON Schema (for editor LSPs and the
// docs-site renderer) and a fully-commented YAML reference (the Helm
// values.yaml analogue). Field descriptions come from godoc comments on
// the source struct, extracted at call time from the package source via
// invopop/jsonschema's AddGoComments.
package configschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// FormatYAML produces a fully-commented YAML reference document with
	// the same field structure as FormatJSON and godoc descriptions
	// rendered as YAML comments above each field.
	FormatYAML
)

// String returns the format name for span attributes and error messages.
func (f Format) String() string {
	switch f {
	case FormatJSON:
		return "json"
	case FormatYAML:
		return "yaml"
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

	// Description set on the schema root, overriding the type's godoc.
	// Optional.
	Description string

	// PackagePaths are filesystem paths to Go packages whose source
	// should be parsed for field godoc. Each path is passed through
	// to invopop/jsonschema's Reflector.AddGoComments. At least one
	// path is required for descriptions to land in the output.
	PackagePaths []string
}

// Generate renders the schema for the given type in the requested format.
//
// For FormatJSON, the output is a JSON Schema document produced by
// invopop/jsonschema with godoc descriptions extracted from the packages
// in opts.PackagePaths. For FormatYAML, the output is not yet implemented
// and the function returns an error.
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
		if err := r.AddGoComments(modulePath, path); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("AddGoComments(%s): %w", path, err)
		}
	}

	schema := r.ReflectFromType(t)
	if opts.Title != "" {
		schema.Title = opts.Title
	}
	if opts.Description != "" {
		schema.Description = opts.Description
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
	case FormatYAML:
		err := errors.New("FormatYAML not yet implemented")
		span.RecordError(err)
		return nil, err
	default:
		err := fmt.Errorf("unknown format: %v", format)
		span.RecordError(err)
		return nil, err
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
		// nebari-config does not accept unknown fields at any level;
		// the validator surfaces them as errors. Reflect that in the schema.
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
