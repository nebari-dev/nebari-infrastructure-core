// Package configschema generates schema documents from Go config types.
// Two output formats are supported: JSON Schema (for editor LSPs and the
// docs-site renderer) and a fully-commented YAML reference (the Helm
// values.yaml analogue). Field descriptions come from godoc comments on
// the source struct, extracted at call time from the package source.
//
// Currently a skeleton — Generate is not yet implemented. The full
// implementation will wrap github.com/invopop/jsonschema for the JSON
// path and goccy/go-yaml's CommentMap for the YAML path.
package configschema

import (
	"context"
	"errors"
	"reflect"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

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

// Options controls Generate's behavior. PackagePaths is the only required
// field: it lists the filesystem paths whose Go source should be parsed
// for field godoc.
type Options struct {
	// Title set on the schema root (e.g. "AWS provider configuration").
	Title string

	// Description set on the schema root.
	Description string

	// PackagePaths are filesystem paths to the Go packages whose source
	// should be parsed for field godoc. Required: without at least one
	// path, no field descriptions can be extracted.
	PackagePaths []string
}

// Generate renders the schema for the given type in the requested format.
//
// Not yet implemented. The intended behavior is documented in
// config-schema-plan.md and ADR-0005:
//   - FormatJSON: invopop/jsonschema with Reflector.AddGoComments populated
//     from Options.PackagePaths.
//   - FormatYAML: a fully-commented YAML document constructed via
//     goccy/go-yaml's CommentMap, reusing the same comment source.
func Generate(ctx context.Context, t reflect.Type, format Format, opts Options) ([]byte, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "configschema.Generate")
	defer span.End()

	span.SetAttributes(
		attribute.String("format", format.String()),
		attribute.String("type", t.String()),
	)

	return nil, errors.New("configschema.Generate: not yet implemented")
}
